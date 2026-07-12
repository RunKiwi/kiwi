package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/orchestrator"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func main() {
	addr := flag.String("addr", ":8080", "The address the Kiwi cloud daemon listens on")
	dsn := flag.String("dsn", "host=localhost user=postgres password=postgres dbname=kiwi port=5432 sslmode=disable", "The Postgres DSN")
	flag.Parse()

	fmt.Println("====================================================")
	fmt.Println("  KIWID: Kiwi Cloud Daemon Server")
	fmt.Println("====================================================")

	// Initialize GORM Postgres DB
	db, err := orchestrator.InitDB(*dsn)
	if err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}

	storage := store.NewPostgresStore(db)
	server := orchestrator.NewServer(storage)

	// Recover tasks interrupted by a previous restart before accepting new work.
	server.RecoverTasks()

	err = server.Start(*addr)
	if err != nil {
		fmt.Printf("Error starting Kiwi daemon: %v\n", err)
		os.Exit(1)
	}
}
