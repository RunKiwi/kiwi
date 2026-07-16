package daemon

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"log"
	"os"

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
	pubKey ed25519.PublicKey
	priKey ed25519.PrivateKey
}

// New creates a new Daemon instance.
func New(cfg Config) *Daemon {
	return &Daemon{
		config: cfg,
	}
}

// Start boots up the daemon, generating or loading the Ed25519 keypair.
func (d *Daemon) Start() error {
	log.Println("Starting KiwiDaemon...")

	if err := d.initCrypto(); err != nil {
		return fmt.Errorf("failed to initialize crypto: %w", err)
	}

	pubPEM, _ := crypto.EncodePublicKeyToPEM(d.pubKey)
	log.Printf("Daemon initialized with Public Key:\n%s\n", pubPEM)

	// Print base64 encoded public key simulating payload for registration
	log.Printf("Registration Payload (Base64 PubKey): %s\n", base64.StdEncoding.EncodeToString(d.pubKey))

	// Future: start heartbeat polling loop (Issue #79)
	log.Println("Ready for handshake and task orchestration (polling not yet implemented).")

	return nil
}

func (d *Daemon) initCrypto() error {
	if d.config.KeyPath != "" {
		if _, err := os.Stat(d.config.KeyPath); err == nil {
			// Key exists, load it
			log.Printf("Loading existing Ed25519 keypair from %s\n", d.config.KeyPath)
			keyBytes, err := os.ReadFile(d.config.KeyPath)
			if err != nil {
				return err
			}
			priv, err := crypto.DecodePrivateKeyFromPEM(keyBytes)
			if err != nil {
				return err
			}
			d.priKey = priv
			// Derive public key from private key
			// Ed25519 private keys are 64 bytes, public keys are the last 32 bytes of the generation process,
			// but we can extract it from the PrivateKey which is typed as []byte.
			d.pubKey = priv.Public().(ed25519.PublicKey)
			return nil
		}
	}

	log.Println("Generating new Ed25519 keypair...")
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
		if err := os.WriteFile(d.config.KeyPath, pemBytes, 0600); err != nil {
			return err
		}
	}

	return nil
}
