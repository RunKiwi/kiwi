package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
)

func main() {
	var apiURL string
	var keyPath string

	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	defaultKeyPath := filepath.Join(home, ".kiwi", "daemon.key")

	flag.StringVar(&apiURL, "api-url", "https://api.runkiwi.com", "The URL of the Kiwi Control Plane API")
	flag.StringVar(&keyPath, "key-path", defaultKeyPath, "Path to load/save the X25519 private key.")
	flag.Parse()

	cfg := daemon.Config{
		APIURL:  apiURL,
		KeyPath: keyPath,
	}

	d := daemon.New(cfg)
	if err := d.Start(); err != nil {
		log.Fatalf("Fatal error starting kiwidaemon: %v", err)
	}

	// Setup context that cancels on SIGINT/SIGTERM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Printf("Received signal: %v, shutting down...", sig)
		cancel()
	}()

	// Start the polling engine (blocks until context is canceled)
	if err := d.Run(ctx); err != nil {
		if err == context.Canceled {
			log.Println("Daemon shutdown complete.")
		} else {
			log.Fatalf("Daemon run error: %v", err)
		}
	}
}
