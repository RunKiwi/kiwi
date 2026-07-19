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
	output, passed, err := runTest(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("loop: initial test run failed: %w", err)
	}
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
			content, err := os.ReadFile(task.FilePath)
			if err != nil {
				return Result{Steps: step - 1, CostUSD: cost, FinalOutput: lastOutput},
					fmt.Errorf("loop: read target file: %w", err)
			}

			r.logf("[loop] step %d: Actor proposing edit\n", step)
			proposed, err := r.Provider.GetCodeEdit(ctx, task.Description, task.FilePath, string(content),
				composeActorInput(lastOutput, criticReasons))
			if err != nil {
				return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput},
					fmt.Errorf("loop: actor failed: %w", err)
			}
			cost += callCost(r.Provider, nominalActorCost)
			criticReasons = ""

			// Optional Critic gate before we touch the file.
			if r.Critic != nil {
				verdict, err := r.Critic.ReviewEdit(ctx, task.Description, task.FilePath, string(content), proposed, lastOutput)
				if err != nil {
					return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput},
						fmt.Errorf("loop: critic failed: %w", err)
				}
				cost += callCost(r.Critic, nominalCriticCost)
				if !verdict.Approved {
					r.logf("[loop] step %d: Critic rejected: %s\n", step, verdict.Reasons)
					criticReasons = verdict.Reasons
					continue // Actor retries with feedback; nothing applied, no test
				}
			}

			if err := os.WriteFile(task.FilePath, []byte(proposed), 0o644); err != nil {
				return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput},
					fmt.Errorf("loop: write target file: %w", err)
			}
		}

		output, passed, err := runTest(ctx)
		if err != nil {
			return Result{Steps: step, CostUSD: cost, FinalOutput: lastOutput},
				fmt.Errorf("loop: test run failed: %w", err)
		}
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

// composeActorInput appends Critic feedback (if any) to the test output so the
// Actor sees why its previous attempt was rejected. Provider signatures are
// fixed, so feedback rides inside the buildOutput argument.
func composeActorInput(buildOutput, criticReasons string) string {
	if strings.TrimSpace(criticReasons) == "" {
		return buildOutput
	}
	return buildOutput + "\n\n[Critic feedback on your previous attempt]: " + criticReasons
}

// callCost returns the provider's reported cost for its last call, falling back
// to a nominal figure when the provider does not report usage (e.g. the mock).
func callCost(caller any, fallback float64) float64 {
	if r, ok := caller.(provider.UsageReporter); ok {
		return r.LastCostUSD()
	}
	return fallback
}

func (r *Runner) proposeMultiFileEdit(ctx context.Context, task *Task, cost *float64, lastOutput string, step int) error {
	var sb strings.Builder
	validFiles := make(map[string]string)

	for _, f := range task.Files {
		stat, err := os.Stat(f)
		if err != nil {
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

	system := "You are an expert software engineer in an automated fix loop. Make the SMALLEST changes to make the tests pass. Return ONLY JSON in this format: {\"files\":[{\"path\":\"<matching-input-path>\",\"content\":\"<full new file>\"}]}"
	user := fmt.Sprintf("Task: %s\n\nFiles:\n%s\nBuild/test output:\n%s", task.Description, sb.String(), lastOutput)

	resp, err := r.Provider.Complete(ctx, system, user)
	if err != nil {
		return fmt.Errorf("actor complete failed: %w", err)
	}
	*cost += callCost(r.Provider, nominalActorCost)

	start := strings.IndexByte(resp, '{')
	end := strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return fmt.Errorf("invalid json response from model")
	}

	var edit struct {
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}

	if err := json.Unmarshal([]byte(resp[start:end+1]), &edit); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	for _, f := range edit.Files {
		var match string
		for vf := range validFiles {
			if strings.HasSuffix(vf, f.Path) {
				match = vf
				break
			}
		}
		if match == "" {
			continue
		}

		if task.WorktreeRoot != "" {
			rel, err := filepath.Rel(task.WorktreeRoot, match)
			if err != nil || !filepath.IsLocal(rel) {
				continue
			}
		}

		if r.Critic != nil {
			oldC := validFiles[match]
			verdict, err := r.Critic.ReviewEdit(ctx, task.Description, match, oldC, f.Content, lastOutput)
			if err != nil {
				return fmt.Errorf("critic failed: %w", err)
			}
			*cost += callCost(r.Critic, nominalCriticCost)
			if !verdict.Approved {
				r.logf("[loop] step %d: Critic rejected edit for %s: %s\n", step, match, verdict.Reasons)
				continue
			}
		}

		if err := os.WriteFile(match, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
	}
	return nil
}
