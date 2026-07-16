package daemon

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
)

// Config holds the configuration for the KiwiDaemon.
type Config struct {
	APIURL  string
	KeyPath string
}

// Daemon represents the core kiwidaemon orchestrator.
type Daemon struct {
	config Config
	pubKey *ecdh.PublicKey
	priKey *ecdh.PrivateKey
	client *Client
}

// New creates a new Daemon instance.
func New(cfg Config) *Daemon {
	return &Daemon{
		config: cfg,
		client: NewClient(cfg.APIURL),
	}
}

// Start boots up the daemon, generating or loading the X25519 keypair.
func (d *Daemon) Start() error {
	log.Println("Starting KiwiDaemon boot sequence...")

	if err := d.initCrypto(); err != nil {
		return fmt.Errorf("failed to initialize crypto: %w", err)
	}

	pubPEM, _ := crypto.EncodePublicKeyToPEM(d.pubKey)
	log.Printf("Daemon initialized with Public Key:\n%s\n", pubPEM)

	return nil
}

// Run starts the daemon's heartbeat polling engine.
// It blocks until the context is canceled.
func (d *Daemon) Run(ctx context.Context) error {
	pubKeyBase64 := base64.StdEncoding.EncodeToString(d.pubKey.Bytes())
	req := HeartbeatReq{PubKey: pubKeyBase64}

	log.Printf("Starting polling engine (URL: %s)...", d.config.APIURL)

	baseInterval := 5 * time.Second
	maxInterval := 60 * time.Second
	currentInterval := baseInterval

	// Immediate poll
	success := d.pollCP(ctx, req)
	if !success {
		currentInterval *= 2
	}

	timer := time.NewTimer(currentInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down daemon polling engine...")
			return ctx.Err()
		case <-timer.C:
			success := d.pollCP(ctx, req)
			if success {
				currentInterval = baseInterval
			} else {
				currentInterval *= 2
				if currentInterval > maxInterval {
					currentInterval = maxInterval
				}
			}

			// Add jitter (e.g. +/- 10%)
			jitterRange := int64(currentInterval) / 5
			if jitterRange <= 0 {
				jitterRange = 1
			}
			jitter := time.Duration(rand.Int63n(jitterRange)) - (currentInterval / 10)
			timer.Reset(currentInterval + jitter)
		}
	}
}

func (d *Daemon) pollCP(ctx context.Context, req HeartbeatReq) bool {
	// log.Println("Heartbeating Control Plane...") // Commented out to avoid spam
	res, err := d.client.Heartbeat(ctx, req)
	if err != nil {
		log.Printf("Heartbeat failed: %v", err)
		return false
	}

	if res == nil {
		// No content
		return true
	}

	log.Printf("Received worker specs from Control Plane! (Tasks: %d)", len(res.Specs))
	for _, spec := range res.Specs {
		log.Printf(" - Task ID: %s, Model: %s, Target: %s", spec.ID, spec.Model, spec.Task)
	}

	// Issue #81: Integrate Sandbox Spawning here
	return true
}

func (d *Daemon) initCrypto() error {
	if d.config.KeyPath != "" {
		if _, err := os.Stat(d.config.KeyPath); err == nil {
			// Key exists, load it
			log.Printf("Loading existing X25519 keypair from %s\n", d.config.KeyPath)
			keyBytes, err := os.ReadFile(d.config.KeyPath)
			if err != nil {
				return err
			}
			priv, err := crypto.DecodePrivateKeyFromPEM(keyBytes)
			if err != nil {
				return err
			}
			d.priKey = priv
			d.pubKey = priv.PublicKey()
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat key path %s: %w", d.config.KeyPath, err)
		}
	}

	log.Println("Generating new X25519 keypair...")
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		return err
	}
	d.pubKey = pub
	d.priKey = priv

	if d.config.KeyPath != "" {
		log.Printf("Saving generated keypair to %s\n", d.config.KeyPath)
		pemBytes, err := crypto.EncodePrivateKeyToPEM(priv)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(d.config.KeyPath), 0o700); err != nil {
			return fmt.Errorf("mkdir for key path: %w", err)
		}
		if err := os.WriteFile(d.config.KeyPath, pemBytes, 0600); err != nil {
			return err
		}
	}

	return nil
}
