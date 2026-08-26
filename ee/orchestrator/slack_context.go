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

// slackCompleter builds a Control-Plane-side LLM call for these calls
// (context sufficiency, repo inference, thread-reply classification), which
// all run on the Control Plane rather than a customer's daemon and so need
// their own key rather than the org's.
//
// The cheapest Kiwi-funded OpenRouter *frontier*-tier model in the catalog is
// tried first — picked at call time, not hardcoded, so a discovery refresh
// that finds a cheaper qualifying model routes here automatically. Economy
// tier was the original choice and is wrong for this call: these three
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
func (s *Server) slackCompleter(ctx context.Context) (completeFunc, error) {
	if or, ok, err := s.storage.CheapestKiwiFundedModel(ctx, store.GlobalCatalogOrg, provider.ProviderOpenRouter, store.TierFrontier); err == nil && ok {
		if key, ok := provider.PlatformKeyFor(provider.ProviderOpenRouter); ok && key != "" {
			if spec, ok := provider.SpecFor(provider.ProviderOpenRouter); ok {
				p := provider.NewOpenAICompatibleProvider(key, or, or, spec.BaseURL, spec.ID)
				return p.Complete, nil
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
	return p.Complete, nil
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
