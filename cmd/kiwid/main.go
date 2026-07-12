package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/orchestrator"
	"github.com/ibreakthecloud/kiwi/pkg/queue"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	addr := flag.String("addr", ":8080", "The address the Kiwi cloud daemon listens on")
	dsn := flag.String("dsn", "host=localhost user=postgres password=postgres dbname=kiwi port=5432 sslmode=disable", "The Postgres DSN")
	role := flag.String("role", "all", "The node role to run: 'api', 'orchestrator', or 'all'")
	natsURL := flag.String("nats", "nats://localhost:4222", "The NATS server URL")
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

	nc, err := nats.Connect(*natsURL)
	if err != nil {
		fmt.Printf("Error connecting to NATS: %v\n", err)
		os.Exit(1)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		fmt.Printf("Error creating JetStream context: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := queue.SetupStream(ctx, js); err != nil {
		fmt.Printf("Error setting up JetStream stream: %v\n", err)
		os.Exit(1)
	}

	storage := store.NewPostgresStore(db)
	server := orchestrator.NewServer(storage, *role)

	if *role == "all" || *role == "orchestrator" {
		// Recover tasks interrupted by a previous restart before accepting new work.
		server.RecoverTasks()

		consumer := queue.NewConsumer(js, storage, server.LaunchTask)
		if err := consumer.Start(ctx); err != nil {
			fmt.Printf("Error starting consumer: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[Orchestrator] Job Consumer started")
	}

	if *role == "all" || *role == "api" {
		relay := queue.NewRelay(db, js)
		go relay.Start(ctx)
		fmt.Println("[API] Outbox Relay started")

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
