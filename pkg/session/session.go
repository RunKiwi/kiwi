// Package session is Kiwi's agentic execution loop: one persistent Architect
// that plans a task and reviews every round of it, and a tool-using Implementer
// that carries each round out against the real repository.
//
// It is the counterpart to pkg/loop, not a replacement for it, and it keeps
// that package's discipline about dependencies: everything context-specific —
// how commands reach a sandbox, where the repository lives, how credentials are
// obtained — is injected by the caller. This package imports pkg/provider and
// the standard library, so it can be driven from the daemon, from a test, or
// from the control plane without any of them importing each other.
//
// The shape of a run:
//
//	Architect plans ──▶ Implementer round ──▶ verify ──▶ Architect reviews ──┐
//	      ▲                                                                  │
//	      └──────────────────── revise ─────────────────────────────────────┘
//
// The Implementer starts each round with a fresh context. That is not a
// limitation being worked around, it is the design: its transcript is a stale
// cache of a filesystem that it is itself editing, while the worktree and the
// job branch are current and durable. Making the round self-contained is also
// what makes it restartable, which is what lets a long session live on a lease
// queue built for disposable units of work.
package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// ErrNoChanges reports that a commit found nothing to commit. It mirrors the
// daemon's own errNoChanges: a round that changes nothing has produced nothing
// to deliver, and that is never success.
var ErrNoChanges = errors.New("session: no changes to commit")

// Workspace is the repository the session works in. The session never runs git
// itself — git is the one operation that needs the credential the sandbox is
// not allowed to hold, so it stays with the caller.
type Workspace interface {
	// Tree lists the repository's files, relative to its root.
	Tree(ctx context.Context) ([]string, error)
	// Diff returns the accumulated diff for the whole task.
	Diff(ctx context.Context) (string, error)
	// FilesChanged lists the paths the task has touched so far.
	FilesChanged(ctx context.Context) ([]string, error)
	// Commit records the working tree, returning the new head. It returns
	// ErrNoChanges when there is nothing to record.
	Commit(ctx context.Context, message string) (string, error)
	// HeadSHA reports the current head.
	HeadSHA(ctx context.Context) (string, error)
	// Reset discards working-tree changes back to sha.
	Reset(ctx context.Context, sha string) error
}

// VerifyFunc runs the task's verification command. Its contract is loop's
// TestFunc verbatim: err is for a broken sandbox, a failing test is
// (output, false, nil).
type VerifyFunc func(ctx context.Context) (output string, passed bool, err error)

// Task is one unit of work: the whole request, from prompt to pull request.
type Task struct {
	ID          string
	Description string
	TestCmd     string
	// RepoContext is the repository's own AGENT.md, if it has one.
	RepoContext string
	// Learnings are summaries of prior jobs on this repository, resolved by the
	// control plane. They reach the Architect here because the control plane no
	// longer plans and so no longer consumes them itself.
	Learnings []string
}

// Config tunes the session's rails. Zero values get defaults.
//
// The single-file loop's rails — six steps, fifty cents, three identical
// failures, three rejections — assume a short bounded loop. An open-ended
// agentic session needs the same guarantees expressed at two scales, because
// there are now two loops: rounds, and tool calls within a round.
type Config struct {
	MaxRounds                int
	MaxToolCallsPerRound     int
	MaxConsecutiveToolErrors int
	MaxRejections            int
	RoundBudgetUSD           float64
	SessionBudgetUSD         float64
	RoundDeadline            time.Duration
	SessionDeadline          time.Duration
	// NoCache turns prompt caching OFF for the Implementer's conversation.
	//
	// Inverted deliberately, unlike provider.ConversationOpts.Cache. At the
	// transport layer opt-in is right: a provider must not write cache entries a
	// caller did not ask for. Here the zero value is a policy default, and the
	// right default is on — a tool round re-sends its transcript every turn, so
	// caching is roughly the difference between a $5 round and a $0.70 one, and
	// a caller that forgets a field should not pay seven times over for it.
	NoCache bool
	// CompactAt is the transcript token size above which a round compacts.
	// Negative disables compaction; zero uses the default.
	CompactAt int64

	Log     func(format string, a ...any)
	OnEvent func(Event)
}

// Defaults. Every number here is a starting point chosen from the arithmetic in
// the RFC, not from evidence; they are the two knobs most worth setting from a
// real evaluation rather than from an author's judgment.
const (
	defaultMaxRounds                = 4
	defaultMaxToolCallsPerRound     = 60
	defaultMaxConsecutiveToolErrors = 5
	defaultMaxRejections            = 3
	defaultRoundBudgetUSD           = 1.50
	defaultSessionBudgetUSD         = 5.00
	defaultRoundDeadline            = 15 * time.Minute
	defaultSessionDeadline          = 90 * time.Minute
	// defaultCompactAt is where a round's transcript is summarised and restarted.
	// Well below any current model's context window on purpose: the aim is to
	// stop paying to re-send exploration the model has already drawn its
	// conclusions from, not to rescue a round about to overflow.
	defaultCompactAt = 100_000
	// compactKeepResults is how many recent tool results survive compaction
	// verbatim. The most recent output is what the model is actually working
	// from; older reads are what the summary is for.
	compactKeepResults = 8

	// dupCommandWarn is how many identical (command, output) pairs in a round
	// earn an explicit warning, and dupCommandHalt how many end the round.
	//
	// Warning before halting is the deliberate difference from the single-file
	// loop, which can only stop. A model told "you have run this exact command
	// three times and got the same output" frequently changes approach; the old
	// loop never gets to say so, because its Actor has no turn to say it in.
	dupCommandWarn = 3
	dupCommandHalt = 5

	// noProgressHalt stops the session when consecutive rounds end in the same
	// place — same tree, same verification output.
	noProgressHalt = 2
)

