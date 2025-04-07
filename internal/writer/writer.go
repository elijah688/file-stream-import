package writer

import (
	"bufio"
	"encoding/csv"
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
	chunkSize   = 12000
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

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error reading message:", err)
			break
		}
		ms := map[string]int{
			"LOCID":       0,
			"LOCTIMEZONE": 1,
			"COUNTRY":     2,
			"LOCNAME":     3,
			"BUSINESS":    4,
		}
		log.Printf("Received chunk: %s", string(msg))

		rows := strings.Split(string(msg), "\n")

		for _, row := range rows {
			record := strings.Split(row, ",")

			if len(record) < 5 {
				log.Printf("Skipping invalid row: %s", row)
				continue
			}

			location := model.Location{
				LocID:       record[ms["LOCID"]],
				LocTimeZone: record[ms["LOCTIMEZONE"]],
				Country:     record[ms["COUNTRY"]],
				LocName:     record[ms["LOCNAME"]],
				Business:    record[ms["BUSINESS"]],
			}

			log.Printf("Parsed Location: %+v", location)

		}

		if err = conn.WriteMessage(websocket.TextMessage, []byte("Chunk received")); err != nil {
			log.Println("Error sending acknowledgment:", err)
			break
		}
	}

	log.Println("WebSocket connection closed.")
}
func (w *Writer) uploadHandler(wr http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(wr, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("Receiving file stream...")
	reader := bufio.NewReader(r.Body)
	csvReader := csv.NewReader(reader)

	wg, errChan, rows := new(sync.WaitGroup), make(chan error), make(chan []model.Location, workerCount*chunkSize)

	count := new(atomic.Uint32)
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ls := range rows {

				if err := w.db.ProcessCSVChunks(r.Context(), ls); err != nil {
					errChan <- err
					return
				}
				count.Add(uint32(len(ls)))
				log.Println(count.Load())
			}
		}()

	}

	go func() {

		defer close(rows)

		ms := make(map[string]int, 0)

		buf := make([]model.Location, 0)
		for i := 0; ; i++ {
			record, err := csvReader.Read()
			if err != nil {
				if err == io.EOF {
					break
				}
				errChan <- fmt.Errorf("error reading CSV file")
				return
			}
			if i == 0 {
				for j, c := range record {
					ms[c] = j
				}
				continue
			}

			buf = append(buf, model.Location{
				LocID:       record[ms["LOCID"]],
				LocTimeZone: record[ms["LOCTIMEZONE"]],
				Country:     record[ms["COUNTRY"]],
				LocName:     record[ms["LOCNAME"]],
				Business:    record[ms["BUSINESS"]],
			})

			if len(buf) == chunkSize {
				rows <- buf
				buf = make([]model.Location, 0)
			}

		}
		if len(buf) > 0 {
			rows <- buf
		}
	}()

	go func() {
		wg.Wait()
		close(errChan)
	}()

	for err := range errChan {
		if err != nil {
			log.Println(err)
			http.Error(wr, "failed importing", http.StatusInternalServerError)
			return
		}
	}

	fmt.Println("CSV processed successfully!")
	wr.WriteHeader(http.StatusOK)
	wr.Write([]byte("CSV processed successfully"))
	fmt.Fprintf(wr, "CSV uploaded and processed successfully")
}
