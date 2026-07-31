// Package loop is the Actor–Critic execution loop, factored out so it can run
// in either execution context without either importing the other: the BYOC
// daemon (pkg/daemon) drives it against a Docker sandbox, and the control-plane
// orchestrator can drive it against its own infra.
//
// The loop depends only on pkg/provider (the LLM interface) and the local
// filesystem. Everything context-specific — how the test command actually runs,
// where credentials come from — is injected by the caller, so this package
// pulls in no sandbox, tunnel, checkpoint, or store dependency and can be
// imported from anywhere.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// TestFunc runs the task's verification command and reports its result. output
// is the combined build/test output shown to the Actor; passed is whether the
// task's definition of done is met; err is only for infrastructure failures
// (the sandbox itself broke), not a failing test — a failing test is
// passed=false with a nil error.
type TestFunc func(ctx context.Context) (output string, passed bool, err error)

// Task is one unit of work: edit FilePath so that the test command passes.
type Task struct {
	// Description is the natural-language goal handed to the Actor.
	Description string
	// FilePath is the absolute path to the single file the Actor may edit.
	FilePath string
	// Files is a list of absolute paths to files the Actor may edit (multi-file mode).
	Files []string
	// WorktreeRoot is the absolute path to the worktree root, required for multi-file path validation.
	WorktreeRoot string
}

// Config tunes the loop's safety rails. Zero values get sensible defaults.
type Config struct {
	// MaxSteps caps Actor iterations before giving up. Default 6.
	MaxSteps int
	// MaxBudgetUSD halts the loop once accumulated provider cost reaches it.
	// Default 0.50. A live agent on a customer's key must not run away.
	MaxBudgetUSD float64
	// Log receives human-readable progress lines. nil discards them.
	Log func(format string, a ...any)
	// OnEvent receives a structured record of every loop phase, in order. It is
	// how a caller that persists telemetry (the daemon, reporting back to the
	// Control Plane) learns what the Actor proposed and what the Critic ruled —
	// the log lines above are for humans and are not parseable. nil discards
	// them, so a caller that wants no telemetry pays nothing.
	OnEvent func(Event)
}

// Event is one structured phase of a loop run. It deliberately carries no task
// or org identity: the loop does not know them, and the caller that persists an
// Event is the one that can attribute it.
type Event struct {
	// Step is 0 for the initial test and 1..N for each Actor iteration.
	Step int
	// Phase ∈ initial_test | actor | critic | test.
	Phase string
	// Outcome ∈ pass | fail | proposed | approved | rejected | error.
	Outcome string
	// Detail is human-readable context: the Critic's reasons, or truncated test
	// output. Never assume it is safe to publish verbatim — test output can
	// carry secrets, so consumers that export it should hash rather than copy.
	Detail       string
	DurationMs   int64
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
}

// detailCap bounds how much of a Detail string an Event carries. Recent output
// is the useful part of a failing test, so we keep the tail.
const detailCap = 2000

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Trim on a rune boundary so the result is always valid UTF-8.
	cut := s[len(s)-n:]
	for len(cut) > 0 && !utf8.RuneStart(cut[0]) {
		cut = cut[1:]
	}
	return cut
}