func (c Config) withDefaults() Config {
	if c.MaxRounds <= 0 {
		c.MaxRounds = defaultMaxRounds
	}
	if c.MaxToolCallsPerRound <= 0 {
		c.MaxToolCallsPerRound = defaultMaxToolCallsPerRound
	}
	if c.MaxConsecutiveToolErrors <= 0 {
		c.MaxConsecutiveToolErrors = defaultMaxConsecutiveToolErrors
	}
	if c.MaxRejections <= 0 {
		c.MaxRejections = defaultMaxRejections
	}
	if c.RoundBudgetUSD <= 0 {
		c.RoundBudgetUSD = defaultRoundBudgetUSD
	}
	if c.SessionBudgetUSD <= 0 {
		c.SessionBudgetUSD = defaultSessionBudgetUSD
	}
	if c.SessionDeadline <= 0 {
		c.SessionDeadline = defaultSessionDeadline
	}
	if c.RoundDeadline <= 0 {
		// Derived from the session's own clock, not a flat 15 minutes.
		//
		// A round is one Architect objective, one agentic Implementer stretch and
		// one review. The value of several rounds is the review BETWEEN them: the
		// Architect sees the diff, says what is wrong, and the next round acts on
		// it. One long round is a worse use of the same minutes.
		//
		// A fixed 15 minutes was invisible while the wall-clock cap was ten —
		// the session deadline always fired first, so this never applied. Raise
		// the cap past 15 minutes and it becomes live in the worst way: a single
		// round could consume three quarters of the budget and leave no time to
		// act on its review. Allowing a third of the session per round keeps at
		// least three, which is where MaxRounds sits anyway.
		//
		// The ceiling still applies, so a long BYOC session is unchanged.
		c.RoundDeadline = c.SessionDeadline / 3
		if c.RoundDeadline > defaultRoundDeadline {
			c.RoundDeadline = defaultRoundDeadline
		}
	}
	if c.CompactAt == 0 {
		c.CompactAt = defaultCompactAt
	}
	return c
}

// Event is one structured phase of a session, in order. Like loop.Event it
// carries no task or org identity — the caller that persists one is the one
// that can attribute it.
type Event struct {
	Round   int
	Phase   string // plan | round_start | tool | verify | review | round_end | session_end | compaction
	Outcome string // proposed | ok | error | pass | fail | approve | revise | abandon | halted
	// Detail is human-readable context. As with loop.Event, never assume it is
	// safe to publish verbatim: tool output can carry secrets.
	Detail string
	Tool   string
	// Input is the tool call's arguments, as the model wrote them. Without it a
	// timeline can say that `run` was called and what it printed, but never the
	// command — which is most of what a reader wants to know.
	//
	// Model-authored, so it is safe to display; it is capped like Detail and, in
	// the signed record, hashed rather than quoted for the same reason Detail is.
	Input        string
	DurationMs   int64
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	// seq orders events across the whole session for the durable log. It is set
	// by emit, not by callers, and is unexported because it is a storage detail
	// rather than something a telemetry consumer should reason about.
	seq int
}

// Seq reports the event's position in the session's history.
func (e Event) Seq() int { return e.seq }

const detailCap = 2000

// inputCap bounds a recorded tool argument. Smaller than detailCap because the
// one call that can be genuinely large is write_file, whose whole content would
// otherwise be carried twice — once as the argument and once as the file.
const inputCap = 600

// Result reports the outcome of a session.
type Result struct {
	Success bool
	Rounds  int
	CostUSD float64
	Usage   provider.ToolUsage
	// Summary is the Architect's pull request body, set when it approved.
	Summary string
	// Detail explains a non-success outcome in words a user can act on.
	Detail      string
	FinalOutput string
	HeadSHA     string
}

// Runner executes a session.
type Runner struct {
	Architect   Architect
	Implementer provider.ToolRunner
	// ImplementerModel is used only for reporting; routing already happened when
	// the caller built Implementer.
	ImplementerModel string
	Tools            ToolHost
	Workspace        Workspace
	Verify           VerifyFunc
	Config           Config
	// Store makes the session durable. Nil runs it entirely in memory, which is
	// what a test or a single-shot run wants; with one, a crashed daemon's task
	// resumes from its last finished round rather than starting over.
	Store Store
	// SessionID identifies this session in the Store. Required when Store is set.
	SessionID string
}

func (r *Runner) logf(format string, a ...any) {
	if r.Config.Log != nil {
		r.Config.Log(format, a...)
	}
}

// emit reports one phase and, when the session is durable, queues it for the
// next checkpoint. Events are buffered rather than written as they happen: they
// have to land in the same transaction as the checkpoint they belong to, or a
// resumed session's history has a hole exactly where the crash was.
func (r *Runner) emit(st *state, ev Event) {
	ev.Detail = tail(ev.Detail, detailCap)
	// Arguments are truncated from the FRONT, unlike Detail. A tool call's
	// meaning is at its start — the path, the pattern, the command — whereas
	// command output explains itself at the end.
	ev.Input = head(ev.Input, inputCap)
	if r.Store != nil {
		ev.seq = st.seq
		st.seq++
		st.pending = append(st.pending, ev)
	}
	if r.Config.OnEvent != nil {
		r.Config.OnEvent(ev)
	}
}

