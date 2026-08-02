package session

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// fakeWorkspace stands in for a git worktree. Commit records a message and
// advances a synthetic head, which is enough to exercise every branch the
// runner takes on a real repository.
type fakeWorkspace struct {
	tree    []string
	diff    string
	files   []string
	commits []string
	head    string
	noWork  bool
}

func (w *fakeWorkspace) Tree(context.Context) ([]string, error)         { return w.tree, nil }
func (w *fakeWorkspace) Diff(context.Context) (string, error)           { return w.diff, nil }
func (w *fakeWorkspace) FilesChanged(context.Context) ([]string, error) { return w.files, nil }
func (w *fakeWorkspace) HeadSHA(context.Context) (string, error)        { return w.head, nil }
func (w *fakeWorkspace) Reset(_ context.Context, sha string) error      { w.head = sha; return nil }
func (w *fakeWorkspace) Commit(_ context.Context, msg string) (string, error) {
	if w.noWork {
		return "", ErrNoChanges
	}
	w.commits = append(w.commits, msg)
	w.head = fmt.Sprintf("sha%d", len(w.commits))
	return w.head, nil
}

// fakeArchitect replays a scripted sequence of verdicts.
type fakeArchitect struct {
	plan    Spec
	planErr error
	reviews []Spec
	seen    []ReviewInput
	usage   provider.ToolUsage
	costPer float64
}

func (a *fakeArchitect) Usage() provider.ToolUsage { return a.usage }

func (a *fakeArchitect) Plan(context.Context, PlanInput) (Spec, error) {
	a.usage.Add(provider.ToolUsage{CostUSD: a.costPer})
	if a.planErr != nil {
		return Spec{}, a.planErr
	}
	return a.plan, nil
}

func (a *fakeArchitect) Review(_ context.Context, in ReviewInput) (Spec, error) {
	a.usage.Add(provider.ToolUsage{CostUSD: a.costPer})
	a.seen = append(a.seen, in)
	if len(a.reviews) == 0 {
		return Spec{Verdict: VerdictApprove, Summary: "done"}, nil
	}
	s := a.reviews[0]
	a.reviews = a.reviews[1:]
	return s, nil
}

// finishingRunner is an Implementer that calls finish on its first turn, which
// is the shortest possible well-behaved round.
func finishingRunner(note string) *provider.MockToolRunner {
	return &provider.MockToolRunner{
		Script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			if n == 1 {
				return provider.Turn{Calls: []provider.ToolCall{
					provider.MockCall("f", ToolFinish, map[string]string{"note": note}),
				}}, nil
			}
			return provider.Turn{Text: "done"}, nil
		},
	}
}

func newRunner(t *testing.T, arch Architect, impl provider.ToolRunner, ws *fakeWorkspace, verify VerifyFunc) (*Runner, *[]Event) {
	t.Helper()
	tools, _ := newTools(t)
	var events []Event
	r := &Runner{
		Architect:   arch,
		Implementer: impl,
		Tools:       tools,
		Workspace:   ws,
		Verify:      verify,
		Config: Config{
			OnEvent: func(e Event) { events = append(events, e) },
		},
	}
	return r, &events
}

func passing(out string) VerifyFunc {
	return func(context.Context) (string, bool, error) { return out, true, nil }
}

func failing(out string) VerifyFunc {
	return func(context.Context) (string, bool, error) { return out, false, nil }
}

func TestApprovedRoundSucceedsAndCarriesThePullRequestBody(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "add the retry wrapper"},
		reviews: []Spec{{Verdict: VerdictApprove, Summary: "Adds a retry wrapper to the fetch path."}},
	}
	ws := &fakeWorkspace{tree: []string{"main.go"}, diff: "+retry", files: []string{"main.go"}, head: "base"}
	r, events := newRunner(t, arch, finishingRunner("wrapped the fetch call"), ws, passing("ok"))

	res, err := r.Run(context.Background(), Task{Description: "add retries", TestCmd: "go test ./..."})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got detail %q", res.Detail)
	}
	if res.Rounds != 1 {
		t.Errorf("rounds = %d, want 1", res.Rounds)
	}
	if res.Summary != "Adds a retry wrapper to the fetch path." {
		t.Errorf("summary = %q", res.Summary)
	}
	if len(ws.commits) != 1 {
		t.Errorf("expected one commit, got %v", ws.commits)
	}
	if !hasEvent(*events, "session_end", VerdictApprove) {
		t.Errorf("expected an approving session_end event, got %v", *events)
	}
}

