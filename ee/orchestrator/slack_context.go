// ee/orchestrator/slack_context.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// fixedLookback is how many prior channel messages a fresh (non-thread)
// trigger pulls before asking whether that's enough. Tuning knob, not a
// contract — widen based on real usage rather than a guess made here.
const fixedLookback = 10

// escalatedLookback is how far back a second attempt goes when the first
// window was judged insufficient. Also a tuning knob.
const escalatedLookback = 50

type completeFunc func(ctx context.Context, system, user string) (string, error)

// slackCompleterTimeout bounds a single completion call slackCompleter's
// completeFunc makes. Without it, a slow frontier model (a "thinking"
// reasoning model, picked because it's the cheapest Kiwi-funded frontier
// candidate, can easily run past a minute) can consume the entirety of
// handleSlackTrigger's own 60s budget (slack_webhook.go) on one call, and
// worse, leave no time — and a context already past its deadline — to post
// even the fallback "not sure" reply, which is what silently swallowed the
// reply in the incident this constant fixes. 15s leaves room for the up to
// two or three completer calls one trigger can make (context sufficiency,
// repo inference, thread-reply classification, investigation hint) inside
// that 60s budget, while still failing an individual slow call fast enough
// that something can be reported back.
const slackCompleterTimeout = 15 * time.Second

// boundCompleter wraps a completeFunc so every call gets its own bounded
// sub-context, independent of (but derived from, so it still respects) the
// caller's own deadline or cancellation.
func boundCompleter(inner completeFunc) completeFunc {
	return func(ctx context.Context, system, user string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, slackCompleterTimeout)
		defer cancel()
		return inner(ctx, system, user)
	}
}

// replyTimeout bounds the fresh context replyCtx substitutes in — long
// enough for a couple of Slack API calls and a DB write, short enough that a
// caller which is itself context-bound (a webhook goroutine, ultimately)
// doesn't hang past its own request lifetime for no reason.
const replyTimeout = 10 * time.Second

// replyCtx returns ctx unchanged when it's still live, or a fresh
// short-lived context when it has already expired or been cancelled.
//
// One or more slackCompleter calls (bound individually by slackCompleterTimeout,
// but there can be several in one trigger — context sufficiency, repo
// inference, thread-reply classification) can together still exhaust the
// caller's own budget (handleSlackTrigger's 60s in slack_webhook.go). Posting
// a reply on that same, by-then-expired context — including the "not sure
// which repository" fallback that exists specifically to tell the user
// something went wrong — fails immediately and silently, since PostMessage's
// error is not otherwise surfaced anywhere a human sees it. The caller should
// switch to this context for everything from the point it might report an
// outcome (a reaction, a reply, persisting the triggered-task row) onward, so
// a request that ran out of time upstream can still leave a visible trace.
func replyCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), replyTimeout)
}

// slackCompleter builds a Control-Plane-side LLM call for these calls
// (context sufficiency, repo inference, thread-reply classification), which
// all run on the Control Plane rather than a customer's daemon and so need
// their own key rather than the org's.
//
// The cheapest Kiwi-funded OpenRouter *frontier*-tier model in the catalog
// that doesn't look like a slow reasoning model (looksLikeSlowReasoningModel)
// is tried first — picked at call time, not hardcoded, so a discovery
// refresh that finds a cheaper qualifying model routes here automatically.
// Economy tier was the original choice and is wrong for this call: these three
// decisions gate whether a task runs at all (wrong repo, false-ambiguous,
// misclassified continue/fork/new), so a few cents of margin isn't worth
// picking the weakest model in the catalog — see the resolveSlackRepo
// incident where the cheapest economy model (mistral-nemo) returned
// low-confidence or unparseable output for an unambiguous "repo is
// runkiwi/docs" message. No org is in play for this call (it is a
// Control-Plane operating cost, not billed to any org's allowance), so the
// lookup reads the global catalog and skips the per-org entitlement check
// SubmitPlan's equivalent runs. Gemini is the fallback for a deployment with
// no OpenRouter platform key configured, or whose catalog has not discovered
// a qualifying frontier model yet — the operator override
// (KIWI_SLACK_INFERENCE_MODEL) still applies there, not to the catalog pick.
// slackCompleterCandidateLimit is how many cheapest frontier candidates
// slackCompleter fetches before giving up on OpenRouter and falling to
// Gemini. Wide enough to reliably contain at least one non-reasoning model
// alongside the reasoning-named ones a catalog refresh can surface (5
// candidates at the time this was written split 3 reasoning / 2 not), not a
// promise every catalog will have one within this window.
const slackCompleterCandidateLimit = 5