// save writes a checkpoint together with everything emitted since the last one.
//
// A failed save is logged and swallowed. The session is the product; durability
// is insurance, and dropping a run because the Control Plane was briefly
// unreachable would trade a small risk of repeating a round for a certainty of
// losing one.
func (r *Runner) save(ctx context.Context, st *state, nextRound, attempts int) {
	if r.Store == nil || r.SessionID == "" {
		return
	}
	events := st.pending
	st.pending = nil
	if err := r.Store.Save(ctx, r.SessionID, st.checkpoint(st.baseSHA, nextRound, attempts), events); err != nil {
		r.logf("[session] could not checkpoint round %d: %v\n", nextRound, err)
		// Put them back so the next checkpoint carries them rather than losing
		// the history of a round that did happen.
		st.pending = append(events, st.pending...)
	}
}

// finish records the terminal status, so a task leased again starts a new
// session instead of resuming a concluded one.
func (r *Runner) finish(ctx context.Context, st *state, success bool) {
	if r.Store == nil || r.SessionID == "" {
		return
	}
	r.save(ctx, st, st.round, 0)
	if err := r.Store.Finish(ctx, r.SessionID, success); err != nil {
		r.logf("[session] could not record the session as finished: %v\n", err)
	}
}

// state is the session's live position. It is deliberately a value that could
// be written down and read back: the durable-session work persists exactly this
// and resumes a crashed run from it.
type state struct {
	round int
	spec  Spec
	// Architect and Implementer spend are tracked apart rather than as one
	// running total, because they are the two halves of the tiering decision:
	// an expensive reviewer called a handful of times and a cheap worker called
	// constantly. A single number cannot answer "was the split worth it", which
	// is the question the defaults in this file exist to be checked against.
	architect   provider.ToolUsage
	implementer provider.ToolUsage
	// roundConv is the last-seen cumulative usage of the round's conversation,
	// so per-turn deltas can be folded into implementer.
	roundConv    provider.ToolUsage
	rejections   int
	history      []string
	lastVerify   string
	verifyPassed bool
	headSHA      string
	// progress records (tree, verification output) fingerprints per round, so a
	// session that keeps arriving in the same place can be stopped.
	progress map[string]int
	// specSeen records spec fingerprints, so a reviewer asking for the same
	// thing twice can be stopped.
	specSeen map[string]int
	// baseSHA is where the session started. Fixed for its lifetime.
	baseSHA string
	// pending holds events emitted since the last checkpoint.
	pending []Event
	// seq numbers events for the durable log. It counts across the whole
	// session, not per round and not per checkpoint.
	//
	// Per-checkpoint numbering silently lost events: a round writes two
	// checkpoints (one when it starts, one when it ends), so the counter reset
	// mid-round and the first event after the reset collided with the first
	// event before it. The (session, round, seq) index resolves a collision by
	// ignoring the newcomer, so the loss was invisible.
	//
	// Session-wide numbering also survives a resume, which per-round numbering
	// could not: a re-run round would have restarted at zero and collided with
	// the crashed attempt's events for that same round, dropping the retry's
	// history instead of appending it. Hence Checkpoint.Seq.
	seq int
	// attempts counts starts of the current round, carried across a resume.
	attempts int
}

