package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/session"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/telemetry"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// sessionDeps is everything executeTask has already built that a session run
// needs. Passing it as a struct keeps the branch in executeTask to one line and
// means the two paths cannot drift on sandbox configuration.
type sessionDeps struct {
	// leaseID fences every checkpoint, exactly as it fences a result: a daemon
	// that lost the task must not write over the run that replaced it.
	leaseID      string
	worktreePath string
	sandboxCfg   *sandbox.SandboxConfig
	// execEnv is the environment for the Implementer's own shell. It carries the
	// toolchain cache variables and nothing else — see sessionTestEnv.
	execEnv []string
	testCmd string
	verify  session.VerifyFunc
	install session.InstallFunc
	// useBox hands the round's persistent container back to the verification
	// closure, so the test command runs inside the container the session
	// already has rather than in a new one per round. Nil leaves verification
	// on its own per-call container.
	useBox func(*sandbox.Session)
}

// sessionAllowsTestCredentials reports whether the org has opted into putting
// credentials in a session's sandbox.
//
// Off by default, and this is the one place session mode is deliberately less
// capable than the retired single-file loop. There, the sandbox ran one fixed
// command supplied by the user, so a credential in its environment could be read
// but not sent anywhere: the network is off. Here the model chooses the commands
// and their output comes back to the daemon, into the event log and on to the
// Control Plane — so `echo $GIT_TOKEN` is an exfiltration path that needs no
// network at all.
//
// Repositories whose tests genuinely need a secret can set this and accept that.
// What is not acceptable is making that trade silently on a customer's behalf.
func sessionAllowsTestCredentials() bool {
	v, _ := strconv.ParseBool(os.Getenv("KIWI_SESSION_ALLOW_TEST_CREDS"))
	return v
}

// sessionLimits is the per-task round and spend cap for session mode.
//
// It exists as a seam because the spend cap once read the retired single-file
// loop's field instead of this one. That default was $0.50, a session costs
// $2-4, and the two were close enough to look plausible and far enough apart to
// halt every session on the budget rail in round one. Reading the wrong field is
// the regression worth a test, so the read has a name.
//
// Zero is passed through rather than defaulted here; session.Config applies its
// own defaults, and duplicating them would give two places to disagree.
func (d *Daemon) sessionLimits() (rounds int, budgetUSD float64) {
	return d.config.MaxRounds, d.config.SessionBudgetUSD
}

