package daemon

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"github.com/ibreakthecloud/kiwi/pkg/gitcache"
	"github.com/ibreakthecloud/kiwi/pkg/loop"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/session"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// signExecution attests this task's telemetry with the daemon's own signing
// identity. The key ID is the daemon's public key, so a verifier resolves the
// signer without a registry lookup.
func signExecution(priv ed25519.PrivateKey, taskID, status string, events []ver.TaskEvent) (*ver.Signature, error) {
	keyID := base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))
	return ver.SignExecution(priv, keyID, taskID, status, events)
}

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
	// MaxRounds caps Architect/Implementer rounds per task in session mode; 0
	// uses the session default. It is separate from MaxSteps because the two
	// count different things: a step is one model call, a round is a whole
	// agentic pass over the repository, so the same number would mean wildly
	// different budgets in the two modes.
	MaxRounds int
	// MaxBudgetUSD caps provider spend per task on the customer's key; 0 uses
	// the loop default. A runaway loop on a live key is a real cost risk.
	MaxBudgetUSD float64
	// RenewInterval configures how often the daemon extends the lease of a running task.
	RenewInterval time.Duration
	// ProgressInterval is how often partial telemetry is flushed to the Control
	// Plane while a task runs. Defaults to 3s when zero. Distinct from
	// RenewInterval, which runs on minutes — far too slow to watch a run by.
	ProgressInterval time.Duration
	// SandboxRuntime configures the OCI runtime for docker sandboxes (e.g. "runsc").
	SandboxRuntime string
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

	// newProvider builds the Actor/Critic from the unsealed credential bundle and
	// the worker's model. Injectable so tests can drive the loop with a mock LLM
	// instead of calling a real provider. A nil provider return means "no usable
	// key for the selected provider" — the daemon then cannot run a real loop.
	newProvider func(creds map[string]string, model string) (provider.Provider, provider.Critic)
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

// defaultProvider selects a live Actor/Critic by the worker's model, routing
// through provider.ProviderOf so the daemon agrees with the planner, the
// dashboard and the execution record about which provider serves a model. The
// key is read from the sealed bundle under that provider's credential name. If
// it is absent the function returns nil providers, signalling no real loop can
// run.
//
// One model drives both Actor and Critic for now; per-role models are a future
// refinement once the planner emits them.
func defaultProvider(creds map[string]string, model string) (provider.Provider, provider.Critic) {
	prov := provider.ProviderOf(model)
	key := creds[provider.CredentialNameFor(prov)]
	if key == "" {
		return nil, nil
	}
	switch prov {
	case provider.ProviderGemini:
		gp := provider.NewGeminiProviderWithModels(key, model, model)
		return gp, gp
	case provider.ProviderOpenAI:
		op := provider.NewOpenAIProviderWithModels(key, model, model)
		return op, op
	default:
		ap := provider.NewAnthropicProviderWithModels(key, model, model)
		return ap, ap
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
			d.reportResult(ctx, spec.ID, res.LeaseID, taskResult{detail: "failed to open sealed credentials"})
		}
		return true
	}

	for _, spec := range res.Specs {
		// taskCtx governs the run itself. The renewal goroutine cancels it when
		// the Control Plane says the lease is gone, which is how a user-requested
		// cancel actually stops the work: the CP cannot reach this process, so
		// revoking the lease is the only signal it has, and the renewal is where
		// we listen for it.
		taskCtx, taskCancel := context.WithCancel(ctx)
		var leaseLost atomic.Bool

		go func(specID string) {
			interval := d.config.RenewInterval
			if interval <= 0 {
				interval = 4 * time.Minute
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-taskCtx.Done():
					return
				case <-ticker.C:
					err := d.client.RenewLease(taskCtx, RenewReq{
						TaskID:     specID,
						LeaseID:    res.LeaseID,
						SignPubKey: base64.StdEncoding.EncodeToString(d.signPubKey),
					})
					switch {
					case err == nil:
						log.Printf("Successfully renewed lease for task %s", specID)
					case errors.Is(err, ErrLeaseLost):
						// Definitive: the task is no longer ours. Abandon it rather
						// than burn metered agent-minutes on work whose result the
						// Control Plane will reject anyway (CompleteTask requires the
						// task to still be LEASED under our fencing token).
						log.Printf("Lease lost for task %s; aborting run: %v", specID, err)
						leaseLost.Store(true)
						taskCancel()
						return
					default:
						// Transient — a network blip must not throw away a good run.
						// The lease still has time on it; the next tick retries.
						log.Printf("Failed to renew lease for task %s: %v", specID, err)
					}
				}
			}
		}(spec.ID)

		// Live telemetry. The daemon is the only observer of the loop, so
		// without this a running task is a spinner: one that is stuck looks
		// exactly like one working hard. Flushed on its own call rather than
		// with the lease renewal, so a failed progress post can never cost the
		// daemon a task it is successfully running.
		prog := &progressReporter{}
		go d.streamProgress(taskCtx, spec.ID, res.LeaseID, prog)

		out := d.executeTask(taskCtx, spec, creds, prog, res.LeaseID)
		taskCancel() // Stop the renewal and progress timers

		// Reporting a result for a task we no longer hold is pointless — the CP
		// rejects it on the fencing token — and actively misleading in the logs.
		// The cancel already recorded the terminal state.
		if leaseLost.Load() {
			log.Printf("Skipping result report for task %s: lease was revoked", spec.ID)
			continue
		}
		d.reportResult(ctx, spec.ID, res.LeaseID, out)
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

