package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/orchestrator"
)

func main() {
	addr := flag.String("addr", ":8080", "The address the Kiwi cloud daemon listens on")
	dbPath := flag.String("db", "kiwi.db", "The path to the SQLite database file")
	flag.Parse()

	fmt.Println("====================================================")
	fmt.Println("  KIWID: Kiwi Cloud Daemon Server")
	fmt.Println("====================================================")

	// Initialize GORM SQLite DB
	db, err := orchestrator.InitDB(*dbPath)
	if err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}

	server := orchestrator.NewServer(db)

	// Recover tasks interrupted by a previous restart before accepting new work.
	server.RecoverTasks()

	err = server.Start(*addr)
	if err != nil {
		fmt.Printf("Error starting Kiwi daemon: %v\n", err)
		os.Exit(1)
	}
}