// looksLikeSlowReasoningModel flags model ids that follow common
// "reasoning"/"thinking" naming conventions across OpenRouter providers —
// DeepSeek's -r1/-reasoner suffix, Qwen's -thinking/QwQ line, OpenAI's
// o1/o3/o4 series. These models spend a variable, often long, hidden
// deliberation budget before answering, which is fundamentally at odds with
// slackCompleterTimeout's 15s bound: a prod incident traced to Cloud Logging
// showed the then-cheapest Kiwi-funded frontier candidate, deepseek-r1,
// reliably eating its full 15s budget on every completer call, discarding
// thread history fetchSlackContext had already gathered and leaving repo
// inference nothing to work with. Name matching is a heuristic, not a
// promise — a reasoning model with no recognizable naming convention (e.g.
// minimax-m1, seen in the same catalog) will still slip through.
func looksLikeSlowReasoningModel(modelID string) bool {
	m := strings.ToLower(modelID)
	for _, marker := range []string{"-r1", "/r1", "thinking", "reasoner", "qwq", "-o1", "/o1", "-o3", "/o3", "-o4", "/o4"} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

// pickNonReasoningCandidate returns the first candidate — already ordered
// cheapest-first by CheapestKiwiFundedModels — that doesn't look like a slow
// reasoning model, or false if every candidate does.
func pickNonReasoningCandidate(candidates []string) (string, bool) {
	for _, c := range candidates {
		if !looksLikeSlowReasoningModel(c) {
			return c, true
		}
	}
	return "", false
}

func (s *Server) slackCompleter(ctx context.Context) (completeFunc, error) {
	if key, ok := provider.PlatformKeyFor(provider.ProviderOpenRouter); ok && key != "" {
		if spec, ok := provider.SpecFor(provider.ProviderOpenRouter); ok {
			candidates, err := s.storage.CheapestKiwiFundedModels(ctx, store.GlobalCatalogOrg, provider.ProviderOpenRouter, store.TierFrontier, slackCompleterCandidateLimit)
			if err == nil {
				if or, ok := pickNonReasoningCandidate(candidates); ok {
					// Logged so the next latency incident is a one-line log
					// grep, not an inference from gaps between SQL
					// timestamps — that inference is exactly how the
					// previous incident's model was identified, and it was
					// only ever a best guess.
					log.Printf("[slackapp] slackCompleter picked %s (candidates: %v)", or, candidates)
					p := provider.NewOpenAICompatibleProvider(key, or, or, spec.BaseURL, spec.ID)
					return boundCompleter(p.Complete), nil
				}
				log.Printf("[slackapp] every frontier candidate looked like a reasoning model, falling to Gemini (candidates: %v)", candidates)
			}
		}
	}

	model := os.Getenv("KIWI_SLACK_INFERENCE_MODEL")
	if model == "" {
		model = "gemini-flash-latest"
	}
	key, ok := provider.PlatformKeyFor(provider.ProviderGemini)
	if !ok || key == "" {
		return nil, fmt.Errorf("no platform key configured for Slack inference")
	}
	p := provider.NewGeminiProviderWithModels(key, model, model)
	return boundCompleter(p.Complete), nil
}

// assembleContext judges one window of history against the sufficiency
// question and returns the composed task description when the LLM says
// it's enough. No I/O: history is already fetched, complete is already
// bound — this is what makes the sufficiency logic itself table-testable.
func assembleContext(ctx context.Context, complete completeFunc, history []string, triggerText string) (string, error) {
	sufficient, err := isContextSufficient(ctx, complete, history, triggerText)
	if err != nil {
		return "", err
	}
	if !sufficient {
		return "", errInsufficientContext
	}
	return composeTaskDescription(history, triggerText), nil
}

var errInsufficientContext = fmt.Errorf("insufficient context in the fixed lookback window")

// assembleContextEscalating tries the fixed window first, and only pulls the
// wider escalated window when the first judged itself insufficient — the
// escalation is one extra call, not a habit, so the common case (already
// discussed in-thread) pays for exactly one LLM round trip.
func assembleContextEscalating(ctx context.Context, complete completeFunc, history, escalatedHistory []string, triggerText string) (string, error) {
	got, err := assembleContext(ctx, complete, history, triggerText)
	if err == nil {
		return got, nil
	}
	if err != errInsufficientContext {
		return "", err
	}
	return assembleContext(ctx, complete, escalatedHistory, triggerText)
}

func isContextSufficient(ctx context.Context, complete completeFunc, history []string, triggerText string) (bool, error) {
	system := "You judge whether a Slack conversation gives enough context to act on an instruction. " +
		"Respond with ONLY a JSON object: {\"sufficient\": true|false}."
	user := fmt.Sprintf("Instruction: %s\n\nConversation so far:\n%s", triggerText, strings.Join(history, "\n"))
	resp, err := complete(ctx, system, user)
	if err != nil {
		return false, err
	}
	var out struct {
		Sufficient bool `json:"sufficient"`
	}
	start, end := strings.IndexByte(resp, '{'), strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return false, fmt.Errorf("no JSON object in sufficiency response")
	}
	if err := json.Unmarshal([]byte(resp[start:end+1]), &out); err != nil {
		return false, fmt.Errorf("parse sufficiency response: %w", err)
	}
	return out.Sufficient, nil
}