// Run drives the session to a pull-requestable state or to a reasoned stop.
func (r *Runner) Run(ctx context.Context, task Task) (Result, error) {
	if r.Architect == nil {
		return Result{}, fmt.Errorf("session: no architect configured")
	}
	if r.Implementer == nil {
		return Result{}, fmt.Errorf("session: no implementer configured")
	}
	if r.Tools == nil || r.Workspace == nil || r.Verify == nil {
		return Result{}, fmt.Errorf("session: tools, workspace and verify are all required")
	}
	cfg := r.Config.withDefaults()
	r.Config = cfg

	ctx, cancel := context.WithTimeout(ctx, cfg.SessionDeadline)
	defer cancel()

	st := &state{progress: map[string]int{}, specSeen: map[string]int{}}

	tree, err := r.Workspace.Tree(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("session: read repository tree: %w", err)
	}
	if st.headSHA, err = r.Workspace.HeadSHA(ctx); err != nil {
		return Result{}, fmt.Errorf("session: read head: %w", err)
	}
	st.baseSHA = st.headSHA

	// Resume, if this task has been leased before and got somewhere.
	//
	// The in-progress round is discarded rather than continued: the worktree is
	// reset to the last committed round and that round re-runs from its spec.
	// Because the Implementer starts every round fresh anyway, this is not a
	// special recovery path — it is the ordinary path entered later — and it
	// costs at most one round rather than requiring a half-written provider
	// transcript with an outstanding tool call to be persisted and replayed.
	resumeFrom := 0
	if r.Store != nil && r.SessionID != "" {
		cp, lerr := r.Store.Load(ctx, r.SessionID)
		if lerr != nil {
			// Refusing to run because the checkpoint could not be read would turn
			// a Control Plane blip into a failed task. Starting over is the safe
			// answer: at worst the work is repeated.
			r.logf("[session] could not load the checkpoint, starting from the beginning: %v\n", lerr)
		} else if cp != nil && cp.Round > 0 {
			if cp.Attempts >= maxRoundAttempts {
				detail := fmt.Sprintf("stopped: round %d has already been started %d times without finishing, so it is being treated as unrunnable rather than retried further",
					cp.Round, cp.Attempts)
				r.logf("[session] %s\n", detail)
				st.restore(cp)
				st.round = cp.Round
				r.finish(ctx, st, false)
				return r.result(st, false, detail), nil
			}
			st.restore(cp)
			st.baseSHA = cp.BaseSHA
			resumeFrom = cp.Round
			st.attempts = cp.Attempts
			r.logf("[session] resuming at round %d (attempt %d) from %s\n", resumeFrom, st.attempts, shortSHA(cp.HeadSHA))
			if cp.HeadSHA != "" {
				if rerr := r.Workspace.Reset(ctx, cp.HeadSHA); rerr != nil {
					return Result{}, fmt.Errorf("session: could not discard the interrupted round: %w", rerr)
				}
			}
		}
	}

	// Baseline and planning happen once per session. A resumed run already has
	// both — the Architect's spec is in the checkpoint — and re-planning would
	// pay for a frontier-model call to reproduce an answer already written down.
	if resumeFrom > 0 {
		// A session resumed at its last round has already spent that many
		// rounds, so a fixed ceiling would leave a continuation with nothing to
		// spend: a task that concluded at round 4 of 4 would resume and halt
		// immediately, reporting that it ran out of rounds without doing
		// anything. The budget is therefore per run, counted from where this one
		// starts, which is also what makes each continuation cost what an
		// ordinary task costs.
		cfg.MaxRounds += resumeFrom
		return r.rounds(ctx, task, st, cfg, resumeFrom)
	}

	// Baseline. As in the single-file loop this establishes what the change must
	// not regress, and it is a guard rather than the objective: a green suite is
	// not a reason to skip the work.
	start := time.Now()
	baseOut, basePassed, err := r.Verify(ctx)
	if err != nil {
		r.emit(st, Event{Phase: "verify", Outcome: "error", Detail: err.Error(), DurationMs: ms(start)})
		return Result{}, fmt.Errorf("session: baseline verification failed to run: %w", err)
	}
	r.emit(st, Event{Phase: "verify", Outcome: passFail(basePassed), Detail: baseOut, DurationMs: ms(start)})
	st.lastVerify, st.verifyPassed = baseOut, basePassed
	if basePassed {
		r.logf("[session] baseline passes; the change must land and keep it passing\n")
	} else {
		r.logf("[session] baseline fails; the change must make it pass\n")
	}

	// Round 0: the Architect writes the opening spec. This is the planning the
	// control plane used to do without ever seeing the repository.
	start = time.Now()
	spec, err := r.Architect.Plan(ctx, PlanInput{
		Task:            task.Description,
		RepoMap:         tree,
		TestCmd:         task.TestCmd,
		BaselineOutput:  baseOut,
		BaselinePassed:  basePassed,
		RepoContext:     task.RepoContext,
		PriorLearnings:  task.Learnings,
		MaxRoundsBudget: cfg.MaxRounds,
	})
	planUsage := r.trackArchitect(st)
	if err != nil {
		r.emit(st, Event{Phase: "plan", Outcome: "error", Detail: err.Error(), DurationMs: ms(start),
			InputTokens: planUsage.InputTokens, OutputTokens: planUsage.OutputTokens, CostUSD: planUsage.CostUSD})
		return r.result(st, false, fmt.Sprintf("planning failed: %v", err)), err
	}
	r.emit(st, Event{Phase: "plan", Outcome: "proposed", Detail: spec.Objective, DurationMs: ms(start),
		InputTokens: planUsage.InputTokens, OutputTokens: planUsage.OutputTokens, CostUSD: planUsage.CostUSD})
	if spec.Verdict == VerdictAbandon {
		r.logf("[session] the architect declined the task: %s\n", spec.Rationale)
		return r.result(st, false, "the task was not attempted: "+spec.Rationale), nil
	}
	st.spec = spec
	st.specSeen[spec.fingerprint()]++
	// Zero, not one: the round loop increments before it runs, so a checkpoint
	// written here records "round 1, not yet attempted".
	st.attempts = 0
	r.save(ctx, st, 1, 0)

	return r.rounds(ctx, task, st, cfg, 1)
}

