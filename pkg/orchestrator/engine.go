package orchestrator

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
)

// Engine orchestrates the feedback loop (TDD actor-critic control loop)
type Engine struct {
	Provider      provider.Provider
	MaxSteps      int
	LogOut        io.Writer
	StateCallback func(string)
	CostCallback  func(float64)
	MaxBudget     float64
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

// RunTask starts the feedback loop to align the codebase to the desired state.
func (e *Engine) RunTask(ctx context.Context, taskID string, dir string, task string, filePath string, testCmd string) error {
	e.log("[Orchestrator] Desired State: %s\n", task)
	e.log("[Orchestrator] Running initial test command: %s\n", testCmd)

	accumulatedCost := 0.0
	outputCounts := make(map[string]int)

	// Resolve target credentials over the tunnel if required
	env, err := e.resolveEnv(ctx, taskID, []string{"GITHUB_TOKEN"})
	if err != nil {
		return fmt.Errorf("failed to resolve tunnel environment: %w", err)
	}

	res, err := sandbox.RunCommand(ctx, dir, testCmd, env)
	if err != nil {
		return fmt.Errorf("sandbox failed to run command: %w", err)
	}

	accumulatedCost += 0.02
	if e.CostCallback != nil {
		e.CostCallback(0.02)
	}

	if res.Success {
		e.log("[Orchestrator] Current state matches desired state. Tests already pass!\n")
		return nil
	}

	e.log("[Orchestrator] Tests failed. Entering correction loop...\n")
	e.log("[Sandbox Output]:\n%s\n", res.Output)

	outputCounts[res.Output]++

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

		e.log("[Actor] Simulating edit proposal...\n")
		fixedCode, err := e.Provider.GetCodeEdit(ctx, task, filePath, string(content), res.Output)
		if err != nil {
			return fmt.Errorf("failed to get code edit: %w", err)
		}

		// Apply proposed patch/fix
		err = os.WriteFile(filePath, []byte(fixedCode), 0644)
		if err != nil {
			return fmt.Errorf("failed to write fix back to target file: %w", err)
		}
		e.log("[Actor] Applied proposed code edits to target file.\n")

		// Run compiler/tests in the sandbox
		e.log("[Sandbox] Re-running build/tests...\n")
		env, err = e.resolveEnv(ctx, taskID, []string{"GITHUB_TOKEN"})
		if err != nil {
			return fmt.Errorf("failed to resolve tunnel environment: %w", err)
		}
		res, err = sandbox.RunCommand(ctx, dir, testCmd, env)
		if err != nil {
			return fmt.Errorf("sandbox run failed: %w", err)
		}

		// Track cost
		accumulatedCost += 0.05
		if e.CostCallback != nil {
			e.CostCallback(0.05)
		}

		// Critic phase
		if res.Success {
			e.log("[Critic] Success: Tests passed, compiler errors cleared.\n")
			return nil
		}

		e.log("[Critic] Fail: Target state still diverging.\n")
		e.log("[Sandbox Output]:\n%s\n", res.Output)

		// Check duplicate output count for loop safety
		outputCounts[res.Output]++
		if outputCounts[res.Output] >= 3 {
			e.log("[Orchestrator Halt] Loop safety cut-off: recursive loop detected (compiler error repeated 3 times).\n")
			return fmt.Errorf("loop safety halt: recursive loop detected (compiler error repeated 3 times)")
		}
	}

	return fmt.Errorf("loop halted: reached max iterations (%d) without resolving task", e.MaxSteps)
}
