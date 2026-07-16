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
}

// Daemon represents the core kiwidaemon orchestrator.
type Daemon struct {
	config   Config
	pubKey   *ecdh.PublicKey
	priKey   *ecdh.PrivateKey
	client   *Client
	gitCache *gitcache.Cache
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
	res, err := d.client.Heartbeat(ctx, req)
	if err != nil {
		log.Printf("Heartbeat failed: %v", err)
		return false
	}

	if res == nil {
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
