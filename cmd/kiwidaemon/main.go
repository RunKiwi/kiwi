package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

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

	// For Phase 1 scaffold, we just block here.
	// Eventually this will run a polling loop (Issue #79) or block on a context.
	select {}
}
