package orchestrator

import (
	"context"
	"fmt"
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
)

// Engine orchestrates the feedback loop (TDD actor-critic control loop)
type Engine struct {
	Provider provider.Provider
	MaxSteps int
}

// NewEngine creates a new Loop Orchestrator engine
func NewEngine(p provider.Provider, maxSteps int) *Engine {
	return &Engine{
		Provider: p,
		MaxSteps: maxSteps,
	}
}

// RunTask starts the feedback loop to align the codebase to the desired state.
func (e *Engine) RunTask(ctx context.Context, dir string, task string, filePath string, testCmd string) error {
	fmt.Printf("[Orchestrator] Desired State: %s\n", task)
	fmt.Printf("[Orchestrator] Running initial test command: %s\n", testCmd)

	res, err := sandbox.RunCommand(ctx, dir, testCmd)
	if err != nil {
		return fmt.Errorf("sandbox failed to run command: %w", err)
	}

	if res.Success {
		fmt.Println("[Orchestrator] Current state matches desired state. Tests already pass!")
		return nil
	}

	fmt.Println("[Orchestrator] Tests failed. Entering correction loop...")
	fmt.Printf("[Sandbox Output]:\n%s\n", res.Output)

	for step := 1; step <= e.MaxSteps; step++ {
		fmt.Printf("\n=== Loop Iteration %d / %d ===\n", step, e.MaxSteps)

		// Read current source code
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read target file: %w", err)
		}

		fmt.Println("[Actor] Simulating edit proposal...")
		fixedCode, err := e.Provider.GetCodeEdit(ctx, task, filePath, string(content), res.Output)
		if err != nil {
			return fmt.Errorf("failed to get code edit: %w", err)
		}

		// Apply proposed patch/fix
		err = os.WriteFile(filePath, []byte(fixedCode), 0644)
		if err != nil {
			return fmt.Errorf("failed to write fix back to target file: %w", err)
		}
		fmt.Println("[Actor] Applied proposed code edits to target file.")

		// Run compiler/tests in the sandbox
		fmt.Println("[Sandbox] Re-running build/tests...")
		res, err = sandbox.RunCommand(ctx, dir, testCmd)
		if err != nil {
			return fmt.Errorf("sandbox run failed: %w", err)
		}

		// Critic phase
		if res.Success {
			fmt.Println("[Critic] Success: Tests passed, compiler errors cleared.")
			return nil
		}

		fmt.Println("[Critic] Fail: Target state still diverging.")
		fmt.Printf("[Sandbox Output]:\n%s\n", res.Output)
	}

	return fmt.Errorf("loop halted: reached max iterations (%d) without resolving task", e.MaxSteps)
}
