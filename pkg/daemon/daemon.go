package daemon

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
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
	// PollInterval is the base interval between heartbeats. Defaults to 5s when zero.
	PollInterval time.Duration
}

// Daemon represents the core kiwidaemon orchestrator.
type Daemon struct {
	config Config
	// X25519 keypair — used to receive credentials sealed to the daemon.
	pubKey *ecdh.PublicKey
	priKey *ecdh.PrivateKey
	// Ed25519 keypair — the daemon's signing identity for authenticating heartbeats.
	signPubKey  ed25519.PublicKey
	signPrivKey ed25519.PrivateKey
	client      *Client
}

// New creates a new Daemon instance.
func New(cfg Config) *Daemon {
	return &Daemon{
		config: cfg,
		client: NewClient(cfg.APIURL),
	}
}

// Start boots up the daemon, generating or loading its keypairs.
func (d *Daemon) Start() error {
	log.Println("Starting KiwiDaemon boot sequence...")

	if err := d.initCrypto(); err != nil {
		return fmt.Errorf("failed to initialize crypto: %w", err)
	}
	if err := d.initSigningCrypto(); err != nil {
		return fmt.Errorf("failed to initialize signing crypto: %w", err)
	}

	pubPEM, _ := crypto.EncodePublicKeyToPEM(d.pubKey)
	log.Printf("Daemon initialized with Encryption Public Key (X25519):\n%s\n", pubPEM)
	log.Printf("Daemon signing identity (Ed25519 pubkey): %s\n", base64.StdEncoding.EncodeToString(d.signPubKey))

	// Hand the signing key to the client so every heartbeat is authenticated.
	d.client.SetSigner(d.signPrivKey)

	return nil
}

// Run starts the daemon's heartbeat polling engine.
// It blocks until the context is canceled.
func (d *Daemon) Run(ctx context.Context) error {
	log.Printf("Starting polling engine (URL: %s)...", d.config.APIURL)

	baseInterval := d.config.PollInterval
	if baseInterval <= 0 {
		baseInterval = 5 * time.Second
	}
	maxInterval := 60 * time.Second
	if maxInterval < baseInterval {
		maxInterval = baseInterval
	}
	currentInterval := baseInterval

	// Immediate poll so a freshly-booted daemon picks up work without waiting.
	if !d.pollCP(ctx) {
		currentInterval = backoff(currentInterval, maxInterval)
	}

	timer := time.NewTimer(withJitter(currentInterval))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down daemon polling engine...")
			return ctx.Err()
		case <-timer.C:
			if d.pollCP(ctx) {
				currentInterval = baseInterval
			} else {
				currentInterval = backoff(currentInterval, maxInterval)
			}
			timer.Reset(withJitter(currentInterval))
		}
	}
}

// backoff doubles the interval up to max (exponential backoff on failure).
func backoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		next = max
	}
	return next
}

// withJitter returns d perturbed by +/-10% to de-synchronize a fleet of daemons.
func withJitter(d time.Duration) time.Duration {
	delta := int64(d) / 10
	if delta <= 0 {
		return d
	}
	return d + time.Duration(rand.Int63n(2*delta+1)-delta)
}

func (d *Daemon) pollCP(ctx context.Context) bool {
	req := HeartbeatReq{
		PubKey:     base64.StdEncoding.EncodeToString(d.pubKey.Bytes()),
		SignPubKey: base64.StdEncoding.EncodeToString(d.signPubKey),
		Timestamp:  time.Now().Unix(),
	}

	res, err := d.client.Heartbeat(ctx, req)
	if err != nil {
		log.Printf("Heartbeat failed: %v", err)
		return false
	}

	if res == nil {
		// No content — no tasks available.
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

// initSigningCrypto loads or generates the Ed25519 signing identity. It is
// persisted alongside the X25519 key (KeyPath + ".sign") so the daemon keeps a
// stable identity across restarts.
func (d *Daemon) initSigningCrypto() error {
	signKeyPath := ""
	if d.config.KeyPath != "" {
		signKeyPath = d.config.KeyPath + ".sign"
	}

	if signKeyPath != "" {
		if _, err := os.Stat(signKeyPath); err == nil {
			log.Printf("Loading existing Ed25519 signing key from %s\n", signKeyPath)
			keyBytes, err := os.ReadFile(signKeyPath)
			if err != nil {
				return err
			}
			priv, err := crypto.DecodeSigningPrivateKeyFromPEM(keyBytes)
			if err != nil {
				return err
			}
			d.signPrivKey = priv
			d.signPubKey = priv.Public().(ed25519.PublicKey)
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat signing key path %s: %w", signKeyPath, err)
		}
	}

	log.Println("Generating new Ed25519 signing key...")
	pub, priv, err := crypto.GenerateSigningKeyPair()
	if err != nil {
		return err
	}
	d.signPubKey = pub
	d.signPrivKey = priv

	if signKeyPath != "" {
		log.Printf("Saving signing key to %s\n", signKeyPath)
		pemBytes, err := crypto.EncodeSigningPrivateKeyToPEM(priv)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(signKeyPath), 0o700); err != nil {
			return fmt.Errorf("mkdir for signing key path: %w", err)
		}
		if err := os.WriteFile(signKeyPath, pemBytes, 0600); err != nil {
			return err
		}
	}

	return nil
}
