package daemon

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
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
	"github.com/ibreakthecloud/kiwi/pkg/loop"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
)

// Config holds the configuration for the KiwiDaemon.
type Config struct {
	APIURL   string
	KeyPath  string
	CacheDir string
	// PollInterval is the base interval between heartbeats. Defaults to 5s when zero.
	PollInterval time.Duration
	// JoinToken is the short-lived, org-bound registration secret. It is
	// required on first boot to enrol the daemon; once registered, the daemon's
	// persisted identity key is sufficient and the token can be omitted.
	JoinToken string
	// MaxCachedRepos bounds the number of bare repositories the git cache keeps
	// before evicting the least-frequently-used one. 0 leaves the cache
	// unbounded; the kiwidaemon CLI supplies a sensible default.
	MaxCachedRepos int
	// MaxSteps caps Actor iterations per task; 0 uses the loop default.
	MaxSteps int
	// MaxBudgetUSD caps provider spend per task on the customer's key; 0 uses
	// the loop default. A runaway loop on a live key is a real cost risk.
	MaxBudgetUSD float64
	// RenewInterval configures how often the daemon extends the lease of a running task.
	// Defaults to 4 minutes if zero.
	RenewInterval time.Duration
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

	// newProvider builds the Actor/Critic from an unsealed API key and the
	// worker's model. Injectable so tests can drive the loop with a mock LLM
	// instead of calling Anthropic. A nil provider return means "no key" — the
	// daemon then cannot run a real loop.
	newProvider func(apiKey, model string) (provider.Provider, provider.Critic)
}

// New creates a new Daemon instance.
func New(cfg Config) (*Daemon, error) {
	// 0 (or negative) means unbounded; the CLI default supplies a real bound.
	maxRepos := cfg.MaxCachedRepos
	if maxRepos < 0 {
		maxRepos = 0
	}
	cache, err := gitcache.NewWithLimit(cfg.CacheDir, maxRepos)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize git cache: %w", err)
	}

	return &Daemon{
		config:      cfg,
		client:      NewClient(cfg.APIURL),
		gitCache:    cache,
		newProvider: defaultProvider,
	}, nil
}

// defaultProvider builds a live Anthropic Actor/Critic from the unsealed API
// key. An empty key yields nil providers, signalling that no real loop can run.
func defaultProvider(apiKey, model string) (provider.Provider, provider.Critic) {
	if apiKey == "" {
		return nil, nil
	}
	// One model drives both Actor and Critic for now; per-role models are a
	// future refinement once the planner emits them.
	ap := provider.NewAnthropicProviderWithModels(apiKey, model, model)
	return ap, ap
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

	// Register if a join token was supplied. Registration is idempotent for a
	// known identity (it re-binds/rotates), so presenting a fresh token on a
	// restart is harmless. Without a token we assume a prior registration and
	// proceed straight to polling; an unregistered daemon simply gets 403s.
	if d.config.JoinToken != "" {
		if err := d.register(); err != nil {
			return fmt.Errorf("daemon registration failed: %w", err)
		}
		log.Println("Daemon registered with Control Plane.")
	} else {
		log.Println("No join token supplied; assuming prior registration.")
	}

	return nil
}

// register performs the join handshake using the daemon's public keys.
func (d *Daemon) register() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return d.client.Register(ctx, RegisterReq{
		JoinToken:  d.config.JoinToken,
		PubKey:     base64.StdEncoding.EncodeToString(d.pubKey.Bytes()),
		SignPubKey: base64.StdEncoding.EncodeToString(d.signPubKey),
	})
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

	// Open the sealed credential bundle once for this heartbeat. Only this
	// daemon's X25519 private key can open it; the plaintext lives in memory for
	// the duration of the tasks below and is never written to disk.
	creds, err := d.openCredentials(res.EncryptedCreds)
	if err != nil {
		log.Printf("Failed to open sealed credentials: %v", err)
		// Without credentials the agent cannot reach its LLM/Git provider. Do
		// not silently run a half-configured task; fail the lease so it requeues.
		for _, spec := range res.Specs {
			d.reportResult(ctx, spec.ID, res.LeaseID, false, "", "failed to open sealed credentials")
		}
		return true
	}

	for _, spec := range res.Specs {
		// Start a lease renewal goroutine that ticks at half the lease TTL
		// to ensure the task isn't reclaimed by the CP while we work on it.
		renewCtx, renewCancel := context.WithCancel(ctx)
		go func(specID string) {
			interval := d.config.RenewInterval
			if interval <= 0 {
				interval = 4 * time.Minute
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-renewCtx.Done():
					return
				case <-ticker.C:
					err := d.client.RenewLease(renewCtx, RenewReq{
						TaskID:     specID,
						LeaseID:    res.LeaseID,
						SignPubKey: base64.StdEncoding.EncodeToString(d.signPubKey),
					})
					if err != nil {
						// A 409 means the lease was lost; we just log it here.
						log.Printf("Failed to renew lease for task %s: %v", specID, err)
					} else {
						log.Printf("Successfully renewed lease for task %s", specID)
					}
				}
			}
		}(spec.ID)

		ok, prURL, detail := d.executeTask(ctx, spec, creds)
		renewCancel() // Stop the renewal timer

		d.reportResult(ctx, spec.ID, res.LeaseID, ok, prURL, detail)
	}

	return true
}