// rounds drives the Implementer/review cycle from startRound onwards. It is
// separate from Run so a resumed session can enter it directly, without
// re-running the baseline or paying for a plan it already has.
func (r *Runner) rounds(ctx context.Context, task Task, st *state, cfg Config, startRound int) (Result, error) {
	var start time.Time
	for st.round = startRound; st.round <= cfg.MaxRounds; st.round++ {
		if err := ctx.Err(); err != nil {
			return r.result(st, false, deadlineDetail(err, st.round)), err
		}
		if st.total().CostUSD >= cfg.SessionBudgetUSD {
			detail := fmt.Sprintf("stopped after %d round(s): the session budget of $%.2f was reached", st.round-1, cfg.SessionBudgetUSD)
			r.logf("[session] halted: %s\n", detail)
			r.emit(st, Event{Round: st.round, Phase: "session_end", Outcome: "halted", Detail: detail})
			r.finish(ctx, st, false)
			return r.result(st, false, detail), nil
		}

		// Record the attempt BEFORE the round runs. A checkpoint written only on
		// success cannot count attempts at all: a round that takes the daemon
		// down never gets to write one, so every retry would look like the first.
		st.attempts++
		if st.attempts > maxRoundAttempts {
			detail := fmt.Sprintf("stopped: round %d has been started %d times without finishing, so it is being treated as unrunnable rather than retried further",
				st.round, st.attempts-1)
			r.logf("[session] %s\n", detail)
			r.emit(st, Event{Round: st.round, Phase: "session_end", Outcome: "halted", Detail: detail})
			r.finish(ctx, st, false)
			return r.result(st, false, detail), nil
		}

		r.emit(st, Event{Round: st.round, Phase: "round_start", Outcome: "ok", Detail: st.spec.Objective})
		r.logf("[session] round %d: %s\n", st.round, st.spec.Objective)
		r.save(ctx, st, st.round, st.attempts)

		note, err := r.runRound(ctx, task, st)
		if err != nil {
			return r.result(st, false, fmt.Sprintf("round %d failed: %v", st.round, err)), err
		}

		// Commit before verifying. The branch is where a round's work becomes
		// durable, and a crash between the two must not lose the edits.
		if sha, cerr := r.Workspace.Commit(ctx, fmt.Sprintf("kiwi: round %d — %s", st.round, firstLine(st.spec.Objective))); cerr == nil {
			st.headSHA = sha
		} else if !errors.Is(cerr, ErrNoChanges) {
			return r.result(st, false, fmt.Sprintf("could not commit round %d: %v", st.round, cerr)), cerr
		}

		start = time.Now()
		out, passed, verr := r.Verify(ctx)
		if verr != nil {
			r.emit(st, Event{Round: st.round, Phase: "verify", Outcome: "error", Detail: verr.Error(), DurationMs: ms(start)})
			return r.result(st, false, fmt.Sprintf("verification could not run: %v", verr)), verr
		}
		r.emit(st, Event{Round: st.round, Phase: "verify", Outcome: passFail(passed), Detail: out, DurationMs: ms(start)})
		st.lastVerify, st.verifyPassed = out, passed

		diff, err := r.Workspace.Diff(ctx)
		if err != nil {
			return r.result(st, false, fmt.Sprintf("could not read the diff: %v", err)), err
		}
		files, _ := r.Workspace.FilesChanged(ctx)

		// No-progress across rounds: the whole-task diff and the verification
		// output are both byte-identical to a previous round, so this round
		// changed nothing about where the session stands. It generalises the
		// single-file loop's identical-output rail, which cannot see the tree and
		// so cannot tell "stuck" from "working on something else".
		//
		// Recorded here but acted on after the review, below: the reviewer is
		// still entitled to look at the round and approve it. Halting first would
		// throw away a finished, passing change because the round before it
		// happened to leave the same state.
		st.progress[fingerprint(diff, out)]++
		stalled := st.progress[fingerprint(diff, out)] >= noProgressHalt

		start = time.Now()
		review, err := r.Architect.Review(ctx, ReviewInput{
			Task:            task.Description,
			Spec:            st.spec,
			Round:           st.round,
			Diff:            diff,
			FilesChanged:    files,
			HandoffNote:     note,
			VerifyOutput:    out,
			VerifyPassed:    passed,
			History:         st.history,
			RoundsRemaining: cfg.MaxRounds - st.round,
		})
		reviewUsage := r.trackArchitect(st)
		if err != nil {
			r.emit(st, Event{Round: st.round, Phase: "review", Outcome: "error", Detail: err.Error(), DurationMs: ms(start),
				InputTokens: reviewUsage.InputTokens, OutputTokens: reviewUsage.OutputTokens, CostUSD: reviewUsage.CostUSD})
			return r.result(st, false, fmt.Sprintf("review failed: %v", err)), err
		}
		r.emit(st, Event{Round: st.round, Phase: "review", Outcome: review.Verdict, Detail: review.Rationale, DurationMs: ms(start),
			InputTokens: reviewUsage.InputTokens, OutputTokens: reviewUsage.OutputTokens, CostUSD: reviewUsage.CostUSD})

		st.history = append(st.history, fmt.Sprintf("- round %d: asked for %q; verification %s; reviewer said %s — %s",
			st.round, firstLine(st.spec.Objective), passFail(passed), review.Verdict, firstLine(review.Rationale)))

		switch review.Verdict {
		case VerdictApprove:
			r.logf("[session] round %d approved\n", st.round)
			r.emit(st, Event{Round: st.round, Phase: "session_end", Outcome: "approve", Detail: review.Rationale})
			r.finish(ctx, st, true)
			res := r.result(st, true, "")
			res.Summary = review.Summary
			if res.Summary == "" {
				res.Summary = review.Rationale
			}
			return res, nil
		case VerdictAbandon:
			detail := "the reviewer stopped the task: " + review.Rationale
			r.logf("[session] %s\n", detail)
			r.emit(st, Event{Round: st.round, Phase: "session_end", Outcome: "abandon", Detail: review.Rationale})
			r.finish(ctx, st, false)
			return r.result(st, false, detail), nil
		}

		// A revise verdict is a rejection: it consumed a round and delivered
		// nothing the reviewer would accept.
		st.rejections++
		if st.rejections >= cfg.MaxRejections {
			detail := fmt.Sprintf("stopped after %d rounds: the reviewer rejected every one and the implementer could not satisfy it — last reason: %s",
				st.round, firstLine(review.Rationale))
			r.logf("[session] halted: %s\n", detail)
			r.emit(st, Event{Round: st.round, Phase: "session_end", Outcome: "halted", Detail: detail})
			r.finish(ctx, st, false)
			return r.result(st, false, detail), nil
		}

		// A reviewer asking for the same thing twice is looping. The single-file
		// loop discovered this failure by counting rejections; comparing them
		// catches it a round earlier and says something more useful about why.
		fpSpec := review.fingerprint()
		st.specSeen[fpSpec]++
		if st.specSeen[fpSpec] >= 2 {
			detail := fmt.Sprintf("stopped after %d rounds: the reviewer asked for the same change twice, so the session is looping — %s",
				st.round, firstLine(review.Rationale))
			r.logf("[session] halted: %s\n", detail)
			r.emit(st, Event{Round: st.round, Phase: "session_end", Outcome: "halted", Detail: detail})
			r.finish(ctx, st, false)
			return r.result(st, false, detail), nil
		}

		// Now act on the stall recorded above — but only if another round would
		// actually follow. On the last round the cap is the accurate explanation,
		// and reporting "no progress" instead would tell the user to look for a
		// stuck agent when what they have is a task too big for its budget.
		if stalled && st.round < cfg.MaxRounds {
			detail := fmt.Sprintf("stopped after %d rounds: the last two ended in exactly the same state, so the session is not making progress", st.round)
			r.logf("[session] halted: %s\n", detail)
			r.emit(st, Event{Round: st.round, Phase: "session_end", Outcome: "halted", Detail: detail})
			r.finish(ctx, st, false)
			return r.result(st, false, detail), nil
		}

		// The round finished and produced a reviewed result, so the next one
		// starts with a clean attempt count: whatever went wrong was the work,
		// not the round being unrunnable.
		st.spec = review
		st.attempts = 0
		r.save(ctx, st, st.round+1, 0)
	}

	detail := fmt.Sprintf("stopped after the maximum of %d rounds without the reviewer approving", cfg.MaxRounds)
	r.emit(st, Event{Round: cfg.MaxRounds, Phase: "session_end", Outcome: "halted", Detail: detail})
	r.finish(ctx, st, false)
	return r.result(st, false, detail), nil
}