// LLM API-key credential names. These are deliberately withheld from the
// sandbox environment: the Actor/Critic run in the daemon process, so the
// sandbox — which executes model-generated code — never holds a model key
// (architecture review §3.1).
const (
	anthropicKeyName = "ANTHROPIC_API_KEY"
	geminiKeyName    = "GEMINI_API_KEY"
	openaiKeyName    = "OPENAI_API_KEY"
)

// isLLMKey reports whether a credential is a model API key that must be kept out
// of the sandbox environment.
//
// A provider added without an entry here would leak its key into the container
// that runs model-generated code, so this is enumerated deliberately rather than
// derived from a prefix — a missing name must fail a test, not silently widen
// what the sandbox can see.
func isLLMKey(name string) bool {
	return name == anthropicKeyName || name == geminiKeyName || name == openaiKeyName
}

// executeTask provisions a workspace and runs the worker's Actor–Critic loop
// against it, returning whether the task succeeded (its test command passed).
//
// The LLM Actor/Critic run in the daemon process; only the test command runs in
// the sandbox. That split means the sandbox executes model-generated code with
// a default-deny network and without the LLM key, while the daemon holds the
// the key and reaches the provider itself.
// taskResult is everything executeTask observed about one worker run. It
// carries the loop telemetry because the daemon is the only component that
// sees the Actor–Critic loop; the Control Plane learns what happened solely
// from what is reported here.
type taskResult struct {
	ok     bool
	prURL  string
	detail string
	abuse  bool
	events []ver.TaskEvent
}