// The reviewer's memory is the whole point of a persistent Architect: round 2's
// review must see what round 1 was asked for and what happened.
func TestReviewerSeesTheHistoryOfEarlierRounds(t *testing.T) {
	arch := &fakeArchitect{
		plan: Spec{Verdict: VerdictProceed, Objective: "first attempt"},
		reviews: []Spec{
			{Verdict: VerdictRevise, Objective: "second attempt", Rationale: "missed the cancellation case"},
			{Verdict: VerdictApprove, Summary: "ok now"},
		},
	}
	ws := &fakeWorkspace{tree: []string{"main.go"}, diff: "+x", head: "base"}
	r, _ := newRunner(t, arch, finishingRunner("did it"), ws, passing("ok"))

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil || !res.Success {
		t.Fatalf("expected success, got %+v err=%v", res, err)
	}
	if len(arch.seen) != 2 {
		t.Fatalf("expected two reviews, got %d", len(arch.seen))
	}
	second := arch.seen[1]
	if len(second.History) == 0 {
		t.Fatal("the second review must carry the first round's history")
	}
	if !strings.Contains(second.History[0], "first attempt") {
		t.Errorf("history does not mention round 1's objective: %v", second.History)
	}
	if second.Spec.Objective != "second attempt" {
		t.Errorf("round 2 should be reviewed against its own spec, got %q", second.Spec.Objective)
	}
	if second.RoundsRemaining != 2 {
		t.Errorf("rounds remaining = %d, want 2", second.RoundsRemaining)
	}
}

// The reviewer always sees the accumulated diff for the whole task — reviewing
// one round's slice in isolation is the single-file Critic's central weakness.
func TestReviewerSeesTheWholeTaskDiff(t *testing.T) {
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "go"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "the whole diff", files: []string{"a.go", "b.go"}, head: "base"}
	r, _ := newRunner(t, arch, finishingRunner("done"), ws, passing("ok"))

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if arch.seen[0].Diff != "the whole diff" {
		t.Errorf("diff = %q", arch.seen[0].Diff)
	}
	if len(arch.seen[0].FilesChanged) != 2 {
		t.Errorf("files changed = %v", arch.seen[0].FilesChanged)
	}
}

func TestMaxRoundsStopsTheSession(t *testing.T) {
	arch := &fakeArchitect{
		plan: Spec{Verdict: VerdictProceed, Objective: "attempt"},
		reviews: []Spec{
			{Verdict: VerdictRevise, Objective: "again 1"},
			{Verdict: VerdictRevise, Objective: "again 2"},
		},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r, _ := newRunner(t, arch, finishingRunner("done"), ws, passing("ok"))
	r.Config.MaxRounds = 2
	r.Config.MaxRejections = 99

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatal("expected failure when the round cap is reached")
	}
	if !strings.Contains(res.Detail, "maximum of 2 rounds") {
		t.Errorf("detail should name the cap, got %q", res.Detail)
	}
}

// Three rejections in a row is the single-file loop's rail, kept: a session
// that cannot satisfy its reviewer makes no progress while spending the budget.
func TestConsecutiveRejectionsHaltTheSession(t *testing.T) {
	arch := &fakeArchitect{
		plan: Spec{Verdict: VerdictProceed, Objective: "attempt"},
		reviews: []Spec{
			{Verdict: VerdictRevise, Objective: "one", Rationale: "no"},
			{Verdict: VerdictRevise, Objective: "two", Rationale: "still no"},
			{Verdict: VerdictRevise, Objective: "three", Rationale: "definitely not"},
		},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, head: "base"}
	// A changing diff keeps the no-progress rail out of the way so this test
	// isolates the rejection rail.
	step := 0
	verify := func(context.Context) (string, bool, error) {
		step++
		return fmt.Sprintf("attempt %d", step), true, nil
	}
	r, _ := newRunner(t, arch, finishingRunner("done"), ws, verify)
	r.Config.MaxRounds = 10

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(res.Detail, "rejected every one") {
		t.Errorf("detail = %q", res.Detail)
	}
	if res.Rounds != 3 {
		t.Errorf("rounds = %d, want 3", res.Rounds)
	}
}

