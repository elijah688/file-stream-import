package writer

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"import/internal/db"
	"import/internal/model"
	"io"
	"net/http"

	"github.com/rs/cors"
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

	i := 0

	ms := make(map[string]int, 0)
	for {
		record, err := csvReader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			http.Error(wr, "Error reading CSV file", http.StatusInternalServerError)
			return
		}
		if i == 0 {
			for i, c := range record {
				ms[c] = i
			}
		}
		location := model.Location{
			LocID:       record[ms["LOCID"]],
			LocTimeZone: record[ms["LOCTIMEZONE"]],
			Country:     record[ms["COUNTRY"]],
			LocName:     record[ms["LOCNAME"]],
			Business:    record[ms["BUSINESS"]],
		}

		// Process the data into the DB
		if err := w.db.ProcessCSVChunks(r.Context(), location); err != nil {
			http.Error(wr, fmt.Sprintf("failed to process CSV chunk: %v", err), http.StatusInternalServerError)
			return
		}

		i++

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