// openCredentials decrypts the sealed credential bundle from a heartbeat into a
// name→value map. An empty blob (org has no credentials) is not an error.
func (d *Daemon) openCredentials(sealed string) (map[string]string, error) {
	if sealed == "" {
		return map[string]string{}, nil
	}
	plaintext, err := crypto.OpenSealed(d.priKey, sealed)
	if err != nil {
		return nil, fmt.Errorf("open sealed box: %w", err)
	}
	var creds map[string]string
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("decode credential bundle: %w", err)
	}
	return creds, nil
}

// anthropicKeyName is the credential the daemon uses to call the LLM. It is
// deliberately withheld from the sandbox environment: the Actor/Critic run in
// the daemon process, so the sandbox — which executes model-generated code —
// never holds the model key (architecture review §3.1).
const anthropicKeyName = "ANTHROPIC_API_KEY"

// smokeCommand is the fallback run when a task cannot drive a real loop (no test
// command, target file, or LLM key). It keeps the end-to-end seam exercisable
// for smoke tests without pretending an agent ran.
var smokeCommand = `echo "processing: $TASK" > sandbox.log`

// executeTask provisions a workspace and runs the worker's Actor–Critic loop
// against it, returning whether the task succeeded (its test command passed).
//
// The LLM Actor/Critic run in the daemon process; only the test command runs in
// the sandbox. That split means the sandbox executes model-generated code with
// a default-deny network and without the LLM key, while the daemon holds the
// key and reaches the provider itself.
func (d *Daemon) executeTask(ctx context.Context, spec agent.WorkerSpec, creds map[string]string) (bool, string, string) {
	log.Printf(" - Task ID: %s, Model: %s, Target: %s", spec.ID, spec.Model, spec.Task)

	// Sanitize spec.ID to prevent path traversal into the cache dir.
	if matched, _ := regexp.MatchString(`^[A-Za-z0-9_-]+$`, spec.ID); !matched {
		log.Printf("Invalid task ID format: %s", spec.ID)
		return false, "", "invalid task ID format"
	}

	worktreePath := filepath.Join(d.config.CacheDir, "worktrees", spec.ID)

	if spec.RepoURL != "" && spec.Ref != "" {
		log.Printf("Cloning worktree for %s (ref: %s)...", spec.RepoURL, spec.Ref)
		if err := d.gitCache.GetWorktree(ctx, spec.RepoURL, spec.Ref, worktreePath); err != nil {
			log.Printf("Failed to provision worktree for task %s: %v", spec.ID, err)
			return false, "", "failed to provision worktree"
		}
		defer func(url, path string) {
			log.Printf("Cleaning up worktree: %s", path)
			if err := d.gitCache.RemoveWorktree(context.Background(), url, path); err != nil {
				log.Printf("Failed to remove worktree: %v", err)
			}
		}(spec.RepoURL, worktreePath)
	} else {
		worktreePath = filepath.Join(os.TempDir(), "kiwi-sandbox", spec.ID)
		if err := os.MkdirAll(worktreePath, 0o755); err != nil {
			log.Printf("Failed to create fallback sandbox dir: %v", err)
			return false, "", "failed to create fallback sandbox dir"
		}
	}

	sandboxCtx := context.WithValue(ctx, sandbox.SandboxConfigKey, &sandbox.SandboxConfig{
		UseDocker:   dockerEnabled(),
		MemoryLimit: "512m",
		CPULimit:    "1.0",
		NetworkNone: true,
	})

	// Test-command environment: every credential except the LLM key.
	testEnv := []string{"TASK=" + spec.Task}
	for name, value := range creds {
		if name == anthropicKeyName {
			continue
		}
		testEnv = append(testEnv, name+"="+value)
	}

	// Build the Actor/Critic from the LLM key (daemon-side, not in the sandbox).
	actor, critic := d.newProvider(creds[anthropicKeyName], spec.Model)

	// A real loop needs a target file, a definition of done, and a provider.
	// Missing any, fall back to a smoke command rather than fake an agent run.
	if spec.File == "" || spec.TestCmd == "" || actor == nil {
		log.Printf("Task %s: no %s to run an agent loop; running smoke command instead",
			spec.ID, missingLoopInput(spec, actor))
		result, err := sandbox.RunCommand(sandboxCtx, worktreePath, smokeCommand, testEnv)
		if err != nil {
			log.Printf("Smoke command failed for task %s: %v", spec.ID, err)
			return false, "", fmt.Sprintf("smoke command failed: %v", err)
		}
		return result.Success, "", "smoke command run"
	}

	log.Printf("Running Actor–Critic loop for task %s (file %s, test %q)...", spec.ID, spec.File, spec.TestCmd)
	runner := &loop.Runner{
		Provider: actor,
		Critic:   critic,
		Config: loop.Config{
			MaxSteps:     d.config.MaxSteps,
			MaxBudgetUSD: d.config.MaxBudgetUSD,
			Log:          func(format string, a ...any) { log.Printf("task "+spec.ID+": "+format, a...) },
		},
	}
	task := loop.Task{
		Description: spec.Task,
		FilePath:    filepath.Join(worktreePath, spec.File),
	}
	runTest := func(ctx context.Context) (string, bool, error) {
		res, err := sandbox.RunCommand(sandboxCtx, worktreePath, spec.TestCmd, testEnv)
		if err != nil {
			return "", false, err
		}
		return res.Output, res.Success, nil
	}

	result, err := runner.Run(ctx, task, runTest)
	if err != nil {
		log.Printf("Task %s loop ended without success: %v (steps=%d, cost=$%.2f)",
			spec.ID, err, result.Steps, result.CostUSD)
	} else {
		log.Printf("Task %s loop complete: success=%v (steps=%d, cost=$%.2f)",
			spec.ID, result.Success, result.Steps, result.CostUSD)
	}

	prURL := ""
	detail := ""
	if result.Success {
		gitToken := creds["GIT_TOKEN"]
		if gitToken == "" {
			detail = "no GIT_TOKEN; skipped PR"
		} else {
			gh := &restGitHub{token: gitToken}
			pr, d, err := publishResult(ctx, worktreePath, spec, gitToken, gh, "")
			if err != nil {
				log.Printf("Failed to publish result for task %s: %v", spec.ID, err)
				detail = fmt.Sprintf("publish failed: %v", err)
			} else {
				prURL = pr
				detail = d
			}
		}
	}

	return result.Success, prURL, detail
}

