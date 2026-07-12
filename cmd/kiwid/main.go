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
	role := flag.String("role", "all", "The node role to run: 'api', 'orchestrator', or 'all'")
	flag.Parse()

	fmt.Println("====================================================")
	fmt.Printf("  KIWID: Kiwi Cloud Daemon Server (Role: %s)\n", *role)
	fmt.Println("====================================================")

	// Initialize GORM Postgres DB
	db, err := orchestrator.InitDB(*dsn)
	if err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}

	storage := store.NewPostgresStore(db)
	server := orchestrator.NewServer(storage, *role)

	if *role == "all" || *role == "orchestrator" {
		// Recover tasks interrupted by a previous restart before accepting new work.
		server.RecoverTasks()
	}

	if *role == "all" || *role == "api" {
		err = server.Start(*addr)
		if err != nil {
			fmt.Printf("Error starting Kiwi daemon API: %v\n", err)
			os.Exit(1)
		}
	} else {
		// If running exclusively as orchestrator, block forever
		fmt.Println("[Orchestrator] Running in background worker mode...")
		select {}
	}
}
