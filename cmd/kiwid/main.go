package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

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

	cfg, err := orchestrator.LoadAndValidateConfig(*addr, *dsn, *role, *natsURL)
	if err != nil {
		fmt.Printf("Startup error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("====================================================")
	fmt.Printf("  KIWID: Kiwi Cloud Daemon Server (Role: %s)\n", cfg.Role)
	fmt.Println("====================================================")
	cfg.Log()

	// Initialize GORM Postgres DB
	db, err := orchestrator.InitDB(cfg.DSN)
	if err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}

	var js jetstream.JetStream
	var nc *nats.Conn
	var errNats error

	nc, errNats = nats.Connect(cfg.NatsURL)
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
	server := orchestrator.NewServer(storage, cfg.Role)

	if cfg.Role == "all" || cfg.Role == "orchestrator" {
		// Recover tasks interrupted by a previous restart before accepting new work.
		server.RecoverTasks()

		// Return expired daemon leases to the queue so a crashed daemon's work is
		// picked up by another. Dead-lettering after MaxLeaseAttempts happens
		// inside RequeueExpiredLeases.
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if n, err := storage.RequeueExpiredLeases(ctx); err != nil {
						fmt.Printf("[Orchestrator] requeue expired leases: %v\n", err)
					} else if n > 0 {
						fmt.Printf("[Orchestrator] requeued %d expired lease(s)\n", n)
					}
				}
			}
		}()

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

	if cfg.Role == "all" || cfg.Role == "api" {
		if js != nil {
			relay := queue.NewRelay(storage, &JetStreamPublisher{js: js})
			go relay.Start(ctx)
			fmt.Println("[API] Outbox Relay started")
		} else {
			fmt.Println("[API] JetStream not available; Outbox Relay NOT started")
		}

		err = server.Start(cfg.Addr)
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