// executeSession runs a task through the agentic Architect/Implementer loop.
//
// The trust boundary is unchanged from the single-file path and slightly
// stronger: both models run in the daemon process holding the keys, the
// sandbox executes only what they ask for, and in this mode the sandbox holds
// no credentials at all. Git stays in the daemon, so GIT_TOKEN never enters a
// container.
func (d *Daemon) executeSession(ctx context.Context, spec agent.WorkerSpec, creds map[string]string, prog *progressReporter, deps sessionDeps) taskResult {
	architectModel := spec.ArchitectModel
	if architectModel == "" {
		architectModel = spec.Model
	}

	// The Implementer needs tool support; the Architect needs only Complete.
	// They are resolved separately because they are usually different models and
	// may be different providers — the routing rule is the same one the rest of
	// Kiwi uses, so a model's key is found the same way here as anywhere else.
	workerProv, _ := d.newProvider(creds, spec.Model, spec.Provider)
	if workerProv == nil {
		return taskResult{detail: noKeyDetail(spec.Model)}
	}
	implementer, ok := provider.AsToolRunner(workerProv)
	if !ok {
		// There is no non-agentic loop to fall back to any more, and inventing
		// one would be worse than saying so: the model cannot do the job asked
		// of it, and the answer is a different model.
		return taskResult{detail: fmt.Sprintf(
			"model %q cannot use tools, which every Kiwi task requires — choose a tool-capable model",
			spec.Model)}
	}

	// The Architect gets its own resolved provider, because it is usually a
	// different model from a different provider than the Implementer. When the
	// spec carries no ArchitectModel the Architect runs the worker's model, so
	// it inherits the worker's provider too.
	architectProvider := spec.ArchitectProvider
	if spec.ArchitectModel == "" {
		architectProvider = spec.Provider
	}
	architectProv, _ := d.newProvider(creds, architectModel, architectProvider)
	if architectProv == nil {
		return taskResult{detail: noKeyDetail(architectModel)}
	}

	base := &gitWorkspace{root: deps.worktreePath}
	baseSHA, err := base.HeadSHA(ctx)
	if err != nil {
		return taskResult{detail: fmt.Sprintf("could not read the repository head: %v", err)}
	}
	base.base = baseSHA

	// A persistent container for the round's shell commands. One container for
	// the whole task rather than one per command: a round issues dozens.
	box, err := sandbox.NewSession(ctx, deps.worktreePath, deps.sandboxCfg, sandbox.SessionOpts{})
	if err != nil {
		return taskResult{detail: fmt.Sprintf("could not start the sandbox: %v", err)}
	}
	defer func() {
		if cerr := box.Close(); cerr != nil {
			log.Printf("Task %s: could not remove the sandbox container: %v", spec.ID, cerr)
		}
	}()
	// Recorded once, here, rather than read from box wherever a taskResult is
	// built below: box.ProvisionMs() is fixed the moment the container starts
	// and every return path below wants the same number. sandboxImage travels
	// with it so an admin breakdown can bucket by ecosystem (the image's own
	// prefix — "golang:", "node:", "python:" — names it) without a second
	// column enumerating ecosystems that would drift from runtime.go's list.
	sandboxProvisionMs := int64Ptr(box.ProvisionMs())
	sandboxImage := deps.sandboxCfg.DockerImage
	prog.add(ver.TaskEvent{Phase: "sandbox_provision", Outcome: "ok", DurationMs: box.ProvisionMs()})

	// Verification joins the Implementer inside this container. Both were
	// already running the same image, mounts and offline network policy; the
	// only difference was that verification paid to create a container each
	// time, once per round plus the baseline.
	if deps.useBox != nil {
		deps.useBox(box)
	}

	tools := &session.FileTools{
		Root: deps.worktreePath,
		Exec: func(ctx context.Context, command string) (string, bool, error) {
			prog.setActivity("run: "+command, "")
			res, err := box.Exec(ctx, command, deps.execEnv)
			if err != nil {
				return "", false, err
			}
			prog.setActivity("run: "+command, res.Output)
			return res.Output, res.Success, nil
		},
		Install: deps.install,
	}

	// Durable state. Without it a crashed daemon restarts the task from the base
	// commit; with it, the task resumes at its last finished round. The daemon
	// has no database, so this travels over the same signed, lease-fenced
	// channel as every other daemon report.
	var sessionStore session.Store
	if deps.leaseID != "" {
		sessionStore = &cpSessionStore{
			client:         d.client,
			taskID:         spec.ID,
			leaseID:        deps.leaseID,
			signPubKey:     base64.StdEncoding.EncodeToString(d.signPubKey),
			jobID:          spec.JobID,
			repoURL:        spec.RepoURL,
			branch:         jobBranchName(spec),
			architectModel: architectModel,
			workerModel:    spec.Model,
			maxRounds:      d.config.MaxRounds,
		}
	}

	rounds, budget := d.sessionLimits()

	runner := &session.Runner{
		Store:            sessionStore,
		SessionID:        sessionIDFor(spec.ID),
		Architect:        &session.LLMArchitect{Provider: architectProv, Model: architectModel, Tools: session.NewArchitectTools(deps.worktreePath)},
		Implementer:      implementer,
		ImplementerModel: spec.Model,
		Tools:            tools,
		Workspace:        base,
		Verify:           deps.verify,
		Config: session.Config{
			MaxRounds:        rounds,
			SessionBudgetUSD: budget,
			SessionDeadline:  sessionDeadlineFor(spec),
			Log:              func(format string, a ...any) { log.Printf("task "+spec.ID+": "+format, a...) },
			// What is running right now. The sandbox paths set this themselves
			// (install, test, the run tool); this covers the model calls, which
			// nothing else can see and which are the longest silent stretches
			// of a session. Output is empty because a model call produces none
			// until it returns — and passing the previous command's tail would
			// reproduce the staleness this exists to fix.
			OnActivity: func(a string) { prog.setActivity(a, "") },
			OnEvent: func(e session.Event) {
				prog.add(ver.TaskEvent{
					Step:         e.Round,
					Phase:        sessionPhase(e),
					Outcome:      e.Outcome,
					Detail:       e.Detail,
					Input:        e.Input,
					DurationMs:   e.DurationMs,
					InputTokens:  e.InputTokens,
					OutputTokens: e.OutputTokens,
					CostUSD:      e.CostUSD,
				})
			},
		},
	}

	description := spec.Task
	repoCtx := repoContext(deps.worktreePath)
	if repoCtx != "" {
		log.Printf("Task %s: injecting repo AGENT.md context (%d bytes)", spec.ID, len(repoCtx))
	}

	log.Printf("Running session loop for task %s (architect %s, implementer %s, test %q)...",
		spec.ID, architectModel, spec.Model, deps.testCmd)

	res, err := runner.Run(ctx, session.Task{
		ID:                   spec.ID,
		Description:          description,
		TestCmd:              deps.testCmd,
		InvestigationOnly:    spec.InvestigationOnly,
		RepoContext:          repoCtx,
		Learnings:            spec.Learnings,
		RequiresPlanApproval: spec.RequiresPlanApproval,
		RevisionFeedback:     spec.RevisionFeedback,
	})

	if res.PlanPendingReview {
		specJSON, merr := json.Marshal(res.Spec)
		if merr != nil {
			log.Printf("Task %s: failed to marshal plan spec: %v", spec.ID, merr)
			specJSON = []byte("{}")
		}
		return taskResult{
			detail:             "plan pending review",
			events:             prog.all(),
			planReviewStatus:   store.TaskPlanReview,
			planSpecJSON:       string(specJSON),
			cachedPromptTokens: int64Ptr(res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens),
			rawPromptTokens:    int64Ptr(res.Usage.InputTokens),
			sandboxProvisionMs: sandboxProvisionMs,
			sandboxImage:       sandboxImage,
		}
	}

	if err != nil {
		log.Printf("Task %s session ended without success: %v (rounds=%d, cost=$%.2f)", spec.ID, err, res.Rounds, res.CostUSD)
	} else {
		log.Printf("Task %s session complete: success=%v (rounds=%d, cost=$%.2f)", spec.ID, res.Success, res.Rounds, res.CostUSD)
	}

	// A session that times out having completed no round looks exactly like the
	// cryptomining case the single-file path flags: wall clock consumed, nothing
	// produced. Rounds replace steps as the unit, since a session's first round
	// is a substantial piece of work rather than one model call.
	abuse := ctx.Err() == context.DeadlineExceeded && res.Rounds < 1

	if !res.Success {
		detail := res.Detail
		if detail == "" && err != nil {
			if kind, reason := provider.Classify(err); kind != provider.ErrOther {
				detail = fmt.Sprintf("%s: %s", providerNameForModel(spec.Model), reason)
			} else {
				detail = truncateDetail(err.Error())
			}
		}
		return taskResult{
			detail:             truncateDetail(detail),
			abuse:              abuse,
			events:             prog.all(),
			cachedPromptTokens: int64Ptr(res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens),
			rawPromptTokens:    int64Ptr(res.Usage.InputTokens),
			sandboxProvisionMs: sandboxProvisionMs,
			sandboxImage:       sandboxImage,
		}
	}

	if out, matched := investigationOutcome(res); matched {
		out.events = prog.all()
		out.sandboxProvisionMs = sandboxProvisionMs
		out.sandboxImage = sandboxImage
		return out
	}

	gitToken, gitErr := d.resolveGitToken(ctx, spec.ID, deps.leaseID, creds)
	if gitErr != nil {
		return taskResult{
			detail:             truncateDetail(gitErr.Error()),
			events:             prog.all(),
			cachedPromptTokens: int64Ptr(res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens),
			rawPromptTokens:    int64Ptr(res.Usage.InputTokens),
			sandboxProvisionMs: sandboxProvisionMs,
			sandboxImage:       sandboxImage,
		}
	}
	if gitToken == "" {
		return taskResult{
			detail:             "no GIT_TOKEN; skipped PR",
			events:             prog.all(),
			cachedPromptTokens: int64Ptr(res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens),
			rawPromptTokens:    int64Ptr(res.Usage.InputTokens),
			sandboxProvisionMs: sandboxProvisionMs,
			sandboxImage:       sandboxImage,
		}
	}

	gh := &restGitHub{token: gitToken}
	prURL, detail, perr := publishResultFrom(ctx, deps.worktreePath, spec, res.Summary, gitToken, gh, "", baseSHA)
	switch {
	case errors.Is(perr, errNoChanges):
		// The reviewer is instructed never to approve an empty diff, and refuses
		// to when asked; arriving here means something else went wrong, and a
		// green tick with no pull request is the one outcome worse than failing.
		return taskResult{
			detail:             "the session was approved but left the repository unchanged, so there is nothing to open a PR with",
			events:             prog.all(),
			cachedPromptTokens: int64Ptr(res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens),
			rawPromptTokens:    int64Ptr(res.Usage.InputTokens),
			sandboxProvisionMs: sandboxProvisionMs,
			sandboxImage:       sandboxImage,
		}
	case perr != nil:
		return taskResult{
			detail:             truncateDetail(fmt.Sprintf("publish failed: %v", perr)),
			events:             prog.all(),
			cachedPromptTokens: int64Ptr(res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens),
			rawPromptTokens:    int64Ptr(res.Usage.InputTokens),
			sandboxProvisionMs: sandboxProvisionMs,
			sandboxImage:       sandboxImage,
		}
	}
	if detail == "" {
		detail = res.Summary
	}
	return taskResult{
		ok:                 true,
		prURL:              prURL,
		detail:             truncateDetail(detail),
		events:             prog.all(),
		cachedPromptTokens: int64Ptr(res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens),
		rawPromptTokens:    int64Ptr(res.Usage.InputTokens),
		sandboxProvisionMs: sandboxProvisionMs,
		sandboxImage:       sandboxImage,
	}
}

