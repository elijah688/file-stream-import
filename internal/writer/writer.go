package writer

import (
	"context"
	"fmt"
	"import/internal/model"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

const (
	chunkSize   = 13050
	workerCount = 10
)

func (w *Writer) UploadHandler(wr http.ResponseWriter, r *http.Request) {
	conn, err := w.upgrader.Upgrade(wr, r, nil)
	if err != nil {
		http.Error(wr, "Failed to upgrade to WebSocket", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	log.Println("WebSocket connection established.")

	// Channels for data flow and synchronization
	rows := make(chan []model.Location, workerCount*2) // Buffer for batches
	processed := make(chan int, workerCount*2)         // Signal processed batch sizes
	errChan := make(chan error, 1)                     // For error propagation

	var wg sync.WaitGroup
	count := new(atomic.Uint32)

	// Start workers
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Start progress reporter
	go func() {
		for batchSize := range processed {
			count.Add(uint32(batchSize))
			if err := conn.WriteMessage(websocket.TextMessage, []byte("processed")); err != nil {
				errChan <- fmt.Errorf("failed to send progress: %w", err)
				return
			}
			log.Printf("Processed: %d rows", count.Load())
		}
	}()

	// Start workers
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range rows {
				if err := w.db.ProcessCSVChunks(ctx, batch); err != nil {
					errChan <- err
					return
				}
				processed <- len(batch)
			}
		}()
	}

	// Producer: read and batch data
	go func() {
		defer close(rows)
		var (
			ms         map[string]int
			gotHeader  bool
			buf        []model.Location
			lineBuffer string
		)

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) || err == io.EOF {
					// Process any remaining partial line before exiting
					if lineBuffer != "" {
						if loc, err := w.processLine(lineBuffer, ms); err == nil {
							buf = append(buf, loc)
						}
					}
					break
				}
				errChan <- fmt.Errorf("read error: %w", err)
				return
			}

			// Process the message
			data := lineBuffer + string(msg)
			lines := strings.Split(data, "\n")

			// Handle potential partial line at the end
			if strings.HasSuffix(data, "\n") {
				lineBuffer = ""
			} else {
				lineBuffer = lines[len(lines)-1]
				lines = lines[:len(lines)-1]
			}

			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				// Handle header
				if !gotHeader {
					if err := w.processHeader(line, &ms); err != nil {
						errChan <- err
						return
					}
					gotHeader = true
					continue
				}

				// Process data line
				loc, err := w.processLine(line, ms)
				if err != nil {
					log.Printf("Skipping invalid line: %v", err)
					continue
				}
				buf = append(buf, loc)

				// Send batch when full
				if len(buf) >= chunkSize {
					rows <- buf
					buf = make([]model.Location, 0, chunkSize)
				}
			}

		}

		// Send remaining data
		if len(buf) > 0 {
			rows <- buf
		}
	}()

	// Wait for completion or error
	go func() {
		// First wait for all workers to finish processing
		wg.Wait()
		// Then close processed channel - no more updates will be sent
		close(processed)
		// Finally close error channel
		close(errChan)
	}()

	// Handle completion or errors
	if err := <-errChan; err != nil {
		log.Printf("Processing error: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v", err)))
		return
	}

	log.Println("All data processed successfully.")
	conn.WriteMessage(websocket.TextMessage, []byte("Processing complete"))
}

func (w *Writer) processHeader(line string, ms *map[string]int) error {
	record := strings.Split(line, ",")
	if len(record) < 5 {
		return fmt.Errorf("invalid header line: %s", line)
	}
	*ms = make(map[string]int)
	for i, col := range record {
		(*ms)[strings.TrimSpace(col)] = i
	}
	return nil
}

func (w *Writer) processLine(line string, ms map[string]int) (model.Location, error) {
	record := strings.Split(line, ",")
	if len(record) < len(ms) {
		return model.Location{}, fmt.Errorf("invalid row: %s", line)
	}

	return model.Location{
		LocID:       getField(record, ms, "LOCID"),
		LocTimeZone: getField(record, ms, "LOCTIMEZONE"),
		Country:     getField(record, ms, "COUNTRY"),
		LocName:     getField(record, ms, "LOCNAME"),
		Business:    getField(record, ms, "BUSINESS"),
	}, nil
}

func getField(record []string, ms map[string]int, field string) string {
	if idx, exists := ms[field]; exists && idx < len(record) {
		return strings.TrimSpace(record[idx])
	}
	return ""
}
