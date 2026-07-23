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
//
// newModel builds the Completer for a given model id, so the planning model can
// be chosen per request (PlanRequest.PlannerModel) while still running on the
// Control Plane's own key. defaultModel is used when the request doesn't ask for
// a specific one.
type LLMPlanner struct {
	newModel     func(model string) Completer
	defaultModel string
}

// NewLLMPlanner wires a planner to a single fixed Completer (the model id is
// ignored). Kept for tests and simple single-model setups.
func NewLLMPlanner(model Completer) *LLMPlanner {
	return &LLMPlanner{newModel: func(string) Completer { return model }}
}

// NewLLMPlannerFunc wires a planner that builds its Completer per request from
// the requested model id, falling back to defaultModel.
func NewLLMPlannerFunc(newModel func(model string) Completer, defaultModel string) *LLMPlanner {
	return &LLMPlanner{newModel: newModel, defaultModel: defaultModel}
}

const plannerSystem = "You are the Planner in an autonomous coding swarm. " +
	"Decompose the user's task into a DAG of small, independently-executable worker jobs. " +
	"Scope each worker by the file it edits and a test command that defines 'done' — NOT a persona. " +
	"Respond ONLY with a JSON object: " +
	`{"summary": string, "workers": [{"id": string, "task": string, "file": string, "model": string, "test_cmd": string, "depends_on": [string]}]}.`

func (p *LLMPlanner) Plan(ctx context.Context, req PlanRequest) (*Plan, error) {
	if p.newModel == nil {
		return nil, fmt.Errorf("llm planner: no model configured")
	}
	plannerModel := req.PlannerModel
	if plannerModel == "" {
		plannerModel = p.defaultModel
	}
	model := p.newModel(plannerModel)
	if model == nil {
		return nil, fmt.Errorf("llm planner: no model configured")
	}
	// Prepend distilled prior-work context to the planner prompt. The learnings
	// are always same-org (resolved under req.OrgID in SubmitPlan), so this text
	// is the tenant's own data — an org can only influence its own planning, not
	// another's. Kept bounded (top 3, summaries and total both capped) so a large
	// history can't blow up the prompt.
	var contextBlock string
	if len(req.ResolvedLearnings) > 0 {
		var sb strings.Builder
		sb.WriteString("# Prior related work (for context)\n\n")
		for i, l := range req.ResolvedLearnings {
			if i >= 3 {
				break
			}
			outcome := "unknown"
			if l.Outcome != nil && *l.Outcome != "" {
				outcome = *l.Outcome
			}
			pr := "none"
			if l.PRURL != nil && *l.PRURL != "" {
				pr = *l.PRURL
			}

			summary := l.Summary
			if len(summary) > 2000 {
				summary = summary[:2000] + "..."
			}

			sb.WriteString(fmt.Sprintf("%s • %s • %s • %s • %s\n", l.Task, l.Repo, outcome, summary, pr))
		}
		contextBlock = sb.String()
		if len(contextBlock) > 12000 {
			contextBlock = contextBlock[:12000] + "\n...[truncated]\n"
		}
		contextBlock += "\n"
	}

	user := fmt.Sprintf("%sTask: %s\nRepo: %s @ %s\nTarget file (if known): %s\nTest command (definition of done): %s\nMax workers: %d",
		contextBlock, req.Task, req.RepoURL, req.Ref, req.File, req.TestCmd, req.MaxWorkers)

	raw, err := model.Complete(ctx, plannerSystem, user)
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