func composeTaskDescription(history []string, triggerText string) string {
	if len(history) == 0 {
		return triggerText
	}
	return fmt.Sprintf("Context from the conversation:\n%s\n\nInstruction: %s", strings.Join(history, "\n"), triggerText)
}

// fetchSlackContext gets the fixed-lookback window (whole thread if
// threadTS is set, else the last fixedLookback channel messages) and, when
// judged insufficient, the escalated one — the I/O half assembleContext and
// assembleContextEscalating deliberately have none of.
func (s *Server) fetchSlackContext(ctx context.Context, token, channelID, threadTS, triggerText string) (string, error) {
	complete, err := s.slackCompleter(ctx)
	if err != nil {
		log.Printf("[slackapp] no completer available, falling back to trigger text only: %v", err)
		return triggerText, nil
	}

	fetch := func(limit int) ([]string, error) {
		var msgs []string
		if threadTS != "" {
			hist, err := s.slackClient.ConversationReplies(ctx, token, channelID, threadTS)
			if err != nil {
				return nil, err
			}
			for _, m := range hist {
				// Sanitized the same way the triggering message is
				// (instructionFromSlack): an old test:"..."/repo:owner/name
				// token sitting in channel or thread history must not reach
				// the Architect as literal task text just because it happened
				// to be quoted while assembling context — those tokens are
				// only ever meant to be read off the CURRENT trigger message,
				// which handleSlackTrigger/handleSlackThreadReply already do
				// directly against the raw text, not through this history.
				msgs = append(msgs, m.UserID+": "+instructionFromSlack(m.Text))
			}
			return msgs, nil
		}
		hist, err := s.slackClient.ConversationHistory(ctx, token, channelID, limit)
		if err != nil {
			return nil, err
		}
		for _, m := range hist {
			// Sanitized the same way the triggering message is
			// (instructionFromSlack): an old test:"..."/repo:owner/name
			// token sitting in channel or thread history must not reach
			// the Architect as literal task text just because it happened
			// to be quoted while assembling context — those tokens are
			// only ever meant to be read off the CURRENT trigger message,
			// which handleSlackTrigger/handleSlackThreadReply already do
			// directly against the raw text, not through this history.
			msgs = append(msgs, m.UserID+": "+instructionFromSlack(m.Text))
		}
		return msgs, nil
	}

	history, err := fetch(fixedLookback)
	if err != nil {
		return triggerText, nil // best effort: fall back to the bare trigger rather than fail the task
	}

	got, err := assembleContext(ctx, complete, history, triggerText)
	if err == nil {
		return got, nil
	}
	if err != errInsufficientContext {
		return triggerText, nil
	}

	escalated, err := fetch(escalatedLookback)
	if err != nil {
		return composeTaskDescription(history, triggerText), nil
	}
	got, err = assembleContext(ctx, complete, escalated, triggerText)
	if err != nil {
		// Still insufficient after escalating: use what we have rather than
		// refuse the task outright — the spec's fallback is asking the user
		// to clarify, which Task 9 wires as a reply when this returns "".
		return composeTaskDescription(escalated, triggerText), nil
	}
	return got, nil
}