// A reviewer that asks for the same change twice is looping, and saying so is
// more useful than waiting for the rejection count to run out.
func TestRepeatedSpecHaltsTheSession(t *testing.T) {
	same := Spec{Verdict: VerdictRevise, Objective: "make it retry", MustChange: []string{"client.go"}}
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "make it retry", MustChange: []string{"client.go"}},
		reviews: []Spec{same, same},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, head: "base"}
	step := 0
	verify := func(context.Context) (string, bool, error) {
		step++
		return fmt.Sprintf("run %d", step), true, nil
	}
	r, _ := newRunner(t, arch, finishingRunner("done"), ws, verify)
	r.Config.MaxRounds = 10
	r.Config.MaxRejections = 99

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(res.Detail, "same change twice") {
		t.Errorf("detail = %q", res.Detail)
	}
}

// Two rounds ending in exactly the same state means the session is spinning.
func TestNoProgressAcrossRoundsHaltsTheSession(t *testing.T) {
	arch := &fakeArchitect{
		plan: Spec{Verdict: VerdictProceed, Objective: "attempt"},
		reviews: []Spec{
			{Verdict: VerdictRevise, Objective: "try something else"},
			{Verdict: VerdictRevise, Objective: "try a third thing"},
		},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "identical", head: "base"}
	r, _ := newRunner(t, arch, finishingRunner("done"), ws, failing("same failure every time"))
	r.Config.MaxRounds = 10
	r.Config.MaxRejections = 99

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(res.Detail, "not making progress") {
		t.Errorf("detail = %q", res.Detail)
	}
}

func TestSessionBudgetHaltsBeforeTheNextRound(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "attempt"},
		reviews: []Spec{{Verdict: VerdictRevise, Objective: "again"}},
		costPer: 3.0,
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r, _ := newRunner(t, arch, finishingRunner("done"), ws, passing("ok"))
	r.Config.SessionBudgetUSD = 5.0
	r.Config.MaxRounds = 10

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(res.Detail, "session budget") {
		t.Errorf("detail = %q", res.Detail)
	}
}

// The budget covers both roles. A cheap implementer must not be able to mask an
// expensive reviewer.
func TestBudgetCountsBothRoles(t *testing.T) {
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "attempt"}, costPer: 0.5}
	impl := finishingRunner("done")
	impl.CostPerTurn = 0.5
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r, _ := newRunner(t, arch, impl, ws, passing("ok"))

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	// Plan (0.5) + one implementer turn (0.5) + review (0.5).
	if res.CostUSD < 1.4 {
		t.Errorf("cost = %v, expected both roles counted", res.CostUSD)
	}
}

// An architect that declines the task up front is a real answer, delivered in
// one call rather than four rounds of proving it.
func TestArchitectCanAbandonBeforeAnyRound(t *testing.T) {
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictAbandon, Rationale: "this repository has no HTTP layer to add a header to"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, head: "base"}
	r, _ := newRunner(t, arch, finishingRunner("x"), ws, passing("ok"))

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(res.Detail, "no HTTP layer") {
		t.Errorf("the reason must reach the user, got %q", res.Detail)
	}
	if len(ws.commits) != 0 {
		t.Errorf("nothing should have been committed, got %v", ws.commits)
	}
}

// The repetition rail warns before it halts — a model told it is repeating
// itself often changes approach, which the single-file loop can never do.
func TestRepeatedCommandEarnsAWarningThenEndsTheRound(t *testing.T) {
	tools, _ := newTools(t)
	tools.Exec = func(context.Context, string) (string, bool, error) { return "same output", false, nil }

	var warned bool
	impl := &provider.MockToolRunner{
		Script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			for _, r := range results {
				if strings.Contains(r.Content, "Repeating it will not tell you anything new") {
					warned = true
				}
			}
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall(fmt.Sprintf("c%d", n), ToolRun, map[string]string{"command": "go build ./..."}),
			}}, nil
		},
	}

	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "build it"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r := &Runner{Architect: arch, Implementer: impl, Tools: tools, Workspace: ws, Verify: passing("ok")}
	r.Config.MaxRounds = 1
	r.Config.MaxToolCallsPerRound = 20

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if !warned {
		t.Error("expected the model to be warned about repeating a command")
	}
	if len(arch.seen) != 1 {
		t.Fatalf("the round should have ended and been reviewed, got %d reviews", len(arch.seen))
	}
	if !strings.Contains(arch.seen[0].HandoffNote, "same command") {
		t.Errorf("handoff note should explain why the round ended: %q", arch.seen[0].HandoffNote)
	}
}

