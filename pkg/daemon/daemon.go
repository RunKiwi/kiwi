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
	"regexp"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"github.com/ibreakthecloud/kiwi/pkg/gitcache"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
)

// Config holds the configuration for the KiwiDaemon.
type Config struct {
	APIURL   string
	KeyPath  string
	CacheDir string
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
	gitCache    *gitcache.Cache
}

// New creates a new Daemon instance.
func New(cfg Config) (*Daemon, error) {
	cache, err := gitcache.New(cfg.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize git cache: %w", err)
	}

	return &Daemon{
		config:   cfg,
		client:   NewClient(cfg.APIURL),
		gitCache: cache,
	}, nil
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
		func(spec agent.WorkerSpec) {
			log.Printf(" - Task ID: %s, Model: %s, Target: %s", spec.ID, spec.Model, spec.Task)

			// 0. Sanitize spec.ID to prevent path traversal
			if matched, _ := regexp.MatchString(`^[A-Za-z0-9_-]+$`, spec.ID); !matched {
				log.Printf("Invalid task ID format: %s", spec.ID)
				return
			}

			// 1. Generate worktree path
			worktreePath := filepath.Join(d.config.CacheDir, "worktrees", spec.ID)

			// 2. Clone worktree
			if spec.RepoURL != "" && spec.Ref != "" {
				log.Printf("Cloning worktree for %s (ref: %s)...", spec.RepoURL, spec.Ref)
				if err := d.gitCache.GetWorktree(ctx, spec.RepoURL, spec.Ref, worktreePath); err != nil {
					log.Printf("Failed to provision worktree for task %s: %v", spec.ID, err)
					return
				}

				// Defer cleanup of worktree
				defer func(url, path string) {
					log.Printf("Cleaning up worktree: %s", path)
					if err := d.gitCache.RemoveWorktree(context.Background(), url, path); err != nil {
						log.Printf("Failed to remove worktree: %v", err)
					}
				}(spec.RepoURL, worktreePath)
			} else {
				// Fallback if no repo is provided, just use a temp dir for sandbox
				worktreePath = filepath.Join(os.TempDir(), "kiwi-sandbox", spec.ID)
				if err := os.MkdirAll(worktreePath, 0755); err != nil {
					log.Printf("Failed to create fallback sandbox dir: %v", err)
					return
				}
			}

			// 3. Inject Sandbox config
			sandboxCtx := context.WithValue(ctx, sandbox.SandboxConfigKey, &sandbox.SandboxConfig{
				UseDocker:   true,
				MemoryLimit: "512m",
				CPULimit:    "1.0",
				NetworkNone: true,
			})

			// 4. Execute placeholder command inside sandbox
			log.Printf("Spawning Docker sandbox for task %s...", spec.ID)
			cmdStr := "echo 'Processing task' > sandbox.log && ls -la"
			env := []string{"TASK=" + spec.Task}

			result, err := sandbox.RunCommand(sandboxCtx, worktreePath, cmdStr, env)
			if err != nil {
				log.Printf("Sandbox execution failed for task %s: %v", spec.ID, err)
			} else {
				log.Printf("Sandbox execution complete. Success: %v\nOutput:\n%s", result.Success, result.Output)
			}
		}(spec)
	}

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