func (d *Daemon) executeTask(ctx context.Context, spec agent.WorkerSpec, creds map[string]string, prog *progressReporter, leaseID string) taskResult {
	log.Printf(" - Task ID: %s, Model: %s, Target: %s", spec.ID, spec.Model, spec.Task)

	// Sanitize spec.ID to prevent path traversal into the cache dir.
	if matched, _ := regexp.MatchString(`^[A-Za-z0-9_-]+$`, spec.ID); !matched {
		log.Printf("Invalid task ID format: %s", spec.ID)
		return taskResult{detail: "invalid task ID format"}
	}

	if spec.File != "" && !filepath.IsLocal(spec.File) {
		log.Printf("Task %s: file path %q escapes worktree", spec.ID, spec.File)
		return taskResult{detail: "file path escapes worktree"}
	}
	for _, f := range spec.Files {
		if !filepath.IsLocal(f) {
			log.Printf("Task %s: file path %q escapes worktree", spec.ID, f)
			return taskResult{detail: "file path escapes worktree"}
		}
	}

	// Anti-gaming (Execution Model RFC §8, issue #132) is no longer decided here.
	// It used to be an outright refusal of any task targeting a test file, on the
	// grounds that the Actor could satisfy "green test = done" by weakening the
	// assertion instead of fixing the code.
	//
	// That reasoning only holds while the tests are FAILING, because only then is
	// a test defining the job. When they pass, the test file defines nothing and
	// "add tests for the parser" is an ordinary request that the blanket refusal
	// rejected outright. The loop knows which case it is in — it runs the tests
	// — so it makes the call; the daemon only reports what it observed, since it
	// is the side that knows each language's naming conventions.
	targetsTest := looksLikeTestFile(spec.File)

	worktreePath := filepath.Join(d.config.CacheDir, "worktrees", spec.ID)

	if spec.RepoURL != "" && spec.Ref != "" {
		// One job = one branch (#126): base the worktree on the shared job branch
		// when it already exists, so this worker sees earlier workers' committed
		// edits and its commit fast-forwards onto them. The first worker falls
		// back to spec.Ref.
		jobBranch := jobBranchName(spec)
		log.Printf("Provisioning worktree for %s (ref: %s, job branch: %s)...", spec.ID, spec.Ref, jobBranch)
		if err := d.gitCache.GetJobWorktree(ctx, spec.RepoURL, spec.Ref, jobBranch, worktreePath); err != nil {
			log.Printf("Failed to provision worktree for task %s: %v", spec.ID, err)
			return taskResult{detail: "failed to provision worktree"}
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
			return taskResult{detail: "failed to create fallback sandbox dir"}
		}
	}

	// Wall-clock cap for the whole task (Phase 4 abuse control). The sandbox
	// context is derived from this capped context on purpose: a docker run bound
	// to a non-expiring context would ignore the deadline, so a spinning test
	// command (the cryptomining case) would hang the daemon and never be killed.
	// Deriving sandboxCtx from taskCtx is what makes the cap enforceable.
	timeout := spec.TimeoutSeconds
	if timeout <= 0 {
		timeout = 1800
	}
	taskCtx, taskCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer taskCancel()

	// The image is held by pointer so a wrong guess can be corrected in place
	// once the sandbox reports what is actually missing — see runTest below.
	sandboxCfg := &sandbox.SandboxConfig{
		UseDocker:   dockerEnabled(),
		MemoryLimit: "512m",
		CPULimit:    "1.0",
		Runtime:     d.config.SandboxRuntime,
		NetworkNone: true,
	}
	sandboxCtx := context.WithValue(taskCtx, sandbox.SandboxConfigKey, sandboxCfg)

	sessionMode := spec.Mode == agent.ModeSession

	// Test-command environment: every credential except the LLM keys.
	//
	// Session mode withholds them all by default. The exclusion of LLM keys
	// exists because the sandbox runs model-generated code; the wider exclusion
	// exists because in session mode the model also chooses the *commands*, and
	// their output travels back into the event log. A credential that can be
	// read and echoed does not need a network to escape. See
	// sessionAllowsTestCredentials for the opt-in and what it costs.
	testEnv := taskTestEnv(spec.Task, creds, sessionMode)
	if sessionMode && !sessionAllowsTestCredentials() {
		log.Printf("Task %s: session mode — withholding all credentials from the sandbox", spec.ID)
	}

	// Build the Actor/Critic (daemon-side, not in the sandbox). The provider is
	// selected from the worker's model; its key is picked from the bundle.
	actor, critic := d.newProvider(creds, spec.Model)

	// actor == nil means the model's provider has no key in this org's sealed
	// bundle (e.g. a gemini-* model but no GEMINI_API_KEY). That is a
	// configuration error — fail it with a precise reason instead of papering
	// over it with a run that pretends to succeed.
	if actor == nil {
		reason := fmt.Sprintf("no API key configured for the %s provider that model %q needs — add it under Integrations",
			providerNameForModel(spec.Model), spec.Model)
		log.Printf("Task %s: %s", spec.ID, reason)
		return taskResult{detail: reason}
	}

	// test_cmd is optional. When the submitter did not supply one, infer it from
	// the repo (go.mod → `go test ./...`, package.json test script → `npm test`,
	// and so on) so the caller does not have to know the project's test runner.
	testCmd := spec.TestCmd
	if testCmd == "" {
		if inferred := inferTestCmd(worktreePath); inferred != "" {
			log.Printf("Task %s: no test command given; inferred %q from the repo", spec.ID, inferred)
			testCmd = inferred
		}
	}

	var targetFiles []string
	var isMulti bool
	if len(spec.Files) > 0 {
		targetFiles = spec.Files
		isMulti = true
	} else if spec.File != "" {
		targetFiles = []string{spec.File}
		isMulti = false
	}

	// Everything from here to the end of the extension repair exists to decide
	// which file the Actor is allowed to edit — resolving the planner's hints
	// against the real tree, asking a model to choose when they resolve to
	// nothing, and correcting an extension the planner guessed for the wrong
	// language. A session Implementer greps for itself and writes wherever the
	// work leads, so none of it applies, and running it would cost a model call
	// to answer a question nobody asked.
	if !sessionMode {

		// The repo exists only here, on the daemon, so this is the only place a file
		// path can be checked against reality. A path on the spec is a hint from the
		// planner — which is given the repo URL, not its contents — and previously a
		// hint suppressed discovery entirely: the component that cannot see the repo
		// overrode the one that can, and a near-miss like "components/Footer.tsx"
		// against "src/components/Footer.tsx" became a new duplicate file rather than
		// an edit to the real one.
		tree, _ := repoTree(worktreePath)
		if len(targetFiles) > 0 && len(tree) > 0 {
			resolved := make([]string, 0, len(targetFiles))
			anyReal := false
			for _, hint := range targetFiles {
				got := resolveHint(hint, tree)
				if got == "" {
					// Keep the hint: it may name a file that genuinely has to be
					// created. Whether it does is decided below, once discovery has
					// had a chance to find an existing home instead.
					resolved = append(resolved, hint)
					continue
				}
				if got != hint {
					log.Printf("Task %s: target %q does not exist; resolved to %q", spec.ID, hint, got)
				}
				resolved = append(resolved, got)
				anyReal = true
			}
			targetFiles = resolved

			if !anyReal {
				log.Printf("Task %s: no planned target exists in the repo; asking the model to choose from %d file(s)", spec.ID, len(tree))
				if discovered, _ := discoverTargetFiles(taskCtx, actor, spec.Task, tree); len(discovered) > 0 {
					targetFiles = discovered
					isMulti = len(discovered) > 1
				}
				// Falling through with the original hint is deliberate: discovery only
				// ever returns files that already exist, so when it finds nothing the
				// task really may need a new file, and the loop creates it.
			}
		}

		if len(targetFiles) == 0 {
			discovered, _ := discoverTargetFiles(taskCtx, actor, spec.Task, tree)
			if len(discovered) > 0 {
				targetFiles = discovered
				isMulti = true
			} else {
				return taskResult{detail: "could not identify a file to change from the task description — set one under Advanced options"}
			}
		}

		// Repair a new file whose extension names the wrong language. The Actor can
		// only change a file's contents, never its name, so a planner that guesses
		// "examples/advanced.rs" for a Go repository creates a position the loop
		// cannot win: the Critic rejects Go code in a .rs file, correctly, every
		// time, until the budget runs out.
		eco := inferEcosystem(worktreePath, testCmd)
		for i, f := range targetFiles {
			if _, err := os.Stat(filepath.Join(worktreePath, f)); err == nil {
				continue // exists; its extension is the repository's business
			}
			if fixed := correctNewFileExtension(f, eco, worktreePath); fixed != f {
				log.Printf("Task %s: new file %q has the wrong extension for a %s project; creating %q instead", spec.ID, f, eco, fixed)
				targetFiles[i] = fixed
			}
		}
	}

	if testCmd == "" {
		return taskResult{detail: "no test command, and none could be inferred from the repo — set one under Advanced options so the fix can be verified"}
	}

	// Inject the repo's AGENT.md (if any) as per-repo context for the Actor —
	// conventions, how to run tests, what not to touch (Execution Model RFC §5).
	description := spec.Task
	if rc := repoContext(worktreePath); rc != "" && !sessionMode {
		log.Printf("Task %s: injecting repo AGENT.md context (%d bytes)", spec.ID, len(rc))
		description = withRepoContext(description, rc)
	}

	// The single-file loop and its task are built only for the mode that uses
	// them: a session has no pre-assigned file, so loop.Task's FilePath would
	// index an empty slice.
	var runner *loop.Runner
	var task loop.Task
	if !sessionMode {
		log.Printf("Running Actor–Critic loop for task %s (files %d, test %q)...", spec.ID, len(targetFiles), testCmd)
		// Runner.Run calls OnEvent inline on this goroutine, so ordering matches
		// execution order. The reporter is what carries them out of the process
		// while the task is still running; the full list is still sent with the
		// result, which remains authoritative.
		runner = &loop.Runner{
			Provider: actor,
			Critic:   critic,
			Config: loop.Config{
				MaxSteps:     d.config.MaxSteps,
				MaxBudgetUSD: d.config.MaxBudgetUSD,
				Log:          func(format string, a ...any) { log.Printf("task "+spec.ID+": "+format, a...) },
				OnEvent: func(e loop.Event) {
					prog.add(ver.TaskEvent{
						Step:         e.Step,
						Phase:        e.Phase,
						Outcome:      e.Outcome,
						Detail:       e.Detail,
						DurationMs:   e.DurationMs,
						InputTokens:  e.InputTokens,
						OutputTokens: e.OutputTokens,
						CostUSD:      e.CostUSD,
					})
				},
			},
		}
		task = loop.Task{
			Description:  description,
			FilePath:     filepath.Join(worktreePath, targetFiles[0]),
			WorktreeRoot: worktreePath,
			TargetsTest:  targetsTest || looksLikeTestFile(targetFiles[0]),
		}
		if isMulti {
			absFiles := make([]string, len(targetFiles))
			for i, f := range targetFiles {
				absFiles[i] = filepath.Join(worktreePath, f)
			}
			task.Files = absFiles
		}
	}

	// Pick the image from what the repository declares, using the test command
	// as the strongest signal — it names the binary that has to exist. Kiwi's
	// promise is that a user submits a prompt and nothing else, so there is no
	// image flag to fall back on and no question to ask them.
	// cacheEnv carries the toolchain variables that point at the durable package
	// cache. It goes to BOTH phases: the install writes there, verification
	// reads from there. It contains no credentials.
	var cacheEnv []string

	sandboxCfg.DockerImage = inferSandboxImage(worktreePath, testCmd)

	// Redirect the toolchain's package cache somewhere that outlives a single
	// container, so what the install phase downloads is what verification
	// compiles against. Both phases get the same mount and variables.
	if dc := depCacheFor(inferEcosystem(worktreePath, testCmd), d.config.CacheDir); dc != nil {
		sandboxCfg.Mounts = append(sandboxCfg.Mounts, dc.Mounts...)
		cacheEnv = dc.Env
		testEnv = append(testEnv, dc.Env...)
	}
	log.Printf("Task %s: sandbox image %s (test %q, cache mounts %d)",
		spec.ID, sandboxCfg.DockerImage, testCmd, len(sandboxCfg.Mounts))

	// Phase A: fetch the repository's declared dependencies, with network and
	// without credentials. Runs once, before the loop, so the Actor never sees
	// a missing-module error it cannot fix by editing code. See deps.go for why
	// this is separated from verification rather than solved by relaxing
	// --network none.
	if step := inferInstallStep(worktreePath); step != nil {
		if detail, ok := d.installDependencies(taskCtx, worktreePath, sandboxCfg, step, spec.ID, cacheEnv); !ok {
			return taskResult{detail: detail}
		}
	}
	// What the installed tree currently satisfies. Compared before each
	// verification so an Actor edit to a manifest can be installed rather than
	// refused — see the re-install in runTest below.
	installedManifests := manifestFingerprint(worktreePath)

	// A wrong image and a failing test are the same observation — a non-zero
	// exit with output — and conflating them is what let a Node task spend six
	// Actor steps trying to make `npm test` pass inside a Go image. The first
	// failure is therefore inspected: if the sandbox says the toolchain is
	// missing or the wrong version, repair it and re-run once before any of
	// that output reaches the Actor. Only the first failure is examined, so a
	// genuinely failing test costs nothing extra.
	envChecked := false
	// Set when the repository's own verification cannot run offline. Reported
	// verbatim instead of a generic failure — there is nothing the Actor could
	// do about it, so saying so beats spending the budget proving it.
	offlineBlocked := ""
	runTest := func(ctx context.Context) (string, bool, error) {
		// The Actor may have added a dependency. Phase B has no network, so the
		// package it named would be missing and the build would fail on that
		// rather than on anything the Actor could fix by editing code.
		//
		// Re-running the install phase is the answer, and it keeps both halves
		// of the invariant: this still runs with network and NO credentials, and
		// the verification that follows still runs offline. What changes is only
		// that the manifest being installed was written by the model — so the
		// install is given nothing worth stealing, exactly as before.
		//
		// A failed re-install is returned as the test output instead of failing
		// the task, because "404 Not Found - GET .../nonexistent-pkg" is
		// something the Actor can act on: it names a package that does not
		// exist, and the next step can correct it.
		if fp := manifestFingerprint(worktreePath); fp != installedManifests {
			if step := installStepFor(worktreePath, false); step != nil {
				log.Printf("Task %s: a dependency manifest changed; re-installing with %q", spec.ID, step.Command)
				prog.setActivity("install: "+step.Command, "")
				if detail, ok := d.installDependencies(ctx, worktreePath, sandboxCfg, step, spec.ID, cacheEnv); !ok {
					prog.setActivity("install failed", detail)
					return detail, false, nil
				}
			}
			// Recomputed rather than reused: a resolving install rewrites the
			// lockfile, so the tree's fingerprint after installing is not the
			// one that triggered it.
			installedManifests = manifestFingerprint(worktreePath)
		}

		prog.setActivity("test: "+testCmd, "")
		res, err := sandbox.RunCommand(sandboxCtx, worktreePath, testCmd, testEnv)
		if err != nil {
			return "", false, err
		}
		prog.setActivity("test: "+testCmd, res.Output)
		if !res.Success && !envChecked {
			envChecked = true
			if next, why := correctedImage(sandboxCfg.DockerImage, res.Output, worktreePath); next != "" {
				log.Printf("Task %s: %s; retrying in %s instead of %s",
					spec.ID, why, next, sandboxCfg.DockerImage)
				sandboxCfg.DockerImage = next
				if retry, rerr := sandbox.RunCommand(sandboxCtx, worktreePath, testCmd, testEnv); rerr == nil {
					res = retry
				}
				// A retry that could not run at all falls through with the
				// original result rather than losing it to a second fault.
			}

			// This is the first run, so nothing model-generated has executed
			// yet: a network failure here describes the repository, not the
			// agent. Returning an error stops the loop at step 0 rather than
			// letting it edit code in response to a failed download.
			if !res.Success {
				if why := networkRequired(res.Output); why != "" {
					log.Printf("Task %s: verification cannot run offline: %s", spec.ID, why)
					offlineBlocked = why
					return "", false, errors.New("verification requires network access")
				}
			}
		}
		return res.Output, res.Success, nil
	}

	if sessionMode {
		var installFn session.InstallFunc
		if inferInstallStep(worktreePath) != nil {
			installFn = func(ctx context.Context) (string, bool, error) {
				step := installStepFor(worktreePath, false)
				if step == nil {
					return "this repository declares no dependency install step", false, nil
				}
				if detail, ok := d.installDependencies(ctx, worktreePath, sandboxCfg, step, spec.ID, cacheEnv); !ok {
					return detail, false, nil
				}
				return "dependencies installed", true, nil
			}
		}
		return d.executeSession(taskCtx, spec, creds, prog, sessionDeps{
			leaseID:      leaseID,
			worktreePath: worktreePath,
			sandboxCfg:   sandboxCfg,
			// The Implementer's shell gets the toolchain cache variables and
			// nothing else. cacheEnv is credential-free by construction — it is
			// the same set the networked install phase is given, for the same
			// reason.
			execEnv: cacheEnv,
			testCmd: testCmd,
			verify:  runTest,
			install: installFn,
		})
	}

	result, err := runner.Run(taskCtx, task, runTest)
	if offlineBlocked != "" {
		return taskResult{detail: offlineBlocked, events: prog.all()}
	}
	if err != nil {
		log.Printf("Task %s loop ended without success: %v (steps=%d, cost=$%.2f)",
			spec.ID, err, result.Steps, result.CostUSD)
	} else {
		log.Printf("Task %s loop complete: success=%v (steps=%d, cost=$%.2f)",
			spec.ID, result.Success, result.Steps, result.CostUSD)
	}

	abuse := false
	if taskCtx.Err() == context.DeadlineExceeded && result.Steps < 2 {
		log.Printf("Task %s timed out with %d steps — flagging for cryptomining abuse", spec.ID, result.Steps)
		abuse = true
	}

	ok := result.Success
	prURL := ""
	detail := ""
	if result.Success {
		gitToken := creds["GIT_TOKEN"]
		if gitToken == "" {
			detail = "no GIT_TOKEN; skipped PR"
		} else {
			gh := &restGitHub{token: gitToken}
			pr, d, err := publishResult(ctx, worktreePath, spec, gitToken, gh, "")
			switch {
			case errors.Is(err, errNoChanges):
				// The loop was satisfied without editing anything, so there is
				// nothing to deliver. Reporting SUCCEEDED here was the exact
				// "false green" the case below exists to prevent — the user
				// asked for work, received a green tick, and got no pull
				// request and no explanation beyond the words "no changes".
				//
				// It is the normal outcome for additive work. "Add an example"
				// does not make `go build` start failing, so the test command
				// passes on unmodified code and the loop correctly concludes
				// there is nothing to do. The fault is the definition of done,
				// not the agent, and the message has to say so.
				log.Printf("Task %s: loop passed without changing anything (steps=%d)", spec.ID, result.Steps)
				ok = false
				if result.Steps == 0 {
					detail = fmt.Sprintf("the test command (%s) already passed before any change was made, so nothing was done and there is nothing to open a PR with. "+
						"This task needs a check that fails until the work exists — for new functionality, a test that exercises it.", testCmd)
				} else {
					detail = "the agent finished but left the repository unchanged, so there is nothing to open a PR with"
				}
			case err != nil:
				// The loop passed but delivery failed. Report FAILED rather than a
				// false green — a SUCCEEDED task with no PR is misleading.
				log.Printf("Failed to publish result for task %s: %v", spec.ID, err)
				detail = fmt.Sprintf("publish failed: %v", err)
				ok = false
			default:
				prURL = pr
				detail = d
			}
		}
	} else {
		// Surface WHY the loop failed so the FAILED task explains itself in the
		// Control Plane (result_detail), not only in the daemon's local logs.
		if err != nil {
			// A provider-side failure (out of credits, rate limit, bad key/model)
			// gets a clean, actionable reason instead of a raw API dump — this is
			// what the dashboard shows the operator.
			if kind, reason := provider.Classify(err); kind != provider.ErrOther {
				detail = fmt.Sprintf("%s: %s", providerNameForModel(spec.Model), reason)
			} else {
				detail = truncateDetail(fmt.Sprintf("loop failed after %d step(s): %v", result.Steps, err))
			}
		} else {
			detail = fmt.Sprintf("test did not pass within %d step(s)", result.Steps)
		}
	}

	return taskResult{ok: ok, prURL: prURL, detail: detail, abuse: abuse, events: prog.all()}
}

