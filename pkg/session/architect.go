package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// Architect plans a task and reviews each round's work. One logical
// conversation spans the whole task, which is what the single-file loop's
// Critic is not: that one reviews a single diff with no memory of prior rounds,
// so a reviewer cannot notice that it is asking for the same thing twice.
type Architect interface {
	// Plan writes the first spec, before any work has been done.
	Plan(ctx context.Context, in PlanInput) (Spec, error)
	// Review judges a completed round and writes the next spec — or approves,
	// or abandons.
	Review(ctx context.Context, in ReviewInput) (Spec, error)
	// Usage reports what the Architect has spent so far.
	Usage() provider.ToolUsage
}

// PlanInput is everything the Architect sees before the first round.
type PlanInput struct {
	Task string
	// RepoMap is the file listing. The Architect does not get tools — it reasons
	// about a repository through what the Implementer reports and what the diff
	// shows — so this is its only direct view of the tree, and it is the reason
	// the planner no longer has to guess filenames from a URL.
	RepoMap []string
	// TestCmd is the verification command, and BaselineOutput/BaselinePassed are
	// what it did before anything was touched.
	TestCmd         string
	BaselineOutput  string
	BaselinePassed  bool
	RepoContext     string
	PriorLearnings  []string
	MaxRoundsBudget int
}

// ReviewInput is everything the Architect sees about a completed round.
type ReviewInput struct {
	Task string
	// Spec is what this round was told to do, so the review can be against the
	// brief rather than against the reviewer's recollection.
	Spec  Spec
	Round int
	// Diff is the accumulated diff for the WHOLE task, not this round's slice.
	// Reviewing one file's rewrite in isolation is precisely what today's Critic
	// does and why it cannot catch a change that is locally reasonable and
	// globally wrong.
	Diff         string
	FilesChanged []string
	HandoffNote  string
	// Answers, NewQuestions and Decisions are the structured half of the round's
	// Report — the return channel Spec.OpenQuestions opens. Before this existed,
	// whatever the Implementer had to say about an open question was folded into
	// HandoffNote as prose, indistinguishable from everything else in it: there
	// was no way to check that a question actually got answered rather than
	// quietly dropped.
	Answers      []string
	NewQuestions []string
	Decisions    []string
	VerifyOutput string
	VerifyPassed bool
	// History is the compacted account of earlier rounds: what was asked, what
	// happened. This is the Architect's memory, reconstructed by the Runner
	// rather than held as a live provider transcript.
	History         []string
	RoundsRemaining int
}

// LLMArchitect drives an Architect over any provider that can Complete.
//
// Complete is deliberately all it needs. The Architect does not use tools — it
// reads diffs and reports — so every provider Kiwi already supports can serve
// this role today, including the two that cannot yet hold a tool conversation.
// That is what makes the expensive-planner / cheap-implementer split available
// to every org rather than only to Anthropic ones.
type LLMArchitect struct {
	Provider provider.Provider
	Model    string
	usage    provider.ToolUsage
}

func (a *LLMArchitect) Usage() provider.ToolUsage { return a.usage }

func (a *LLMArchitect) record() {
	var u provider.ToolUsage
	if r, ok := a.Provider.(provider.UsageReporter); ok {
		u.CostUSD = r.LastCostUSD()
	}
	if r, ok := a.Provider.(provider.TokenReporter); ok {
		u.InputTokens, u.OutputTokens = r.LastUsage()
	}
	a.usage.Add(u)
}

const architectSystem = `You are the Architect on an automated software change. You own one task from
the user's request through to the pull request: you write the specification, you review what the
implementer produced, and you decide when it is done.

An implementer with its own tools (read, grep, write, run) carries out each round. It starts a FRESH
context every round and does not remember previous ones — everything it needs must be in your spec.

Hold these in mind:

1. The task description is the objective. The test command is a GUARD that proves the change broke
   nothing; a passing suite does NOT mean the work is done. Additive work ("add an example", "add an
   endpoint") typically leaves a green suite green — that is success only if the thing was actually
   built.
2. Review the accumulated diff against your own acceptance criteria, not against a vague sense of
   quality. If the criteria are met, approve.
3. Do not ask for the same change twice. If a round failed to satisfy you, work out why — an
   implementer that cannot comply usually was not told something it needed, or was asked for
   something impossible.
4. Prefer approving a correct, minimal change over pursuing polish. Every extra round costs the
   user money and time.
5. If the task cannot be done in this repository, or the request is based on a false premise, return
   verdict "abandon" and explain. That is a real answer, not a failure.
6. open_questions are answered, not just asked. A round that received them returns answers, in order,
   or — where it could not resolve one — a new question of its own; both appear in your next review
   under their own headings, along with any implementation decisions worth knowing. Read them before
   writing the next spec. Do not ask a question that was already answered.

Respond ONLY with a JSON object:

{
  "verdict": "proceed" | "revise" | "approve" | "abandon",
  "rationale": "why this verdict, in a sentence or two",
  "objective": "what this round must achieve (required for proceed/revise)",
  "acceptance_criteria": ["checkable statements you will review against"],
  "must_change": ["paths you expect to change"],
  "must_not_change": ["paths that must not be touched — e.g. a failing test that defines the job"],
  "hints": ["what you learned that saves the implementer time"],
  "open_questions": ["things to resolve from the code"],
  "summary": "on approve: the pull request body describing what changed and why"
}`

