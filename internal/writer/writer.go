package writer

import (
	"context"
	"fmt"
	"import/internal/db"
	"import/internal/model"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/rs/cors"
)

const (
	chunkSize   = 13050
	workerCount = 10
)

type Writer struct {
	db       *db.DB
	mux      *http.ServeMux
	upgrader *websocket.Upgrader
}

func NewWriter(db *db.DB) *Writer {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	return &Writer{
		db:       db,
		mux:      new(http.ServeMux),
		upgrader: &upgrader,
	}
}

func (w *Writer) RegisterHandlers() {
	w.mux.HandleFunc("/process", w.UploadHandler)
	w.mux.HandleFunc("/locations", w.GetLocations)
}

func (w *Writer) StartServer(addr string) error {
	w.RegisterHandlers()

	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}).Handler(w.mux)
	fmt.Println("Starting server on", addr)

	if err := http.ListenAndServe(addr, corsHandler); err != nil {
		return fmt.Errorf("error starting server: %v", err)
	}

	return nil
}

func (w *Writer) UploadHandler(wr http.ResponseWriter, r *http.Request) {
	conn, err := w.upgrader.Upgrade(wr, r, nil)
	if err != nil {
		http.Error(wr, "Failed to upgrade to WebSocket", http.StatusInternalServerError)
		return
	}
	defer conn.Close()
	log.Println("WebSocket connection established.")

	rows := make(chan []model.Location, workerCount*chunkSize)
	errChan := make(chan error, workerCount)
	var wg sync.WaitGroup
	count := new(atomic.Uint32)

	for range workerCount {
		wg.Add(1)
		go w.worker(r.Context(), &wg, rows, errChan, count)
	}

	go func() {
		defer close(rows)
		w.produceBatches(conn, rows)
	}()

	go func() {
		wg.Wait()
		close(errChan)
	}()

	if err := w.monitorErrors(conn, errChan); err != nil {
		log.Printf("Processing failed: %v", err)
		return
	}

	log.Println("All data processed successfully.")
	conn.WriteMessage(websocket.TextMessage, []byte("Processing complete"))
}

func (w *Writer) worker(
	ctx context.Context,
	wg *sync.WaitGroup,
	rows <-chan []model.Location,
	errChan chan<- error, count *atomic.Uint32,
) {
	defer wg.Done()
	for batch := range rows {
		if err := w.db.ProcessCSVChunks(ctx, batch); err != nil {
			errChan <- err
			return
		}
		count.Add(uint32(len(batch)))
		log.Printf("Processed: %d rows", count.Load())
	}
}

func (w *Writer) produceBatches(conn *websocket.Conn, rows chan<- []model.Location) {
	var (
		ms         map[string]int
		gotHeader  bool
		buf        []model.Location
		lineBuffer string
	)

	defer func() {
		if len(buf) > 0 {
			rows <- buf
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			handleWebSocketClose(err)
			break
		}

		data := lineBuffer + string(msg)
		lines := strings.Split(data, "\n")

		if strings.HasSuffix(data, "\n") {
			lineBuffer = ""
		} else {
			lineBuffer = lines[len(lines)-1]
			lines = lines[:len(lines)-1]
		}

		if err := w.processChunk(lines, &ms, &gotHeader, &buf, rows); err != nil {
			log.Printf("Processing error: %v", err)
			break
		}

		if err := sendAck(conn); err != nil {
			break
		}
	}

	if lineBuffer != "" {
		w.processRemainingLine(lineBuffer, &ms, &gotHeader, &buf, rows)
	}
}

func (w *Writer) processChunk(
	lines []string,
	ms *map[string]int,
	gotHeader *bool,
	buf *[]model.Location,
	rows chan<- []model.Location,
) error {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := w.processLine(line, ms, gotHeader, buf, rows); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) processLine(
	line string,
	ms *map[string]int,
	gotHeader *bool,
	buf *[]model.Location,
	rows chan<- []model.Location,
) error {
	record := strings.Split(line, ",")

	if needsHeader := !*gotHeader; needsHeader {
		if len(record) < 5 {
			return fmt.Errorf("invalid header line: %s", line)
		}
		*ms = make(map[string]int)
		for i, col := range record {
			(*ms)[strings.TrimSpace(col)] = i
		}
		*gotHeader = true
		return nil
	}

	if len(record) < len(*ms) {
		log.Printf("Skipping invalid row: %s", line)
		return nil
	}

	*buf = append(*buf, model.Location{
		LocID:       getField(record, *ms, "LOCID"),
		LocTimeZone: getField(record, *ms, "LOCTIMEZONE"),
		Country:     getField(record, *ms, "COUNTRY"),
		LocName:     getField(record, *ms, "LOCNAME"),
		Business:    getField(record, *ms, "BUSINESS"),
	})

	if len(*buf) >= chunkSize {
		rows <- *buf
		*buf = make([]model.Location, 0, chunkSize)
	}
	return nil
}

func (w *Writer) processRemainingLine(
	line string,
	ms *map[string]int,
	gotHeader *bool,
	buf *[]model.Location,
	rows chan<- []model.Location,
) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	record := strings.Split(line, ",")
	if len(record) >= len(*ms) {
		w.processLine(line, ms, gotHeader, buf, rows)
	} else {
		log.Printf("Skipping incomplete final line: %s", line)
	}
}

func getField(record []string, ms map[string]int, field string) string {
	if idx, exists := ms[field]; exists && idx < len(record) {
		return strings.TrimSpace(record[idx])
	}
	return ""
}

func handleWebSocketClose(err error) {
	if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) || err == io.EOF {
		log.Println("WebSocket closed normally")
	} else {
		log.Printf("WebSocket error: %v", err)
	}
}

func sendAck(conn *websocket.Conn) error {
	if err := conn.WriteMessage(websocket.TextMessage, []byte("Chunk received")); err != nil {
		log.Println("Failed to send ack:", err)
		return err
	}
	return nil
}

func (w *Writer) monitorErrors(conn *websocket.Conn, errChan <-chan error) error {
	for err := range errChan {
		if err != nil {
			log.Printf("Processing error: %v", err)
			if werr := conn.WriteMessage(websocket.TextMessage, []byte("Processing error")); werr != nil {
				return fmt.Errorf("error sending status: %v (original error: %w)", werr, err)
			}
			return err
		}

	}
	return nil
}