// installDependencies runs phase A of the sandbox: the repository's own install
// command, with network enabled and no credentials whatsoever.
//
// It reports (detail, false) when the task cannot proceed. Failing here rather
// than letting the loop discover it is deliberate — an Actor handed "Cannot
// find module 'react'" will try to fix it by editing code, which cannot work
// and costs the user their whole budget to learn.
//
// cfg is shared with the verification phase and may be updated here: if the
// install reveals the image is wrong, the correction carries over, so the tests
// do not rediscover the same fault.
func (d *Daemon) installDependencies(ctx context.Context, worktreePath string, cfg *sandbox.SandboxConfig, step *installStep, taskID string, cacheEnv []string) (string, bool) {
	log.Printf("Task %s: installing dependencies — %q (from %s)", taskID, step.Command, step.Source)

	installCtx, cancel := context.WithTimeout(ctx, installTimeout())
	defer cancel()

	// A separate config so only this phase gets network. Everything else —
	// image, gVisor runtime, resource caps — matches verification, so the
	// dependencies land in the environment that will actually use them.
	netCfg := *cfg
	netCfg.NetworkNone = false
	run := func() (*sandbox.Result, error) {
		return sandbox.RunCommand(
			context.WithValue(installCtx, sandbox.SandboxConfigKey, &netCfg),
			worktreePath, step.Command,
			// Only the cache variables — never a credential. Not the git token,
			// not a registry secret. This phase executes third-party install
			// hooks with network access, so it is deliberately given nothing
			// worth stealing.
			cacheEnv,
		)
	}

	res, err := run()
	if err != nil {
		return fmt.Sprintf("could not run dependency installation (%s): %v", step.Command, err), false
	}

	// The same environment faults that break tests break installs, and this is
	// the first command to run — so a wrong image is usually discovered here.
	if !res.Success {
		if next, why := correctedImage(netCfg.DockerImage, res.Output, worktreePath); next != "" {
			log.Printf("Task %s: %s; reinstalling in %s instead of %s", taskID, why, next, netCfg.DockerImage)
			netCfg.DockerImage = next
			cfg.DockerImage = next // carry the correction into verification
			if retry, rerr := run(); rerr == nil {
				res = retry
			}
		}
	}

	if !res.Success {
		if installCtx.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("dependency installation timed out after %s (%s)", installTimeout(), step.Command), false
		}
		return truncateDetail(fmt.Sprintf("dependency installation failed (%s): %s",
			step.Command, outputTail(res.Output, 400))), false
	}

	log.Printf("Task %s: dependencies installed", taskID)
	return "", true
}

