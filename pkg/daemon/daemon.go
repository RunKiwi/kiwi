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
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/session"
	"github.com/ibreakthecloud/kiwi/pkg/telemetry"
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
	// MaxRounds caps Architect/Implementer rounds per task; 0 uses the session
	// default. A round is a whole agentic pass over the repository, not a single
	// model call — the number is small for that reason.
	MaxRounds int
	// SessionBudgetUSD caps provider spend per task; 0 uses the session default.
	//
	// It is a separate field from the retired single-file loop's cap, and the
	// history is worth keeping: that cap defaulted to $0.50 while a session runs
	// a task-long Architect plus an agentic Implementer over several rounds, at
	// $2-4. When one number served both, every session halted on the budget rail
	// around the end of round one.
	SessionBudgetUSD float64
	// RenewInterval configures how often the daemon extends the lease of a
	// running task. Zero uses defaultRenewInterval.
	RenewInterval time.Duration
	// ProgressInterval is how often partial telemetry is flushed to the Control
	// Plane while a task runs. Defaults to 3s when zero. Distinct from
	// RenewInterval, which runs on minutes — far too slow to watch a run by.
	ProgressInterval time.Duration
	// SandboxRuntime configures the OCI runtime for docker sandboxes (e.g. "runsc").
	SandboxRuntime string
	// TelemetryPollInterval governs how often the daemon asks the Control
	// Plane what telemetry polls are due. Zero uses defaultTelemetryPollInterval.
	TelemetryPollInterval time.Duration
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
	newProvider func(creds map[string]string, model, providerID string) (provider.Provider, provider.Critic)

	// telemetryClient is the narrow subset of the Control Plane client that
	// pollTelemetry calls through. It exists as a separate field/interface —
	// rather than widening client above, or typing client itself as an
	// interface — because client is a concrete *Client used throughout this
	// file (Heartbeat, RenewLease, ReportResult, ...) with no existing
	// mocking seam; every daemon test that exercises it does so against a
	// real *Client pointed at an httptest server. Adding one narrow interface
	// for the one new call path is additive and leaves that working pattern
	// untouched. Defaults to the same *Client instance as client in New;
	// swappable in tests via a fake that implements just these two methods.
	telemetryClient telemetryClient
}