func TestToolCallCapEndsTheRound(t *testing.T) {
	tools, _ := newTools(t)
	calls := 0
	impl := &provider.MockToolRunner{
		Script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			calls++
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall(fmt.Sprintf("c%d", n), ToolReadFile, map[string]string{"path": "main.go"}),
			}}, nil
		},
	}
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "read things"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r := &Runner{Architect: arch, Implementer: impl, Tools: tools, Workspace: ws, Verify: passing("ok")}
	r.Config.MaxRounds = 1
	r.Config.MaxToolCallsPerRound = 5

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if calls > 6 {
		t.Errorf("the cap should have stopped the round, got %d turns", calls)
	}
	if !strings.Contains(arch.seen[0].HandoffNote, "5 tool calls") {
		t.Errorf("handoff note should name the cap: %q", arch.seen[0].HandoffNote)
	}
}

func TestConsecutiveToolErrorsEndTheRound(t *testing.T) {
	tools, _ := newTools(t)
	impl := &provider.MockToolRunner{
		Script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall(fmt.Sprintf("c%d", n), ToolReadFile, map[string]string{"path": "does/not/exist.go"}),
			}}, nil
		},
	}
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "read a missing file"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r := &Runner{Architect: arch, Implementer: impl, Tools: tools, Workspace: ws, Verify: passing("ok")}
	r.Config.MaxRounds = 1
	r.Config.MaxConsecutiveToolErrors = 3

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(arch.seen[0].HandoffNote, "failed in a row") {
		t.Errorf("handoff note = %q", arch.seen[0].HandoffNote)
	}
}

// A round that runs out of time is not a failed session: what it committed
// stands, and the reviewer judges it.
func TestRoundDeadlineEndsTheRoundNotTheSession(t *testing.T) {
	tools, _ := newTools(t)
	tools.Exec = func(ctx context.Context, cmd string) (string, bool, error) {
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return "slow", true, nil
		}
	}
	impl := &provider.MockToolRunner{
		Script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall(fmt.Sprintf("c%d", n), ToolRun, map[string]string{"command": "sleep"}),
			}}, nil
		},
	}
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "wait"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r := &Runner{Architect: arch, Implementer: impl, Tools: tools, Workspace: ws, Verify: passing("ok")}
	r.Config.MaxRounds = 1
	r.Config.RoundDeadline = 20 * time.Millisecond

	res, err := r.Run(context.Background(), Task{Description: "task"})
	// The round is abandoned but the session still reaches a reviewed verdict.
	if err != nil && !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = res
}

func TestMissingCollaboratorsAreRejected(t *testing.T) {
	ws := &fakeWorkspace{}
	tools, _ := newTools(t)

	if _, err := (&Runner{Implementer: finishingRunner("x"), Tools: tools, Workspace: ws, Verify: passing("")}).
		Run(context.Background(), Task{}); err == nil {
		t.Error("expected an error with no architect")
	}
	if _, err := (&Runner{Architect: &fakeArchitect{}, Tools: tools, Workspace: ws, Verify: passing("")}).
		Run(context.Background(), Task{}); err == nil {
		t.Error("expected an error with no implementer")
	}
	if _, err := (&Runner{Architect: &fakeArchitect{}, Implementer: finishingRunner("x")}).
		Run(context.Background(), Task{}); err == nil {
		t.Error("expected an error with no workspace")
	}
}

// A broken sandbox on the baseline run stops the session before any model is
// asked anything — the same early exit loop.Run makes.
func TestBaselineVerificationFailureStopsTheSession(t *testing.T) {
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "x"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, head: "base"}
	verify := func(context.Context) (string, bool, error) {
		return "", false, fmt.Errorf("docker daemon not running")
	}
	r, _ := newRunner(t, arch, finishingRunner("x"), ws, verify)

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err == nil {
		t.Fatal("expected an error when the sandbox cannot run at all")
	}
	if len(arch.seen) != 0 {
		t.Error("no review should happen when the baseline could not run")
	}
}

func hasEvent(events []Event, phase, outcome string) bool {
	for _, e := range events {
		if e.Phase == phase && e.Outcome == outcome {
			return true
		}
	}
	return false
}