// outputTail returns the last n bytes of s on a rune boundary. The end of a
// failing install is the part that says why.
func outputTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	for len(cut) > 0 && !utf8.RuneStart(cut[0]) {
		cut = cut[1:]
	}
	return cut
}

// maxDetailLen bounds the result detail stored on a task so a verbose provider
// or test error cannot bloat the row or a status response.
const maxDetailLen = 500

// truncateDetail caps a detail string at maxDetailLen runes.
func truncateDetail(s string) string {
	r := []rune(s)
	if len(r) <= maxDetailLen {
		return s
	}
	return string(r[:maxDetailLen]) + "…(truncated)"
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

// reportResult closes the lease for a task by reporting its terminal status.
// Failures here are logged, not fatal: if the report is lost, the lease simply
// expires and the task is retried.
func (d *Daemon) reportResult(ctx context.Context, taskID, leaseID string, out taskResult) {
	if leaseID == "" {
		// No fencing token (older CP, or a spec surfaced without a lease). Cannot
		// safely complete; let the lease lapse.
		return
	}
	status := "SUCCEEDED"
	if !out.ok {
		status = "FAILED"
	}
	sandboxRT := d.config.SandboxRuntime
	if sandboxRT == "" {
		sandboxRT = "docker"
	}
	req := ResultReq{
		TaskID:         taskID,
		LeaseID:        leaseID,
		Status:         status,
		SignPubKey:     base64.StdEncoding.EncodeToString(d.signPubKey),
		ResultURL:      out.prURL,
		Detail:         out.detail,
		Abuse:          out.abuse,
		Events:         out.events,
		SandboxRuntime: sandboxRT,
	}
	// Attest the telemetry with the daemon's own signing key. In BYOC this key
	// lives only in the customer's cloud, so the execution half of the record is
	// signed by something the Control Plane never holds. Best-effort: a signing
	// failure must not cost us the result itself.
	if sig, err := signExecution(d.signPrivKey, taskID, status, out.events); err != nil {
		log.Printf("Failed to sign execution telemetry for task %s: %v", taskID, err)
	} else {
		req.ExecSignature = sig
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := d.client.ReportResult(reqCtx, req)
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