// int64Ptr returns a pointer to an int64, for building cache-usage telemetry.
func int64Ptr(v int64) *int64 { return &v }

// sessionPhase maps a session event onto the execution record's phase
// vocabulary. The record already understands the single-file loop's phases, and
// a consumer that has never heard of rounds should still be able to read a
// session run rather than seeing every event as "unknown".
func sessionPhase(e session.Event) string {
	switch e.Phase {
	case "verify":
		return "test"
	case "plan", "review":
		return "critic"
	case "tool":
		if e.Tool != "" {
			return "actor:" + e.Tool
		}
		return "actor"
	default:
		return e.Phase
	}
}

// sessionDeadlineFor derives the session wall-clock cap from the task timeout
// the Control Plane already sets per org. A session needs a longer default than
// a single-file run, but it must not be able to exceed the cap the org's limits
// express — that cap is what stops a runaway from billing agent-minutes forever.
func sessionDeadlineFor(spec agent.WorkerSpec) time.Duration {
	if spec.TimeoutSeconds > 0 {
		return time.Duration(spec.TimeoutSeconds) * time.Second
	}
	return 0 // let pkg/session apply its own default
}

func noKeyDetail(model string) string {
	return fmt.Sprintf("no API key configured for the %s provider that model %q needs — add it under Integrations",
		providerNameForModel(model), model)
}