// emit reports one phase to the caller, attributing token/cost usage to the
// provider that performed it. A nil OnEvent makes this a no-op.
func (r *Runner) emit(step int, phase, outcome, detail string, start time.Time, caller any) {
	if r.Config.OnEvent == nil {
		return
	}
	ev := Event{
		Step:       step,
		Phase:      phase,
		Outcome:    outcome,
		Detail:     tailOf(detail, detailCap),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if u, ok := caller.(provider.UsageReporter); ok {
		ev.CostUSD = u.LastCostUSD()
	}
	if t, ok := caller.(provider.TokenReporter); ok {
		ev.InputTokens, ev.OutputTokens = t.LastUsage()
	}
	r.Config.OnEvent(ev)
}

// Result reports the outcome of a loop run.
type Result struct {
	Success     bool    // the test command passed
	Steps       int     // Actor iterations performed
	CostUSD     float64 // accumulated provider cost
	FinalOutput string  // last test output (for logging / reporting)
}

// Runner executes the Actor–Critic loop. Critic is optional: when nil, every
// proposed edit is applied and gated only by the test command (the test is the
// review — the model this is built for, red CI -> green CI).
type Runner struct {
	Provider provider.Provider
	Critic   provider.Critic
	Config   Config
}

func (r *Runner) logf(format string, a ...any) {
	if r.Config.Log != nil {
		r.Config.Log(format, a...)
	}
}

// nominal per-call cost used when a provider does not report real token cost
// (e.g. the offline mock), so the budget path stays exercised in tests.
const (
	nominalActorCost  = 0.05
	nominalCriticCost = 0.02
	defaultMaxSteps   = 6
	defaultMaxBudget  = 0.50
	// dupOutputHalt stops the loop when the identical test output recurs this
	// many times — a sign the Actor is stuck making no progress.
	dupOutputHalt = 3
)

// Run drives the loop: run the test; if it already passes there is nothing to
// do. Otherwise repeatedly ask the Actor for a corrected file, optionally gate
// it through the Critic, apply it, and re-test — until the test passes, the
// budget or step cap is hit, or the Actor stalls.
func (r *Runner) Run(ctx context.Context, task Task, runTest TestFunc) (Result, error) {
	if r.Provider == nil {
		return Result{}, fmt.Errorf("loop: no provider configured")
	}
	maxSteps := r.Config.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	maxBudget := r.Config.MaxBudgetUSD
	if maxBudget <= 0 {
		maxBudget = defaultMaxBudget
	}

	// Initial test: the task may already be satisfied, in which case editing
	// anything would be wrong.
	initStart := time.Now()
	output, passed, err := runTest(ctx)
	if err != nil {
		r.emit(0, "initial_test", "error", err.Error(), initStart, nil)
		return Result{}, fmt.Errorf("loop: initial test run failed: %w", err)
	}
	r.emit(0, "initial_test", outcomeOf(passed), output, initStart, nil)
	if passed {
		r.logf("[loop] initial test already passes; nothing to do\n")
		return Result{Success: true, Steps: 0, FinalOutput: output}, nil
	}
	r.logf("[loop] initial test failed; entering correction loop\n")

	var cost float64
	criticReasons := ""
	outputCounts := map[string]int{output: 1}
	lastOutput := output

	for step := 1; step <= maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return Result{Steps: step - 1, CostUSD: cost, FinalOutput: lastOutput}, err
		}
		if cost >= maxBudget {
			r.logf("[loop] halted: budget $%.2f reached\n", maxBudget)
			return Result{Success: false, Steps: step - 1, CostUSD: cost, FinalOutput: lastOutput},
				fmt.Errorf("loop: budget limit ($%.2f) exceeded", maxBudget)
		}

		if len(task.Files) > 0 {
			r.logf("[loop] step %d: Actor proposing multi-file edit\n", step)
			if err := r.proposeMultiFileEdit(ctx, &task, &cost, lastOutput, step); err != nil {
				return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput}, fmt.Errorf("loop: multi-file propose failed: %w", err)
			}
			criticReasons = ""
		} else {
			// A target that does not exist yet is a file to CREATE, not an error.
			// "Add a cookie consent popup" plans a new component; failing here
			// meant every additive task died at step 1 with "no such file or
			// directory", before the Actor was ever asked anything. The Actor
			// returns whole file contents regardless, so empty content is the
			// correct starting point. Any other read error (permissions, a
			// directory in the way) is still fatal — those are not creations.
			content, err := os.ReadFile(task.FilePath)
			if err != nil {
				if !os.IsNotExist(err) {
					return Result{Steps: step - 1, CostUSD: cost, FinalOutput: lastOutput},
						fmt.Errorf("loop: read target file: %w", err)
				}
				content = nil
				r.logf("[loop] step %d: target %s does not exist; creating it\n", step, task.FilePath)
			}

			r.logf("[loop] step %d: Actor proposing edit\n", step)
			actorStart := time.Now()
			proposed, err := r.Provider.GetCodeEdit(ctx, task.Description, task.FilePath, string(content),
				composeActorInput(lastOutput, criticReasons))
			if err != nil {
				r.emit(step, "actor", "error", err.Error(), actorStart, r.Provider)
				return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput},
					fmt.Errorf("loop: actor failed: %w", err)
			}
			r.emit(step, "actor", "proposed", "", actorStart, r.Provider)
			cost += callCost(r.Provider, nominalActorCost)
			criticReasons = ""

			// Optional Critic gate before we touch the file.
			if r.Critic != nil {
				criticStart := time.Now()
				verdict, err := r.Critic.ReviewEdit(ctx, task.Description, task.FilePath, string(content), proposed, lastOutput)
				if err != nil {
					r.emit(step, "critic", "error", err.Error(), criticStart, r.Critic)
					return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput},
						fmt.Errorf("loop: critic failed: %w", err)
				}
				cost += callCost(r.Critic, nominalCriticCost)
				if !verdict.Approved {
					r.emit(step, "critic", "rejected", verdict.Reasons, criticStart, r.Critic)
					r.logf("[loop] step %d: Critic rejected: %s\n", step, verdict.Reasons)
					criticReasons = verdict.Reasons
					continue // Actor retries with feedback; nothing applied, no test
				}
				r.emit(step, "critic", "approved", verdict.Reasons, criticStart, r.Critic)
			}

			if err := writeTargetFile(task.FilePath, []byte(proposed)); err != nil {
				return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput},
					fmt.Errorf("loop: write target file: %w", err)
			}
		}

		testStart := time.Now()
		output, passed, err := runTest(ctx)
		if err != nil {
			r.emit(step, "test", "error", err.Error(), testStart, nil)
			return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput},
				fmt.Errorf("loop: test run failed: %w", err)
		}
		r.emit(step, "test", outcomeOf(passed), output, testStart, nil)
		if passed {
			r.logf("[loop] step %d: test passed\n", step)
			return Result{Success: true, Steps: step, CostUSD: cost, FinalOutput: output}, nil
		}
		r.logf("[loop] step %d: test still failing\n", step)
		lastOutput = output

		// Stall detection: the same failing output repeating means the Actor is
		// not making progress; stop rather than burn budget in a circle.
		outputCounts[output]++
		if outputCounts[output] >= dupOutputHalt {
			r.logf("[loop] halted: identical test output repeated %d times\n", dupOutputHalt)
			return Result{Success: false, Steps: step, CostUSD: cost, FinalOutput: output},
				fmt.Errorf("loop: stalled (identical failure repeated %d times)", dupOutputHalt)
		}
	}

	return Result{Success: false, Steps: maxSteps, CostUSD: cost, FinalOutput: lastOutput},
		fmt.Errorf("loop: reached max steps (%d) without passing", maxSteps)
}

