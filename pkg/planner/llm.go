package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Completer is the minimal frontier-model surface the LLMPlanner needs. It is
// satisfied by an adapter over the Anthropic/Codex/Gemini providers, and is
// trivially faked in tests (no network).
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// LLMPlanner asks a frontier model (e.g. Fable) to decompose a task into a DAG
// of workers. It implements Planner, so callers depend only on the interface.
type LLMPlanner struct {
	model Completer
}

func NewLLMPlanner(model Completer) *LLMPlanner { return &LLMPlanner{model: model} }

const plannerSystem = "You are the Planner in an autonomous coding swarm. " +
	"Decompose the user's task into a DAG of small, independently-executable worker jobs. " +
	"Scope each worker by the file it edits and a test command that defines 'done' — NOT a persona. " +
	"Respond ONLY with a JSON object: " +
	`{"summary": string, "workers": [{"id": string, "task": string, "file": string, "model": string, "test_cmd": string, "depends_on": [string]}]}.`

func (p *LLMPlanner) Plan(ctx context.Context, req PlanRequest) (*Plan, error) {
	if p.model == nil {
		return nil, fmt.Errorf("llm planner: no model configured")
	}
	user := fmt.Sprintf("Task: %s\nRepo: %s @ %s\nTarget file (if known): %s\nTest command (definition of done): %s\nMax workers: %d",
		req.Task, req.RepoURL, req.Ref, req.File, req.TestCmd, req.MaxWorkers)

	raw, err := p.model.Complete(ctx, plannerSystem, user)
	if err != nil {
		return nil, fmt.Errorf("planner model call failed: %w", err)
	}

	var plan Plan
	if err := json.Unmarshal([]byte(extractJSON(raw)), &plan); err != nil {
		return nil, fmt.Errorf("planner returned invalid JSON: %w", err)
	}
	if len(plan.Workers) == 0 {
		return nil, fmt.Errorf("planner returned no workers")
	}

	// Ensure every worker carries the loop's scope: a model, a target file, and
	// a test command that defines "done" (#130). Fall back to the request's
	// values when the model omitted them, so a worker is always executable.
	for i := range plan.Workers {
		if plan.Workers[i].Model == "" {
			plan.Workers[i].Model = req.Model
		}
		if plan.Workers[i].File == "" {
			plan.Workers[i].File = req.File
		}
		if plan.Workers[i].TestCmd == "" {
			plan.Workers[i].TestCmd = req.TestCmd
		}
	}
	return &plan, nil
}

// extractJSON pulls a JSON object out of a possibly fenced model response.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "json")
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
