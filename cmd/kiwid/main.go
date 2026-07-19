package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
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
	role := flag.String("role", "all", "The node role to run: 'api', 'orchestrator', 'migrate', or 'all'")
	natsURL := flag.String("nats", "nats://localhost:4222", "The NATS server URL")
	flag.Parse()

	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := orchestrator.LoadAndValidateConfig(*addr, *dsn, *role, *natsURL)
	if err != nil {
		slog.Error("Startup error", "err", err)
		os.Exit(1)
	}

	slog.Info("KIWID: Kiwi Cloud Daemon Server", "role", cfg.Role, "addr", cfg.Addr, "env", cfg.Env)

	// Initialize GORM Postgres DB
	db, err := orchestrator.InitDB(cfg.DSN)
	if err != nil {
		slog.Error("Error initializing database", "err", err)
		os.Exit(1)
	}

	if cfg.Role == "migrate" {
		slog.Info("Running migrations...")
		if err := orchestrator.RunMigrations(db); err != nil {
			slog.Error("Migration failed", "err", err)
			os.Exit(1)
		}
		slog.Info("Migrations applied successfully")
		os.Exit(0)
	}

	var js jetstream.JetStream
	var nc *nats.Conn
	var errNats error

	nc, errNats = nats.Connect(cfg.NatsURL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if errNats != nil {
		slog.Warn("Error connecting to NATS, running without NATS", "err", errNats)
	} else {
		defer nc.Close()
		js, errNats = jetstream.New(nc)
		if errNats != nil {
			slog.Warn("Error creating JetStream context", "err", errNats)
		} else {
			if err := queue.SetupStream(ctx, js); err != nil {
				slog.Warn("Error setting up JetStream stream", "err", err)
			}
		}
	}

	storage := store.NewPostgresStore(db)
	server := orchestrator.NewServer(storage, cfg.Role)

	if cfg.Role == "all" || cfg.Role == "orchestrator" {
		server.RecoverTasks()

		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if n, err := storage.RequeueExpiredLeases(ctx); err != nil {
						slog.Error("requeue expired leases failed", "err", err)
					} else if n > 0 {
						slog.Info("requeued expired lease(s)", "count", n)
					}
				}
			}
		}()

		if js != nil {
			consumer := queue.NewConsumer(js, storage, server.LaunchTask)
			if err := consumer.Start(ctx); err != nil {
				slog.Error("Error starting consumer", "err", err)
				os.Exit(1)
			}
			slog.Info("Job Consumer started")
		} else {
			slog.Info("JetStream not available; Job Consumer NOT started")
		}
	}

	if cfg.Role == "all" || cfg.Role == "api" {
		if js != nil {
			relay := queue.NewRelay(storage, &JetStreamPublisher{js: js})
			go relay.Start(ctx)
			slog.Info("Outbox Relay started")
		} else {
			slog.Info("JetStream not available; Outbox Relay NOT started")
		}
	}

	go func() {
		err = server.Start(cfg.Addr)
		if err != nil && err != fmt.Errorf("http: Server closed") && err.Error() != "http: Server closed" {
			slog.Error("Error starting Kiwi daemon API", "err", err)
			os.Exit(1)
		}
	}()

	if cfg.Role != "all" && cfg.Role != "api" {
		slog.Info("Running in background worker mode...")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server...")

	// Cancel background contexts
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "err", err)
	}

	slog.Info("Server exiting")
}
