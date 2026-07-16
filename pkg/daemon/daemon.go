package daemon

import (
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"log"
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
}

// New creates a new Daemon instance.
func New(cfg Config) *Daemon {
	return &Daemon{
		config: cfg,
	}
}

// Start boots up the daemon, generating or loading the X25519 keypair.
func (d *Daemon) Start() error {
	log.Println("Starting KiwiDaemon...")

	if err := d.initCrypto(); err != nil {
		return fmt.Errorf("failed to initialize crypto: %w", err)
	}

	pubPEM, _ := crypto.EncodePublicKeyToPEM(d.pubKey)
	log.Printf("Daemon initialized with Public Key:\n%s\n", pubPEM)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	pubKeyBase64 := base64.StdEncoding.EncodeToString(d.pubKey.Bytes())
	log.Printf("Registration Payload (Base64 PubKey): %s\n", pubKeyBase64)

	// Future: start heartbeat polling loop (Issue #79)
	log.Println("Ready for handshake and task orchestration (polling not yet implemented).")

	return nil
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