// runRound drives one Implementer conversation to its end and returns the
// handoff note it left.
func (r *Runner) runRound(ctx context.Context, task Task, st *state) (string, error) {
	cfg := r.Config
	roundCtx, cancel := context.WithTimeout(ctx, cfg.RoundDeadline)
	defer cancel()

	if fr, ok := r.Tools.(interface{ Reset() }); ok {
		fr.Reset()
	}

	system := implementerSystem(task.TestCmd)
	opts := provider.ConversationOpts{Cache: !cfg.NoCache, CompactAt: cfg.CompactAt}
	conv := r.Implementer.StartConversation(system, r.Tools.Defs(), opts)

	roundStartCost := st.total().CostUSD
	st.roundConv = provider.ToolUsage{}
	text := r.roundPrompt(task, st)
	var results []provider.ToolResult
	// compacted carries the summary of a transcript that was replaced, so the
	// round continues from a digest rather than from nothing.
	compacted := ""

	dupes := map[string]int{}
	consecutiveErrors := 0

	for calls := 0; calls < cfg.MaxToolCallsPerRound; {
		if err := roundCtx.Err(); err != nil {
			// A round that runs out of time is not a failed session: the work it
			// committed stands and the reviewer judges it. Only a session-level
			// deadline ends the run.
			r.logf("[session] round %d hit its %s deadline\n", st.round, cfg.RoundDeadline)
			return r.noteOrDefault("the round ran out of time before the implementer finished"), nil
		}

		// Compaction. The transcript is re-sent on every turn, so an exploration
		// phase that has already yielded its conclusions is pure recurring cost.
		// Rather than editing a provider's message list from outside — which
		// would mean owning its format — the round asks the model to summarise,
		// then starts a fresh conversation from that summary. It is the same move
		// the session makes between rounds, applied within one.
		//
		// Checked here, before the send, because this is the point where the
		// outstanding tool results are still in hand: they go out with the
		// compaction request, satisfying the API's requirement that every tool
		// call be answered, and are then dropped along with the transcript they
		// belong to.
		if cfg.CompactAt > 0 {
			if tr, ok := conv.(provider.TranscriptReporter); ok && tr.TranscriptTokens() >= cfg.CompactAt {
				summary, cerr := r.compact(roundCtx, conv, st, results)
				r.trackImplementer(st, conv)
				if cerr != nil {
					// Not fatal: a round that cannot compact is a round that keeps
					// paying full price, which is worse than the alternative but
					// far better than a failed task.
					r.logf("[session] round %d: could not compact the transcript: %v\n", st.round, cerr)
				} else {
					compacted = summary
					conv = r.Implementer.StartConversation(system, r.Tools.Defs(), opts)
					st.roundConv = provider.ToolUsage{}
					text = r.compactedPrompt(task, st, compacted)
					results = nil
				}
			}
		}

		// The Implementer's own turn is timed and reported like every other phase.
		//
		// It was the one thing in a session that was not. Every surrounding phase
		// emitted a duration, so the sum of a task's events came to less than its
		// wall clock and the difference — model generation, plus any provider
		// backoff inside it — was attributed to nothing at all. That gap is
		// usually the largest single component of a run, which made "where did
		// the time go" unanswerable from the record.
		turnStart := time.Now()
		turn, err := conv.Send(roundCtx, text, results)
		used := r.trackImplementer(st, conv)
		if err != nil {
			r.emit(st, Event{Round: st.round, Phase: "implementer", Outcome: "error",
				Detail: err.Error(), DurationMs: ms(turnStart),
				InputTokens: used.InputTokens, OutputTokens: used.OutputTokens, CostUSD: used.CostUSD})
			if roundCtx.Err() != nil {
				return r.noteOrDefault("the round ran out of time before the implementer finished"), nil
			}
			return "", fmt.Errorf("implementer turn failed: %w", err)
		}
		// Detail is the model's own prose, which is what it said it was doing
		// between tool calls; the tool calls themselves are emitted below.
		r.emit(st, Event{Round: st.round, Phase: "implementer", Outcome: turnOutcome(turn),
			Detail: turn.Text, DurationMs: ms(turnStart),
			InputTokens: used.InputTokens, OutputTokens: used.OutputTokens, CostUSD: used.CostUSD})
		text, results = "", nil

		if spent := st.total().CostUSD - roundStartCost; spent >= cfg.RoundBudgetUSD {
			r.logf("[session] round %d hit its $%.2f budget\n", st.round, cfg.RoundBudgetUSD)
			return r.noteOrDefault(fmt.Sprintf("the round stopped at its $%.2f budget", cfg.RoundBudgetUSD)), nil
		}
		if st.total().CostUSD >= cfg.SessionBudgetUSD {
			return r.noteOrDefault(fmt.Sprintf("the session stopped at its $%.2f budget", cfg.SessionBudgetUSD)), nil
		}

		if turn.Done {
			// The model ended its turn without asking for a tool and without
			// calling finish. Treat its closing text as the note: it has said
			// what it did, and refusing to accept that would burn a round on
			// protocol.
			if done, note := r.finished(); done {
				return note, nil
			}
			return r.noteOrDefault(turn.Text), nil
		}

		for _, call := range turn.Calls {
			calls++
			start := time.Now()
			out, err := r.Tools.Call(roundCtx, call)
			if err != nil {
				r.emit(st, Event{Round: st.round, Phase: "tool", Outcome: "error", Tool: call.Name,
					Input: string(call.Input), Detail: err.Error(), DurationMs: ms(start)})
				return "", err
			}
			r.emit(st, Event{Round: st.round, Phase: "tool", Outcome: okErr(!out.IsError), Tool: call.Name,
				Input: string(call.Input), Detail: out.Content, DurationMs: ms(start)})

			if out.IsError {
				consecutiveErrors++
			} else {
				consecutiveErrors = 0
			}

			// Repetition rail. Warn first — a model told plainly that it is
			// repeating itself usually changes approach — and halt only if it
			// keeps going.
			if call.Name == ToolRun {
				key := fingerprint(string(call.Input), out.Content)
				dupes[key]++
				switch {
				case dupes[key] >= dupCommandHalt:
					r.logf("[session] round %d: the same command produced the same output %d times; ending the round\n", st.round, dupes[key])
					return r.noteOrDefault("the round was stopped: the same command kept producing the same output"), nil
				case dupes[key] == dupCommandWarn:
					out.Content += fmt.Sprintf("\n\n[kiwi] You have now run this exact command %d times and received identical output. "+
						"Repeating it will not tell you anything new — change your approach.", dupes[key])
				}
			}

			results = append(results, out)
		}

		if consecutiveErrors >= cfg.MaxConsecutiveToolErrors {
			r.logf("[session] round %d: %d tool calls failed in a row; ending the round\n", st.round, consecutiveErrors)
			return r.noteOrDefault(fmt.Sprintf("the round was stopped after %d tool calls failed in a row", consecutiveErrors)), nil
		}

		if done, note := r.finished(); done {
			return note, nil
		}
	}

	r.logf("[session] round %d reached the %d tool-call cap\n", st.round, cfg.MaxToolCallsPerRound)
	return r.noteOrDefault(fmt.Sprintf("the round reached its limit of %d tool calls", cfg.MaxToolCallsPerRound)), nil
}

