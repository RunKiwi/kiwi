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

type JetStreamPublisher struct {
	js jetstream.JetStream
}

func (p *JetStreamPublisher) Publish(ctx context.Context, topic string, payload []byte, msgID string) error {
	_, err := p.js.Publish(ctx, topic, payload, jetstream.WithMsgID(msgID))
	return err
}

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

	var js jetstream.JetStream
	var nc *nats.Conn
	var errNats error

	nc, errNats = nats.Connect(*natsURL)
	ctx := context.Background()

	if errNats != nil {
		fmt.Printf("[Warning] Error connecting to NATS: %v. Running without NATS...\n", errNats)
	} else {
		defer nc.Close()
		js, errNats = jetstream.New(nc)
		if errNats != nil {
			fmt.Printf("[Warning] Error creating JetStream context: %v\n", errNats)
		} else {
			if err := queue.SetupStream(ctx, js); err != nil {
				fmt.Printf("[Warning] Error setting up JetStream stream: %v\n", err)
			}
		}
	}

	storage := store.NewPostgresStore(db)
	server := orchestrator.NewServer(storage, *role)

	if *role == "all" || *role == "orchestrator" {
		// Recover tasks interrupted by a previous restart before accepting new work.
		server.RecoverTasks()

		if js != nil {
			consumer := queue.NewConsumer(js, storage, server.LaunchTask)
			if err := consumer.Start(ctx); err != nil {
				fmt.Printf("Error starting consumer: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("[Orchestrator] Job Consumer started")
		} else {
			fmt.Println("[Orchestrator] JetStream not available; Job Consumer NOT started")
		}
	}

	if *role == "all" || *role == "api" {
		if js != nil {
			relay := queue.NewRelay(storage, &JetStreamPublisher{js: js})
			go relay.Start(ctx)
			fmt.Println("[API] Outbox Relay started")
		} else {
			fmt.Println("[API] JetStream not available; Outbox Relay NOT started")
		}

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