// telemetryClient is the Control Plane surface pollTelemetry needs: asking
// what's due and reporting what was found. Satisfied by *Client (see
// client.go's TelemetryDue/TelemetryReport, Task 7) and by a test double in
// daemon_test.go.
type telemetryClient interface {
	TelemetryDue(ctx context.Context, req TelemetryDueReq) (*TelemetryDueRes, error)
	TelemetryReport(ctx context.Context, req TelemetryReportReq) error
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

	c := NewClient(cfg.APIURL)
	return &Daemon{
		config: cfg,
		client: c,
		// Same underlying *Client, narrowed to the telemetry interface. See
		// the telemetryClient field's doc comment for why this is a separate
		// field rather than widening client's type.
		telemetryClient: c,
		gitCache:        cache,
		newProvider:     defaultProvider,
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
func defaultProvider(creds map[string]string, model, providerID string) (provider.Provider, provider.Critic) {
	// The Control Plane resolves the provider against the model catalog and
	// sends it on the spec. Falling back to prefix inference keeps specs written
	// before that field existed working, but inference cannot recognise an
	// aggregator's model ids, so a Kiwi-provided model with no Provider set
	// would be misrouted here.
	prov := providerID
	if prov == "" {
		prov = provider.ProviderOf(model)
	}
	key := creds[provider.CredentialNameFor(prov)]
	if key == "" {
		return nil, nil
	}

	// An OpenAI-compatible provider is served by the OpenAI client pointed at
	// the registry's base URL. Without the URL the client would call
	// api.openai.com with a key that endpoint never issued.
	if spec, ok := provider.SpecFor(prov); ok && spec.Kind == provider.KindOpenAICompatible {
		cp := provider.NewOpenAICompatibleProvider(key, model, model, spec.BaseURL, spec.ID)
		return cp, cp
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

	// The telemetry poll is daemon-lifetime-scoped like the heartbeat itself
	// — not task-scoped — so it lives in this same top-level select rather
	// than a per-task goroutine. See defaultTelemetryPollInterval's doc
	// comment for why it runs on its own, much longer, interval.
	telemetryInterval := d.config.TelemetryPollInterval
	if telemetryInterval <= 0 {
		telemetryInterval = defaultTelemetryPollInterval
	}
	telemetryTimer := time.NewTimer(telemetryInterval)
	defer telemetryTimer.Stop()

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
		case <-telemetryTimer.C:
			pollCtx, cancel := context.WithTimeout(ctx, telemetryPollBudget)
			d.pollTelemetry(pollCtx)
			cancel()
			telemetryTimer.Reset(telemetryInterval)
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
				interval = defaultRenewInterval
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

// defaultRenewInterval is how often a running task's lease is extended, and now
// also how often a busy daemon proves it is alive.
//
// It must stay comfortably under TWO separate deadlines, and it used to respect
// only one of them. The lease is 10 minutes (leaseTTL in pkg/orchestrator), so
// any interval well under that keeps the task. But the Control Plane reports a
// daemon offline after 3 minutes without contact (daemonStaleAfter in
// pkg/store/queue_diagnose.go), and the old 4-minute interval was longer than
// that window — so a daemon doing nothing wrong looked dead between renewals.
//
// Two minutes sits inside both, and the shorter interval buys a second thing
// worth having: five renewal attempts per lease instead of two, so a transient
// failure no longer needs to be a near-miss.
const defaultRenewInterval = 2 * time.Minute

// defaultTelemetryPollInterval governs how often this daemon asks the
// Control Plane what telemetry polls are due. Independent of the 5s
// heartbeat and the per-task 2min renewal — this is daemon-lifetime-scoped
// like the heartbeat itself, not task-scoped, so it lives in Run's top-level
// select rather than a per-task goroutine. Telemetry queries look at
// baseline vs. current windows measured in minutes to hours, so polling much
// faster than this would not surface anything new, and 1 minute keeps the
// Control Plane's due-check cheap and infrequent relative to the 5s
// heartbeat it shares a client identity with.
const defaultTelemetryPollInterval = 1 * time.Minute

// telemetryPollBudget bounds one whole pollTelemetry pass. It runs inline in
// Run's single top-level select, so an unbounded pass freezes the 5s
// heartbeat for its whole duration: a full batch (the Control Plane's
// telemetryDueLimit of 20 specs, two serial queries each at the connectors'
// 15s client timeout) is ~10 minutes of stall in the worst case. The bound
// also keeps a pass comfortably shorter than the Control Plane's
// pollStaleClaimAfter, so the orchestrator's stale-claim sweep cannot
// release a claim out from under a pass that is still running and have the
// same poll queried and reported twice. Both telemetry connectors build
// their requests with http.NewRequestWithContext, so expiry here actually
// aborts an in-flight query rather than being ignored.
const telemetryPollBudget = 2 * time.Minute

// pollTelemetry asks the Control Plane what's due and, if anything is,
// decrypts the credential bundle carried on that same response — sealed
// fresh by the Control Plane for this call, not reused from a heartbeat,
// because an idle daemon between polls (the routine state for post-merge
// telemetry) may never see a heartbeat that carries credentials at all. It
// then executes each query against the org's configured telemetry provider
// and reports results back in one batch. Every failure is logged, never
// silently swallowed — a telemetry poll going quiet with no trace is exactly
// the failure mode this project's earlier security review and
// revert-detection fix both exist to avoid repeating.
func (d *Daemon) pollTelemetry(ctx context.Context) {
	req := TelemetryDueReq{
		SignPubKey: base64.StdEncoding.EncodeToString(d.signPubKey),
		Timestamp:  time.Now().Unix(),
	}
	res, err := d.telemetryClient.TelemetryDue(ctx, req)
	if err != nil {
		log.Printf("[telemetry] due check failed: %v", err)
		return
	}
	if res == nil || len(res.Due) == 0 {
		return
	}

	creds, err := d.openCredentials(res.EncryptedCreds)
	if err != nil {
		log.Printf("[telemetry] failed to open sealed credentials: %v", err)
		return
	}

	results := make([]TelemetryPollResult, 0, len(res.Due))
	for _, spec := range res.Due {
		prov, err := telemetry.ProviderFor(spec.Provider, creds)
		if err != nil {
			log.Printf("[telemetry] poll %s: %v", spec.PollID, err)
			results = append(results, TelemetryPollResult{PollID: spec.PollID, Error: err.Error()})
			continue
		}

		baseline, baseErr := prov.Query(ctx, spec.Query, spec.BaselineStart, spec.BaselineEnd)
		current, curErr := prov.Query(ctx, spec.Query, spec.CurrentStart, spec.CurrentEnd)
		result := TelemetryPollResult{PollID: spec.PollID}
		if baseErr != nil || curErr != nil {
			msg := ""
			if baseErr != nil {
				msg += "baseline: " + baseErr.Error() + "; "
			}
			if curErr != nil {
				msg += "current: " + curErr.Error()
			}
			log.Printf("[telemetry] poll %s query failed: %s", spec.PollID, msg)
			result.Error = msg
		} else {
			result.Baseline = &TelemetryResultDTO{SampleCount: baseline.SampleCount, Mean: baseline.Mean}
			result.Current = &TelemetryResultDTO{SampleCount: current.SampleCount, Mean: current.Mean}
		}
		results = append(results, result)
	}

	if err := d.telemetryClient.TelemetryReport(ctx, TelemetryReportReq{
		SignPubKey: base64.StdEncoding.EncodeToString(d.signPubKey),
		Timestamp:  time.Now().Unix(),
		Results:    results,
	}); err != nil {
		log.Printf("[telemetry] report failed: %v", err)
	}
}

// isLLMKey reports whether a credential is a model API key that must be kept out
// of the sandbox environment.
//
// It delegates to the provider registry, which is the one place a provider is
// defined. That is what makes the guarantee hold: a provider added without a
// registry row has no credential name at all, and one added with a row is
// covered here automatically. TestIsLLMKeyCoversEveryRegistryProvider asserts
// the two cannot drift.
func isLLMKey(name string) bool {
	return provider.IsLLMCredential(name)
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
		return taskResult{detail: "invalid task ID format", events: prog.all()}
	}

	if spec.File != "" && !filepath.IsLocal(spec.File) {
		log.Printf("Task %s: file path %q escapes worktree", spec.ID, spec.File)
		return taskResult{detail: "file path escapes worktree", events: prog.all()}
	}
	for _, f := range spec.Files {
		if !filepath.IsLocal(f) {
			log.Printf("Task %s: file path %q escapes worktree", spec.ID, f)
			return taskResult{detail: "file path escapes worktree", events: prog.all()}
		}
	}

	worktreePath := filepath.Join(d.config.CacheDir, "worktrees", spec.ID)

	if spec.RepoURL != "" && spec.Ref != "" {
		// One job = one branch (#126): base the worktree on the shared job branch
		// when it already exists, so this worker sees earlier workers' committed
		// edits and its commit fast-forwards onto them. The first worker falls
		// back to spec.Ref.
		jobBranch := jobBranchName(spec)
		log.Printf("Provisioning worktree for %s (ref: %s, job branch: %s)...", spec.ID, spec.Ref, jobBranch)
		// The same credential that publishes the result also reads the repo. It
		// was previously applied only on push, so a private repo failed here
		// with git's "could not read Username" — a message that names no
		// credential. Cloning happens in the daemon, never in the sandbox, so
		// this does not put the token anywhere model-generated code can see it.
		//
		// Resolved per operation rather than once per task: an installation
		// token expires within the hour, and the push at the end of a long run
		// must not inherit one minted at the start.
		cloneToken, err := d.resolveGitToken(ctx, spec.ID, leaseID, creds)
		if err != nil {
			log.Printf("Failed to resolve git credential for task %s: %v", spec.ID, err)
			return taskResult{detail: truncateDetail(err.Error()), events: prog.all()}
		}
		if err := reportSetupPhase(prog, "clone", "clone: "+spec.RepoURL, spec.RepoURL, func() error {
			return d.gitCache.GetJobWorktree(ctx, spec.RepoURL, spec.Ref, jobBranch, worktreePath,
				gitcache.WithToken(cloneToken))
		}); err != nil {
			log.Printf("Failed to provision worktree for task %s: %v", spec.ID, err)
			return taskResult{detail: "failed to provision worktree", events: prog.all()}
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
			return taskResult{detail: "failed to create fallback sandbox dir", events: prog.all()}
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
		MemoryLimit: sandboxMemoryLimit(),
		CPULimit:    "1.0",
		Runtime:     d.config.SandboxRuntime,
		NetworkNone: true,
	}
	sandboxCtx := context.WithValue(taskCtx, sandbox.SandboxConfigKey, sandboxCfg)

	// Test-command environment: every credential except the LLM keys.
	//
	// Session mode withholds them all by default. The exclusion of LLM keys
	// exists because the sandbox runs model-generated code; the wider exclusion
	// exists because in session mode the model also chooses the *commands*, and
	// their output travels back into the event log. A credential that can be
	// read and echoed does not need a network to escape. See
	// sessionAllowsTestCredentials for the opt-in and what it costs.
	testEnv := taskTestEnv(spec.Task, creds, true)
	if !sessionAllowsTestCredentials() {
		log.Printf("Task %s: session mode — withholding all credentials from the sandbox", spec.ID)
	}

	// Build the Implementer's provider (daemon-side, not in the sandbox). It is
	// selected from the worker's model; its key is picked from the bundle.
	actor, _ := d.newProvider(creds, spec.Model, spec.Provider)

	// actor == nil means the model's provider has no key in this org's sealed
	// bundle (e.g. a gemini-* model but no GEMINI_API_KEY). That is a
	// configuration error — fail it with a precise reason instead of papering
	// over it with a run that pretends to succeed.
	if actor == nil {
		reason := fmt.Sprintf("no API key configured for the %s provider that model %q needs — add it under Integrations",
			providerNameForModel(spec.Model), spec.Model)
		log.Printf("Task %s: %s", spec.ID, reason)
		return taskResult{detail: reason, events: prog.all()}
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

	if testCmd == "" {
		return taskResult{detail: "no test command, and none could be inferred from the repo — set one under Advanced options so the fix can be verified", events: prog.all()}
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
		var installDetail string
		err := reportSetupPhase(prog, "install", "install: "+step.Command, step.Command, func() error {
			var ok bool
			installDetail, ok = d.installDependencies(taskCtx, worktreePath, sandboxCfg, step, spec.ID, cacheEnv)
			if !ok {
				return errors.New(installDetail)
			}
			return nil
		})
		if err != nil {
			return taskResult{detail: installDetail, events: prog.all()}
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
	// Set when the sandbox stopped existing. Reported instead of a test result,
	// because it is not one: see sandbox_lost.go.
	sandboxGone := ""

	// verifyBox lets session mode run the test command inside the container it
	// already has, instead of paying a fresh `docker run` for every round.
	//
	// The single-file loop leaves this nil and keeps its one-shot behaviour: it
	// has no persistent container to reuse. Session mode sets it once the box
	// exists (see executeSession), which is after this closure is built — hence
	// the pointer rather than a parameter.
	//
	// It is dropped on image correction. The box was started from an image
	// chosen before anything ran, so if that guess turns out to be wrong the
	// running container is wrong too, and the corrected re-run has to go back
	// through RunCommand — which builds a container per call and can therefore
	// use the new image. Correction happens at most once per task, so the
	// fallback costs a container start in the rare case and saves one per round
	// in the common one.
	var verifyBox *sandbox.Session
	runInSandbox := func(ctx context.Context) (*sandbox.Result, error) {
		if verifyBox != nil {
			return verifyBox.Exec(ctx, testCmd, testEnv)
		}
		return sandbox.RunCommand(sandboxCtx, worktreePath, testCmd, testEnv)
	}
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
		res, err := runInSandbox(ctx)
		if err != nil {
			return "", false, err
		}
		prog.setActivity("test: "+testCmd, res.Output)

		// The sandbox is gone. One retry in a fresh per-call container, because
		// the usual cause is a box that died earlier in the session and a new
		// one may well work — the same fall-back the image repair below uses.
		//
		// If that fails too, stop. Every further round would be verified by an
		// error message, and the model cannot act on one: on
		// job_e3a491f48809d606 that cost two rounds and $3 before a no-progress
		// rail halted the session and blamed the agent.
		if why := sandboxLost(res.Output); why != "" {
			log.Printf("Task %s: sandbox lost: %s", spec.ID, why)
			if verifyBox != nil {
				verifyBox = nil
				if retry, rerr := sandbox.RunCommand(sandboxCtx, worktreePath, testCmd, testEnv); rerr == nil {
					res = retry
					prog.setActivity("test: "+testCmd, res.Output)
				}
			}
			if why := sandboxLost(res.Output); why != "" {
				sandboxGone = why
				return "", false, errors.New("the sandbox is gone; verification cannot run")
			}
		}

		if !res.Success && !envChecked {
			envChecked = true
			if next, why := correctedImage(sandboxCfg.DockerImage, res.Output, worktreePath); next != "" {
				log.Printf("Task %s: %s; retrying in %s instead of %s",
					spec.ID, why, next, sandboxCfg.DockerImage)
				sandboxCfg.DockerImage = next
				// The running box is on the image we just rejected, so it cannot
				// serve the retry. Fall back to a per-call container from here on.
				verifyBox = nil
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
	res := d.executeSession(taskCtx, spec, creds, prog, sessionDeps{
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
		useBox:  func(b *sandbox.Session) { verifyBox = b },
	})
	// A repository whose verification cannot run offline is a fact about the
	// repository, not a failure of the run. Report it verbatim rather than
	// letting the session's generic "verification failed" stand in for it.
	// A repository whose verification cannot run offline, or a sandbox that
	// stopped existing, are both facts about the run rather than failures of the
	// work. Report either verbatim instead of letting the session's generic
	// "verification failed" stand in for it.
	if sandboxGone != "" && !res.ok {
		res.detail = sandboxGone
	} else if offlineBlocked != "" && !res.ok {
		res.detail = offlineBlocked
	}
	return res
}

// reportSetupPhase runs fn, reporting it as the live activity before it starts
// and recording one durable Step-0 event with its outcome and duration once it
// finishes.
//
// Setup (clone, initial dependency install) happens before session.Runner
// exists, so it is the one part of a run with no OnEvent/OnActivity of its
// own to hook — this gives it the same live-plus-historical signal every
// later phase already has via those. fallbackDetail is what a SUCCESSFUL run
// records, since a clean install has nothing more informative to say than the
// command itself; a failing fn's own error message is used instead, since
// that names what actually went wrong.
func reportSetupPhase(prog *progressReporter, phase, activity, fallbackDetail string, fn func() error) error {
	prog.setActivity(activity, "")
	start := time.Now()
	err := fn()
	outcome, detail := "ok", fallbackDetail
	if err != nil {
		outcome, detail = "error", err.Error()
	}
	prog.add(ver.TaskEvent{
		Step:       0,
		Phase:      phase,
		Outcome:    outcome,
		Detail:     detail,
		DurationMs: time.Since(start).Milliseconds(),
	})
	return err
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
