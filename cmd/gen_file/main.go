package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"
)

const (
	totalRows  = 1000000
	outputFile = "./locations.csv"
	numWorkers = 5
	batchSize  = 30000
)

var (
	timezones  = []string{"America/New_York", "Europe/London", "Asia/Tokyo", "Australia/Sydney", "America/Los_Angeles", "Europe/Berlin"}
	countries  = []string{"USA", "UK", "Japan", "Australia", "Germany", "Canada"}
	locnames   = []string{"Springfield", "Rivertown", "Lakeside", "Hillview", "Bayport", "Meadowfield"}
	businesses = []string{"TechCorp", "CoffeeCo", "MarketPlace", "MediHealth", "EduWise", "GreenBuild"}
)

func main() {
	start := time.Now()
	file, err := os.Create(outputFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"LOCID", "LOCTIMEZONE", "COUNTRY", "LOCNAME", "BUSINESS"}); err != nil {
		log.Fatal(err)
	}

	rows := make(chan [][]string, numWorkers)

	var wg sync.WaitGroup
	for w := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))
			start := workerID * (totalRows / numWorkers)

			rowsPerWorker := totalRows / numWorkers

			batch := make([][]string, 0, batchSize)
			for i := range rowsPerWorker {
				locID := start + i + 1
				row := []string{
					fmt.Sprintf("LOC%012d", locID),
					timezones[r.Intn(len(timezones))],
					countries[r.Intn(len(countries))],
					fmt.Sprintf("%s_%d", locnames[r.Intn(len(locnames))], r.Intn(1000)),
					fmt.Sprintf("%s_%d", businesses[r.Intn(len(businesses))], r.Intn(1000)),
				}
				batch = append(batch, row)
				if len(batch) == batchSize {
					rows <- batch
					batch = make([][]string, 0, batchSize)
				}
			}
			if len(batch) > 0 {
				rows <- batch
			}
		}(w)
	}

	go func() {
		wg.Wait()
		close(rows)

	}()

	for batch := range rows {
		if err := writer.WriteAll(batch); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Println("Done! CSV saved as:", outputFile)
	fmt.Println("Elapsed time:", time.Since(start))
}
