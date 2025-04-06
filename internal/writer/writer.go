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
	"sync"
	"sync/atomic"

	"github.com/rs/cors"
)

const (
	chunkSize   = 12000
	workerCount = 10
)

type Writer struct {
	db  *db.DB
	mux *http.ServeMux
}

func NewWriter(db *db.DB) *Writer {
	return &Writer{
		db:  db,
		mux: new(http.ServeMux),
	}
}

func (w *Writer) RegisterHandlers() {
	w.mux.HandleFunc("/process", w.UploadHandler)
	w.mux.HandleFunc("/locations", w.GetLocations)
}

func (w *Writer) UploadHandler(wr http.ResponseWriter, r *http.Request) {
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