// writeTargetFile writes content to path, creating any missing parent
// directories first. A newly created target ("src/components/CookieConsent.tsx")
// routinely lands in a directory that does not exist yet, and os.WriteFile does
// not create one — it would fail with the same "no such file or directory" the
// read used to, one step later.
func writeTargetFile(path string, content []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create parent directory: %w", err)
		}
	}
	return os.WriteFile(path, content, 0o644)
}

// composeActorInput appends Critic feedback (if any) to the test output so the
// Actor sees why its previous attempt was rejected. Provider signatures are
// fixed, so feedback rides inside the buildOutput argument.
func composeActorInput(buildOutput, criticReasons string) string {
	if strings.TrimSpace(criticReasons) == "" {
		return buildOutput
	}
	return buildOutput + "\n\n[Critic feedback on your previous attempt]: " + criticReasons
}

// outcomeOf maps a test result to the Event outcome vocabulary.
func outcomeOf(passed bool) string {
	if passed {
		return "pass"
	}
	return "fail"
}

// callCost returns the provider's reported cost for its last call, falling back
// to a nominal figure when the provider does not report usage (e.g. the mock).
func callCost(caller any, fallback float64) float64 {
	if r, ok := caller.(provider.UsageReporter); ok {
		return r.LastCostUSD()
	}
	return fallback
}