// finished reports the handoff note if the Implementer called finish.
func (r *Runner) finished() (bool, string) {
	f, ok := r.Tools.(interface{ Finished() (bool, string) })
	if !ok {
		return false, ""
	}
	return f.Finished()
}

func (r *Runner) noteOrDefault(fallback string) string {
	if done, note := r.finished(); done && strings.TrimSpace(note) != "" {
		return note
	}
	return fallback
}

// compact asks the model to summarise its own round so far, then reports the
// summary. What is safe to drop is a judgment the model is better placed to
// make than a token-counting heuristic: it knows which of the forty files it
// read actually mattered.
// pending carries the tool results that have not been answered yet. They must
// travel WITH the compaction request rather than being dropped: every provider
// requires each tool call to be answered in the turn that follows it —
// Anthropic rejects a bare text turn after tool_use blocks, OpenAI after
// tool_calls, Gemini after a functionCall — and compaction only ever triggers
// when the model has just asked for tools, so sending the prompt alone would
// fail the request every single time it mattered.
func (r *Runner) compact(ctx context.Context, conv provider.ToolConversation, st *state, pending []provider.ToolResult) (string, error) {
	start := time.Now()
	turn, err := conv.Send(ctx, compactPrompt, pending)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(turn.Text) == "" {
		return "", errors.New("the model returned an empty summary")
	}
	r.emit(st, Event{Round: st.round, Phase: "compaction", Outcome: "ok", Detail: turn.Text, DurationMs: ms(start)})
	r.logf("[session] round %d: transcript compacted\n", st.round)
	return turn.Text, nil
}

const compactPrompt = `Your context is being compacted to keep this round affordable. Stop what you are doing and
write a handover to yourself — you will continue with this summary in place of everything above it.

Cover: what you have already changed and where; what you learned about this repository that you would
otherwise have to rediscover; what you were in the middle of; and what remains. Be specific about
paths and symbols. Do not call any tools in this reply.`

// compactedPrompt restarts a round from its summary.
func (r *Runner) compactedPrompt(task Task, st *state, summary string) string {
	var b strings.Builder
	b.WriteString(r.roundPrompt(task, st))
	b.WriteString("\n# Where you had got to\n")
	b.WriteString("This round has already been running. Your earlier work is on disk and your own handover follows; " +
		"re-read anything you need rather than assuming it is unchanged.\n\n")
	b.WriteString(summary)
	b.WriteString("\n")
	return b.String()
}

func (r *Runner) roundPrompt(task Task, st *state) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# The user's request\n%s\n", task.Description)
	if task.RepoContext != "" {
		fmt.Fprintf(&b, "\n# Repository conventions (AGENT.md)\n%s\n", task.RepoContext)
	}
	fmt.Fprintf(&b, "\n# Your brief for this round (round %d)\n%s\n", st.round, st.spec.Prompt())
	if len(st.history) > 0 {
		b.WriteString("\n# What earlier rounds did\nYou do not remember these; they are summarised for you.\n")
		for _, h := range st.history {
			fmt.Fprintf(&b, "%s\n", h)
		}
	}
	if task.TestCmd != "" {
		fmt.Fprintf(&b, "\n# Verification command\n%s\n\nMost recent result (%s):\n```\n%s\n```\n",
			task.TestCmd, passFail(st.verifyPassed), tail(st.lastVerify, 4000))
	}
	b.WriteString("\nStart by orienting yourself in the repository. When the work is done, call finish with a handoff note.\n")
	return b.String()
}