// Plan writes the opening spec.
func (a *LLMArchitect) Plan(ctx context.Context, in PlanInput) (Spec, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task\n%s\n", in.Task)
	if in.RepoContext != "" {
		fmt.Fprintf(&b, "\n# Repository conventions (from AGENT.md)\n%s\n", in.RepoContext)
	}
	if len(in.PriorLearnings) > 0 {
		b.WriteString("\n# What earlier work on this repository learned\n")
		for _, l := range in.PriorLearnings {
			fmt.Fprintf(&b, "- %s\n", l)
		}
	}
	fmt.Fprintf(&b, "\n# Verification command\n%s\n", orNone(in.TestCmd))
	state := "FAILING"
	if in.BaselinePassed {
		state = "PASSING"
	}
	fmt.Fprintf(&b, "\nIt currently reports %s:\n```\n%s\n```\n", state, in.BaselineOutput)
	if in.BaselinePassed {
		b.WriteString("\nThe suite already passes. That is the guard working, not a reason to conclude there is " +
			"nothing to do: make the change the task asks for and keep the suite green.\n")
	}
	fmt.Fprintf(&b, "\n# Repository files\n%s\n", strings.Join(in.RepoMap, "\n"))
	fmt.Fprintf(&b, "\nYou have at most %d rounds. Write the spec for round 1.\n", in.MaxRoundsBudget)

	resp, err := a.Provider.Complete(ctx, architectSystem, b.String())
	a.record()
	if err != nil {
		return Spec{}, fmt.Errorf("architect planning failed: %w", err)
	}
	spec, err := parseSpec(resp)
	if err != nil {
		return Spec{}, err
	}
	// An opening verdict of approve is nonsense — nothing has been done yet —
	// and would silently deliver an empty pull request.
	if spec.Verdict == VerdictApprove {
		return Spec{}, fmt.Errorf("architect approved before any work was done")
	}
	return spec, nil
}

// Review judges a round.
func (a *LLMArchitect) Review(ctx context.Context, in ReviewInput) (Spec, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task\n%s\n", in.Task)

	if len(in.History) > 0 {
		b.WriteString("\n# Rounds so far\n")
		for _, h := range in.History {
			fmt.Fprintf(&b, "%s\n", h)
		}
	}

	fmt.Fprintf(&b, "\n# The spec you wrote for round %d\n%s\n", in.Round, in.Spec.Prompt())
	fmt.Fprintf(&b, "\n# The implementer's handoff note\n%s\n", orNone(in.HandoffNote))

	if len(in.Answers) > 0 {
		b.WriteString("\n# Answers to this round's open questions\n")
		for _, a := range in.Answers {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}
	if len(in.NewQuestions) > 0 {
		b.WriteString("\n# Questions the implementer could not resolve\nAddress these in your next spec — as a new open_question, a hint, or by changing the objective.\n")
		for _, q := range in.NewQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
	}
	if len(in.Decisions) > 0 {
		b.WriteString("\n# Implementation decisions made this round\n")
		for _, d := range in.Decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	}

	if len(in.FilesChanged) > 0 {
		fmt.Fprintf(&b, "\n# Files changed so far\n%s\n", strings.Join(in.FilesChanged, "\n"))
	}
	fmt.Fprintf(&b, "\n# Accumulated diff for the whole task\n```diff\n%s\n```\n", orNone(in.Diff))

	state := "FAILED"
	if in.VerifyPassed {
		state = "PASSED"
	}
	fmt.Fprintf(&b, "\n# Verification %s\n```\n%s\n```\n", state, in.VerifyOutput)

	if in.Diff == "" {
		b.WriteString("\nNOTE: the diff is empty — this round changed nothing. Either the implementer could not " +
			"act on your spec, or the work was already present. Do not approve an empty diff: there would be " +
			"no pull request to open.\n")
	}
	if !in.VerifyPassed {
		b.WriteString("\nNOTE: verification is failing, so this cannot be approved yet. Say specifically what to fix.\n")
	}

	fmt.Fprintf(&b, "\n%d round(s) remain after this one. Review and return your verdict.\n", in.RoundsRemaining)

	resp, err := a.Provider.Complete(ctx, architectSystem, b.String())
	a.record()
	if err != nil {
		return Spec{}, fmt.Errorf("architect review failed: %w", err)
	}
	spec, err := parseSpec(resp)
	if err != nil {
		return Spec{}, err
	}

	// The Architect is not permitted to approve work that does not exist or does
	// not build. It is told both above; this enforces it, because an approval
	// here becomes a pull request and a green tick to the user.
	//
	// This mirrors the rule the single-file loop arrived at the hard way: a run
	// that changes nothing must not be reported as success, or the user learns
	// that a green tick means nothing.
	if spec.Verdict == VerdictApprove {
		switch {
		case in.Diff == "":
			spec.Verdict = VerdictRevise
			spec.Rationale = "approval refused: the diff is empty, so there is nothing to deliver. " + spec.Rationale
			if spec.Objective == "" {
				spec.Objective = in.Spec.Objective
			}
		case !in.VerifyPassed:
			spec.Verdict = VerdictRevise
			spec.Rationale = "approval refused: verification is still failing. " + spec.Rationale
			if spec.Objective == "" {
				spec.Objective = in.Spec.Objective
			}
		}
	}
	return spec, nil
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