// dockerEnabled reports whether task commands run inside a Docker sandbox.
// Isolation is on by default; set USE_DOCKER=false to run commands locally (for
// tests and development on hosts without Docker). This must be honored here
// rather than left to the sandbox package's env fallback, because executeTask
// always supplies an explicit SandboxConfig, which takes precedence over the
// environment inside RunCommand.
func dockerEnabled() bool {
	return os.Getenv("USE_DOCKER") != "false"
}

// missingLoopInput names the first missing prerequisite for a real loop, for a
// clearer log line.
func missingLoopInput(spec agent.WorkerSpec, actor provider.Provider) string {
	switch {
	case spec.File == "":
		return "target file"
	case spec.TestCmd == "":
		return "test command"
	case actor == nil:
		return "LLM credentials"
	default:
		return "prerequisites"
	}
}

// reportResult closes the lease for a task by reporting its terminal status.
// Failures here are logged, not fatal: if the report is lost, the lease simply
// expires and the task is retried.
func (d *Daemon) reportResult(ctx context.Context, taskID, leaseID string, ok bool, resultURL, detail string) {
	if leaseID == "" {
		// No fencing token (older CP, or a spec surfaced without a lease). Cannot
		// safely complete; let the lease lapse.
		return
	}
	status := "SUCCEEDED"
	if !ok {
		status = "FAILED"
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := d.client.ReportResult(reqCtx, ResultReq{
		TaskID:     taskID,
		LeaseID:    leaseID,
		Status:     status,
		SignPubKey: base64.StdEncoding.EncodeToString(d.signPubKey),
		ResultURL:  resultURL,
		Detail:     detail,
	})
	if err != nil {
		log.Printf("Failed to report result for task %s: %v", taskID, err)
	}
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