// truncationHint appends an explanation when a parse failure looks like a
// response that was cut off rather than one that was malformed.
//
// Providers that report a stop reason now raise provider.ErrTruncated before we
// get here, but not every provider does, and a truncated reply is otherwise
// indistinguishable from a syntax error: the extractor finds the last '}' in a
// half-written document, hands the parser a fragment, and the user is told
// "unexpected end of JSON input" — which says nothing about what to do. The
// giveaway is unbalanced braces.
func truncationHint(resp, raw string) string {
	s := raw
	if s == "" {
		s = resp
	}
	if strings.Count(s, "{") <= strings.Count(s, "}") {
		return ""
	}
	return " (the response appears to have been cut off mid-answer — the task may " +
		"involve too many or too large files for the model's output limit; see " +
		"KIWI_COMPLETION_MAX_TOKENS)"
}

// escapeControlCharsInStrings rewrites raw control characters that appear
// *inside* JSON string literals into their escape sequences, leaving the
// structural parts of the document untouched.
//
// It exists because an LLM asked for {"files":[{"content":"..."}]} will happily
// paste a source file in verbatim, newlines and tabs and all. JSON forbids
// unescaped control characters in strings (RFC 8259 §7), so the response is
// syntactically invalid even though the content it carries is exactly right.
// Escaping is a pure encoding repair: it changes no character the model chose,
// only how that character is spelled.
//
// It deliberately does not try to fix unescaped quotes — where a string ends
// would be a guess, and a wrong guess silently corrupts file content rather
// than failing loudly. Input it cannot help is returned unchanged, so the
// caller can report the model's original parse error.
func escapeControlCharsInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)

	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			// This byte is the target of a preceding backslash; copy verbatim so
			// a legitimate \" or \\ cannot flip our in-string state.
			escaped = false
			b.WriteByte(c)
		case c == '\\' && inString:
			escaped = true
			b.WriteByte(c)
		case c == '"':
			inString = !inString
			b.WriteByte(c)
		case inString && c < 0x20:
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			default:
				fmt.Fprintf(&b, `\u%04x`, c)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (r *Runner) proposeMultiFileEdit(ctx context.Context, task *Task, cost *float64, lastOutput string, step int) error {
	var sb strings.Builder
	validFiles := make(map[string]string)

	for _, f := range task.Files {
		stat, err := os.Stat(f)
		if err != nil {
			if !os.IsNotExist(err) {
				continue
			}
			// A target that does not exist yet is one to create. It still belongs
			// in validFiles: that map doubles as the write allowlist below, so
			// skipping it would let the Actor propose the file and then silently
			// discard the result. Its keys come from task.Files, which the caller
			// already validated, so admitting it widens nothing.
			validFiles[f] = ""
			sb.WriteString(fmt.Sprintf("File: %s (does not exist yet — create it)\n```\n```\n\n", f))
			continue
		}
		if stat.Size() > 256*1024 {
			continue
		}
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		validFiles[f] = string(content)
		sb.WriteString(fmt.Sprintf("File: %s\n```\n%s\n```\n\n", f, string(content)))
	}

	if len(validFiles) == 0 {
		return fmt.Errorf("no readable files within size limits")
	}

	// The response has to carry whole file contents, which makes output tokens
	// the binding constraint on this call: with several candidate files, echoing
	// all of them back exceeds any sane ceiling and the reply is truncated
	// mid-JSON. Hence the explicit instruction to return only what changed —
	// most tasks touch one or two files, and omitting the rest is the difference
	// between a reply that fits and one that does not. The JSON rules are spelled
	// out for the same reason the parser repairs them: models paste file content
	// in verbatim, with real newlines, unless told plainly not to.
	system := "You are an expert software engineer in an automated fix loop. " +
		"Make the SMALLEST changes needed. " +
		"Return ONLY JSON in this format: {\"files\":[{\"path\":\"<matching-input-path>\",\"content\":\"<full new file>\"}]}. " +
		"Include ONLY the files you actually modify — omit every file you leave unchanged. " +
		"Each content value must be a single valid JSON string: escape newlines as \\n and quotes as \\\", and never emit a literal newline inside it."
	user := fmt.Sprintf("Task: %s\n\nFiles:\n%s\nBuild/test output:\n%s", task.Description, sb.String(), lastOutput)

	actorStart := time.Now()
	resp, err := r.Provider.Complete(ctx, system, user)
	if err != nil {
		r.emit(step, "actor", "error", err.Error(), actorStart, r.Provider)
		return fmt.Errorf("actor complete failed: %w", err)
	}
	r.emit(step, "actor", "proposed", "", actorStart, r.Provider)
	*cost += callCost(r.Provider, nominalActorCost)

	start := strings.IndexByte(resp, '{')
	end := strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return fmt.Errorf("invalid json response from model%s", truncationHint(resp, ""))
	}

	var edit struct {
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}

	raw := resp[start : end+1]
	if err := json.Unmarshal([]byte(raw), &edit); err != nil {
		// Models routinely emit file content with *literal* newlines inside the
		// JSON string rather than \n escapes, which is invalid JSON ("invalid
		// character '\n' in string literal") and failed the whole step. The
		// content is recoverable — only its encoding is wrong — so escape the
		// stray control characters and retry once before giving up.
		repaired := escapeControlCharsInStrings(raw)
		if repaired == raw {
			return fmt.Errorf("parse json: %w", err)
		}
		if err2 := json.Unmarshal([]byte(repaired), &edit); err2 != nil {
			// Report the original error: it describes what the model actually
			// produced, which is the more useful thing to debug from.
			return fmt.Errorf("parse json: %w%s", err, truncationHint(resp, raw))
		}
	}

	for _, f := range edit.Files {
		// Reject empty/traversing paths outright. An empty path previously matched
		// an arbitrary target (HasSuffix(vf, "") is always true), letting a
		// malformed response overwrite an unrelated file in the set.
		rel := filepath.Clean(f.Path)
		if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}

		// Map the model's path to one of the input files. Accept an exact match
		// (absolute or worktree-relative) or a path-BOUNDARY suffix, so that
		// "config.go" cannot match "old_config.go". Matches are always keys of
		// validFiles, so a write can never land outside the target set.
		var abs string
		if task.WorktreeRoot != "" {
			abs = filepath.Join(task.WorktreeRoot, rel)
		}
		var match string
		for vf := range validFiles {
			if vf == rel || (abs != "" && vf == abs) || strings.HasSuffix(vf, string(os.PathSeparator)+rel) {
				match = vf
				break
			}
		}
		if match == "" {
			continue
		}

		if r.Critic != nil {
			oldC := validFiles[match]
			criticStart := time.Now()
			verdict, err := r.Critic.ReviewEdit(ctx, task.Description, match, oldC, f.Content, lastOutput)
			if err != nil {
				r.emit(step, "critic", "error", err.Error(), criticStart, r.Critic)
				return fmt.Errorf("critic failed: %w", err)
			}
			*cost += callCost(r.Critic, nominalCriticCost)
			if !verdict.Approved {
				r.emit(step, "critic", "rejected", verdict.Reasons, criticStart, r.Critic)
				r.logf("[loop] step %d: Critic rejected edit for %s: %s\n", step, match, verdict.Reasons)
				continue
			}
			r.emit(step, "critic", "approved", verdict.Reasons, criticStart, r.Critic)
		}

		if err := writeTargetFile(match, []byte(f.Content)); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
	}
	return nil
}