func implementerSystem(testCmd string) string {
	var b strings.Builder
	b.WriteString(`You are the Implementer on an automated software change. You have tools to read, search,
write and run things in a sandboxed checkout of a real repository. Do the work described in your brief.

How this works:

- You get ONE round. You will not remember it afterwards — a reviewer reads your diff and either
  approves it or writes a new brief for a fresh instance of you. Put everything worth knowing in the
  handoff note you pass to finish.
- Make the change the brief asks for, and no more. Unrelated refactoring makes review harder and is
  the most common reason a round is rejected.
- Verify your own work before finishing. Run the build and the tests; if something you changed does
  not compile, fix it now rather than leaving it for the reviewer to find.
- Your shell has NO network access and NO credentials. If you need dependencies installed, use the
  install tool, which is the one brokered exception.
- You cannot run git. Committing, branching and pushing are handled for you; just leave the working
  tree in the state you want reviewed.
`)
	if testCmd != "" {
		fmt.Fprintf(&b, "\nThe verification command for this repository is: %s\n", testCmd)
		b.WriteString("It is a guard proving you broke nothing, not the definition of done. Making it pass by " +
			"weakening a test is a failure, not a shortcut.\n")
	}
	return b.String()
}

// total is the session's spend: both roles together. Every budget rail reads
// this, so a cheap implementer cannot be used to hide an expensive reviewer.
func (st *state) total() provider.ToolUsage {
	var t provider.ToolUsage
	t.Add(st.architect)
	t.Add(st.implementer)
	return t
}

// trackArchitect refreshes the Architect's cumulative usage and returns what
// the call just made cost. Architect.Usage reports a running total, so it is
// assigned rather than accumulated, and the delta is the difference.
//
// The delta is what the emitted event carries. Reporting the running total on
// each event instead double-counts every earlier call when a consumer sums the
// stream — and the Control Plane does exactly that to meter a task, so the
// difference decides whether an allowance is charged correctly or many times
// over.
func (r *Runner) trackArchitect(st *state) provider.ToolUsage {
	if r.Architect == nil {
		return provider.ToolUsage{}
	}
	total := r.Architect.Usage()
	delta := provider.ToolUsage{
		InputTokens:      total.InputTokens - st.architect.InputTokens,
		OutputTokens:     total.OutputTokens - st.architect.OutputTokens,
		CacheReadTokens:  total.CacheReadTokens - st.architect.CacheReadTokens,
		CacheWriteTokens: total.CacheWriteTokens - st.architect.CacheWriteTokens,
		CostUSD:          total.CostUSD - st.architect.CostUSD,
	}
	st.architect = total
	return delta
}

// trackImplementer folds one turn's usage into the session total. A
// conversation also reports cumulatively, so the delta since the last turn is
// what this round has newly spent.
// trackImplementer folds one turn's usage into the session totals and returns
// that turn's delta, so a caller can attribute the turn it just made without
// recomputing the subtraction.
func (r *Runner) trackImplementer(st *state, conv provider.ToolConversation) provider.ToolUsage {
	total := conv.Usage()
	prev := st.roundConv
	delta := provider.ToolUsage{
		InputTokens:      total.InputTokens - prev.InputTokens,
		OutputTokens:     total.OutputTokens - prev.OutputTokens,
		CacheReadTokens:  total.CacheReadTokens - prev.CacheReadTokens,
		CacheWriteTokens: total.CacheWriteTokens - prev.CacheWriteTokens,
		CostUSD:          total.CostUSD - prev.CostUSD,
	}
	st.implementer.Add(delta)
	st.roundConv = total
	return delta
}

func (r *Runner) result(st *state, success bool, detail string) Result {
	rounds := st.round
	if rounds > r.Config.MaxRounds {
		rounds = r.Config.MaxRounds
	}
	return Result{
		Success:     success,
		Rounds:      rounds,
		CostUSD:     st.total().CostUSD,
		Usage:       st.total(),
		Detail:      detail,
		FinalOutput: st.lastVerify,
		HeadSHA:     st.headSHA,
	}
}

func fingerprint(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ms(t time.Time) int64 { return time.Since(t).Milliseconds() }

func passFail(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func okErr(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}

// shortSHA abbreviates a commit for a log line.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	for len(cut) > 0 && cut[0]&0xC0 == 0x80 {
		cut = cut[1:]
	}
	return cut
}

// turnOutcome describes what an Implementer turn decided to do: ask for tools,
// or stop. Both are ordinary, so neither is an error — the distinction is what
// tells a reader whether the round ended because the model was finished or
// because a rail cut it short.
func turnOutcome(t provider.Turn) string {
	switch {
	case len(t.Calls) > 0:
		return "tools"
	case t.Done:
		return "done"
	default:
		return "ok"
	}
}

// head keeps the first n bytes, trimming back to a rune boundary. The mirror of
// tail, for the things whose meaning is at the start rather than the end.
func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && cut[len(cut)-1]&0xC0 == 0x80 {
		cut = cut[:len(cut)-1]
	}
	// A multi-byte rune can straddle the cut: drop the partial leader too.
	for len(cut) > 0 && cut[len(cut)-1]&0xC0 == 0xC0 {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func deadlineDetail(err error, round int) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("the session ran out of time during round %d", round)
	}
	return fmt.Sprintf("the session was cancelled during round %d", round)
}
