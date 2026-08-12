package provider

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Adaptive thinking is not available on every Anthropic model, and asking for
// it where it is unsupported is fatal rather than ignored:
//
//	400 invalid_request_error: adaptive thinking is not supported on this model
//
// It arrived with the Claude 4.6 generation. Older models — Haiku 4.5, Sonnet
// 4.5, Opus 4.5, and anything 4.1 or earlier — take the previous
// {type: "enabled", budget_tokens: N} form instead, and reject adaptive.
//
// Every Anthropic call sent adaptive unconditionally, so non-agentic calls were broken
// end to end on claude-haiku-4-5-20251001 — which is the dashboard's
// DEFAULT_WORKER_MODEL. The planner default (claude-opus-4-8) does support it,
// so the failure looked strange from the outside: the plan succeeded and then
// every worker died on its first Actor call.
//
// adaptiveThinkingModels lists the prefixes that support it. A prefix rather
// than an exact id because Anthropic ids carry optional date suffixes, and the
// generation is what determines support.
var adaptiveThinkingModels = []string{
	"claude-fable-5",
	"claude-mythos-5",
	"claude-opus-5",
	"claude-opus-4-8",
	"claude-opus-4-7",
	"claude-opus-4-6",
	"claude-sonnet-5",
	"claude-sonnet-4-6",
}

// supportsAdaptiveThinking reports whether a model accepts adaptive thinking.
//
// The list is an allowlist, not a denylist, and that direction is deliberate.
// An unknown model — a new release, a typo, a customer's alias — gets no
// thinking rather than a 400. Losing thinking costs some quality on one call;
// guessing wrong in the other direction fails the task outright.
func supportsAdaptiveThinking(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range adaptiveThinkingModels {
		if strings.HasPrefix(m, prefix) {
			return true
		}
	}
	return false
}

// thinkingFor returns the thinking configuration to send for a model: adaptive
// where supported, and the zero union (the field is omitted) everywhere else.
//
// Omitting is always valid. The alternative for older models is the deprecated
// budget_tokens form, which is not worth carrying: it needs a token budget
// strictly below MaxTokens, and every model Kiwi defaults to either supports
// adaptive or is cheap enough that the loop's own iteration does the work.
func thinkingFor(model string) anthropic.ThinkingConfigParamUnion {
	if !supportsAdaptiveThinking(model) {
		return anthropic.ThinkingConfigParamUnion{}
	}
	return anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
}