// slackWebhookCredentialName is the Control-Plane-only Slack notification
// credential (ee/orchestrator's notifySlackVerdict — see pkg/store's
// CredentialWebhook kind). SealCredentialsForDaemon bundles every org
// credential regardless of Kind, so this name-literal exclusion is what
// actually keeps it out of the sandbox test-command environment; nothing in
// the daemon ever reads or forwards it otherwise.
const slackWebhookCredentialName = "SLACK_WEBHOOK_URL"

// slackBotTokenCredentialName is the Control-Plane-only Slack trigger
// credential (ee/orchestrator's Slack @mention pipeline — see pkg/store's
// CredentialSlack kind). It can post/edit messages and read channel and
// thread history, so it gets the same unconditional exclusion as
// slackWebhookCredentialName rather than being left to the opt-in below.
const slackBotTokenCredentialName = "SLACK_BOT_TOKEN"

// taskTestEnv builds the environment the sandbox runs with.
//
// Five exclusions, for two different reasons. LLM keys, telemetry
// credentials (Datadog/Prometheus — pkg/telemetry), and the two
// Control-Plane-only Slack credentials are always withheld because the
// sandbox executes model-generated code, and that has been true for LLM
// keys since the Actor/Critic split; telemetry and Slack credentials are org
// infrastructure/notification secrets with the same exposure, so they get
// the same unconditional treatment rather than being left to the opt-in
// below. Everything else is withheld in session mode because there the
// model also chooses the commands, and their output is carried back into
// the event log — so a credential in the environment has a read-and-echo
// path out that needs no network.
func taskTestEnv(task string, creds map[string]string, sessionMode bool) []string {
	env := []string{"TASK=" + task}
	if sessionMode && !sessionAllowsTestCredentials() {
		return env
	}
	for name, value := range creds {
		if isLLMKey(name) || telemetry.IsTelemetryCredential(name) || name == slackWebhookCredentialName || name == slackBotTokenCredentialName {
			continue
		}
		env = append(env, name+"="+value)
	}
	return env
}

// investigationOutcome reports the taskResult for a successful round the
// Architect explicitly declared needs no diff, or (matched=false) says
// nothing — meaning the caller should fall through to the ordinary
// PR-publish path. Kept pure and separate from runSession's imperative flow
// so the decision itself — not the git/session plumbing around it — is what
// a test exercises.
func investigationOutcome(res session.Result) (taskResult, bool) {
	if !res.Success || !res.NoDiffExpected {
		return taskResult{}, false
	}
	return taskResult{
		ok:                 true,
		detail:             truncateDetail(res.Summary),
		cachedPromptTokens: int64Ptr(res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens),
		rawPromptTokens:    int64Ptr(res.Usage.InputTokens),
	}, true
}
