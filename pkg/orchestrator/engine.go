package orchestrator

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
)

// Engine orchestrates the feedback loop (TDD actor-critic control loop)
type Engine struct {
	Provider      provider.Provider
	Critic        provider.Critic
	MaxSteps      int
	LogOut        io.Writer
	StateCallback func(string)
	CostCallback  func(float64)
	MaxBudget     float64
	LLMMode       string // "mock" (default) or "anthropic"
	SandboxConfig *sandbox.SandboxConfig
	EventCallback func(TaskEvent)
}

// NewEngine creates a new Loop Orchestrator engine
func NewEngine(p provider.Provider, maxSteps int) *Engine {
	return &Engine{
		Provider:  p,
		MaxSteps:  maxSteps,
		LogOut:    os.Stdout,
		MaxBudget: 0.20, // Default max budget $0.20 for safe testing
	}
}

// Log writes formatted log messages to the configured output writer
func (e *Engine) log(format string, a ...interface{}) {
	if e.LogOut != nil {
		fmt.Fprintf(e.LogOut, format, a...)
	}
}

// resolveEnv requests credentials over the reverse tunnel.
// If the tunnel is disconnected, it pauses statefully and blocks until reconnect.
func (e *Engine) resolveEnv(ctx context.Context, taskID string, reqKeys []string) ([]string, error) {
	t := tunnel.GlobalRegistry.Get(taskID)
	if t == nil {
		return nil, nil // No tunnel registered for this task
	}

	var envs []string
	for _, key := range reqKeys {
		for {
			// Query secret (will read from cache instantly if resolved previously)
			val, err := t.GetSecret(ctx, key)
			if err == nil {
				envs = append(envs, fmt.Sprintf("%s=%s", key, val))
				break
			}

			// Secret is not in cache and tunnel is disconnected. Pause task state.
			e.log("[Orchestrator] Reverse tunnel disconnected. Pausing execution... (waiting for client to reconnect)\n")
			if e.StateCallback != nil {
				e.StateCallback("PAUSED")
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1 * time.Second):
			}
		}

		// Reconnected or read from cache, restore state to RUNNING if it was paused
		if e.StateCallback != nil {
			e.StateCallback("RUNNING")
		}
	}
	return envs, nil
}

// composeActorInput appends any Critic feedback to the build output so the
// Actor sees why its previous attempt was rejected. Signatures are fixed, so
// feedback rides along inside the buildOutput argument.
func composeActorInput(buildOutput, criticReasons string) string {
	if strings.TrimSpace(criticReasons) == "" {
		return buildOutput
	}
	return buildOutput + "\n\n[Critic feedback on your previous attempt]: " + criticReasons
}

// charge attributes the cost of the most recent call by `caller` to the budget.
// Live providers report real token cost via UsageReporter; the mock uses a
// small nominal fallback so the budget path stays exercised offline.
func (e *Engine) charge(acc *float64, caller interface{}, fallback float64) {
	cost := fallback
	if r, ok := caller.(provider.UsageReporter); ok {
		cost = r.LastCostUSD()
	}
	*acc += cost
	if e.CostCallback != nil {
		e.CostCallback(cost)
	}
}

// emit records a structured telemetry event for one loop phase. Best-effort:
// a nil callback is a no-op. Token/cost come from the caller when it reports them.
func (e *Engine) emit(step int, phase, outcome, detail string, dur time.Duration, caller interface{}) {
	if e.EventCallback == nil {
		return
	}
	ev := TaskEvent{
		Step:       step,
		Phase:      phase,
		Outcome:    outcome,
		Detail:     detail,
		DurationMs: dur.Milliseconds(),
	}
	if r, ok := caller.(provider.UsageReporter); ok {
		ev.CostUSD = r.LastCostUSD()
	}
	if tr, ok := caller.(provider.TokenReporter); ok {
		ev.InputTokens, ev.OutputTokens = tr.LastUsage()
	}
	e.EventCallback(ev)
}

