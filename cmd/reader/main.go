package main

import (
	"context"
	"import/internal/db"
	"import/internal/writer"
	"log"
)

func main() {
	ctx := context.Background()
	db, err := db.NewDB(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.CreateTableIfNotExist(ctx); err != nil {
		log.Fatal(err)
	}

	if err := writer.NewWriter(db).StartServer(":9090"); err != nil {
		log.Fatal(err)
	}

}