// resolveAPIKey resolves ANTHROPIC_API_KEY through the reverse tunnel (cache
// first), falling back to the daemon environment. If neither is available it
// pauses statefully until the client reconnects.
func (e *Engine) resolveAPIKey(ctx context.Context, taskID string) (string, error) {
	t := tunnel.GlobalRegistry.Get(taskID)
	for {
		if t != nil {
			if val, err := t.GetSecret(ctx, "ANTHROPIC_API_KEY"); err == nil && val != "" {
				if e.StateCallback != nil {
					e.StateCallback("RUNNING")
				}
				return val, nil
			}
		}
		if val := os.Getenv("ANTHROPIC_API_KEY"); val != "" {
			return val, nil
		}
		e.log("[Orchestrator] ANTHROPIC_API_KEY unavailable. Pausing until client reconnects...\n")
		if e.StateCallback != nil {
			e.StateCallback("PAUSED")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

// RunTask starts the feedback loop to align the codebase to the desired state.
func (e *Engine) RunTask(ctx context.Context, taskID string, dir string, task string, filePath string, testCmd string) error {
	if e.SandboxConfig != nil {
		ctx = context.WithValue(ctx, sandbox.SandboxConfigKey, e.SandboxConfig)
	}

	e.log("[Orchestrator] Desired State: %s\n", task)
	e.log("[Orchestrator] Running initial test command: %s\n", testCmd)

	accumulatedCost := 0.0
	outputCounts := make(map[string]int)

	// Build the live provider on demand (key resolved via tunnel / env).
	if e.LLMMode == "anthropic" && e.Provider == nil {
		key, err := e.resolveAPIKey(ctx, taskID)
		if err != nil {
			return fmt.Errorf("failed to resolve ANTHROPIC_API_KEY: %w", err)
		}
		ap := provider.NewAnthropicProvider(key)
		e.Provider = ap
		e.Critic = ap
	}

	// Resolve target credentials over the tunnel if required
	env, err := e.resolveEnv(ctx, taskID, []string{"GITHUB_TOKEN"})
	if err != nil {
		return fmt.Errorf("failed to resolve tunnel environment: %w", err)
	}

	initStart := time.Now()
	res, err := sandbox.RunCommand(ctx, dir, testCmd, env)
	if err != nil {
		return fmt.Errorf("sandbox failed to run command: %w", err)
	}

	accumulatedCost += 0.02
	if e.CostCallback != nil {
		e.CostCallback(0.02)
	}

	if res.Success {
		e.emit(0, "initial_test", "pass", summarize(res.Output, 500), time.Since(initStart), nil)
		e.log("[Orchestrator] Current state matches desired state. Tests already pass!\n")
		return nil
	}
	e.emit(0, "initial_test", "fail", summarize(res.Output, 500), time.Since(initStart), nil)

	e.log("[Orchestrator] Tests failed. Entering correction loop...\n")
	e.log("[Sandbox Output]:\n%s\n", res.Output)

	outputCounts[res.Output]++

	lastBuildOutput := res.Output
	criticReasons := ""

	for step := 1; step <= e.MaxSteps; step++ {
		e.log("\n=== Loop Iteration %d / %d ===\n", step, e.MaxSteps)

		// Check budget
		if accumulatedCost >= e.MaxBudget {
			e.log("[Orchestrator Halt] Loop terminated: task budget limit ($%.2f) reached.\n", e.MaxBudget)
			return fmt.Errorf("loop halted: budget limit ($%.2f) exceeded", e.MaxBudget)
		}

		// Read current source code
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read target file: %w", err)
		}

		// Actor proposes an edit (not yet written).
		e.log("[Actor] Proposing edit...\n")
		actorStart := time.Now()
		proposed, err := e.Provider.GetCodeEdit(ctx, task, filePath, string(content), composeActorInput(lastBuildOutput, criticReasons))
		if err != nil {
			e.emit(step, "actor", "error", err.Error(), time.Since(actorStart), e.Provider)
			return fmt.Errorf("failed to get code edit: %w", err)
		}
		e.charge(&accumulatedCost, e.Provider, 0.05)
		e.emit(step, "actor", "proposed", "", time.Since(actorStart), e.Provider)
		criticReasons = ""

		// Critic reviews before applying.
		verdict := provider.Verdict{Approved: true, Reasons: "no critic configured"}
		if e.Critic != nil {
			criticStart := time.Now()
			verdict, err = e.Critic.ReviewEdit(ctx, task, filePath, string(content), proposed, lastBuildOutput)
			if err != nil {
				e.emit(step, "critic", "error", err.Error(), time.Since(criticStart), e.Critic)
				return fmt.Errorf("critic review failed: %w", err)
			}
			e.charge(&accumulatedCost, e.Critic, 0.02)
			outcome := "approved"
			if !verdict.Approved {
				outcome = "rejected"
			}
			e.emit(step, "critic", outcome, verdict.Reasons, time.Since(criticStart), e.Critic)
		}

		if !verdict.Approved {
			e.log("[Critic] Rejected edit: %s\n", verdict.Reasons)
			criticReasons = verdict.Reasons
			continue // do not apply, do not run tests; Actor retries with feedback
		}
		e.log("[Critic] Approved edit.\n")

		// Apply the approved edit.
		if err := os.WriteFile(filePath, []byte(proposed), 0644); err != nil {
			return fmt.Errorf("failed to write fix back to target file: %w", err)
		}
		e.log("[Actor] Applied approved edit to target file.\n")

		// Final gate: run the tests.
		e.log("[Sandbox] Re-running build/tests...\n")
		env, err = e.resolveEnv(ctx, taskID, []string{"GITHUB_TOKEN"})
		if err != nil {
			return fmt.Errorf("failed to resolve tunnel environment: %w", err)
		}
		testStart := time.Now()
		res, err = sandbox.RunCommand(ctx, dir, testCmd, env)
		if err != nil {
			return fmt.Errorf("sandbox run failed: %w", err)
		}

		if res.Success {
			e.emit(step, "test", "pass", summarize(res.Output, 500), time.Since(testStart), nil)
			e.log("[Gate] Success: tests passed.\n")
			return nil
		}

		e.emit(step, "test", "fail", summarize(res.Output, 500), time.Since(testStart), nil)
		e.log("[Gate] Fail: target state still diverging.\n")
		e.log("[Sandbox Output]:\n%s\n", res.Output)
		lastBuildOutput = res.Output

		// Check duplicate output count for loop safety
		outputCounts[res.Output]++
		if outputCounts[res.Output] >= 3 {
			e.log("[Orchestrator Halt] Loop safety cut-off: recursive loop detected (compiler error repeated 3 times).\n")
			return fmt.Errorf("loop safety halt: recursive loop detected (compiler error repeated 3 times)")
		}
	}

	return fmt.Errorf("loop halted: reached max iterations (%d) without resolving task", e.MaxSteps)
}
