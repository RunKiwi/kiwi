package provider

import "testing"

// Adaptive thinking arrived with the Claude 4.6 generation. Sending it to an
// older model is a hard 400, not a silently ignored field:
//
//	adaptive thinking is not supported on this model
//
// claude-haiku-4-5-20251001 is the case that matters most — it is the
// dashboard's DEFAULT_WORKER_MODEL, so getting it wrong broke tasks on the
// default path for every Anthropic user.
func TestSupportsAdaptiveThinking(t *testing.T) {
	supported := []string{
		"claude-opus-5",
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-sonnet-5",
		"claude-sonnet-4-6",
		"claude-fable-5",
		// Date suffixes are optional on Anthropic ids; the generation decides.
		"claude-sonnet-4-6-20260101",
	}
	for _, m := range supported {
		if !supportsAdaptiveThinking(m) {
			t.Errorf("supportsAdaptiveThinking(%q) = false, want true", m)
		}
	}

	unsupported := []string{
		"claude-haiku-4-5-20251001", // the dashboard default worker model
		"claude-haiku-4-5",
		"claude-sonnet-4-5",
		"claude-opus-4-5",
		"claude-opus-4-1",
		"claude-3-5-sonnet",
		"claude-3-5-haiku",
		"gemini-flash-latest",
		"gpt-5-mini",
		"",
	}
	for _, m := range unsupported {
		if supportsAdaptiveThinking(m) {
			t.Errorf("supportsAdaptiveThinking(%q) = true, want false — this 400s the request", m)
		}
	}
}

// The near-miss pairs: a prefix check that is too loose would classify the 4.5
// generation as 4.6-capable and reintroduce the bug.
func TestAdaptiveDoesNotBleedAcrossGenerations(t *testing.T) {
	pairs := []struct {
		capable, notCapable string
	}{
		{"claude-opus-4-6", "claude-opus-4-5"},
		{"claude-sonnet-5", "claude-sonnet-4-5"},
		{"claude-opus-5", "claude-opus-4-1"},
	}
	for _, p := range pairs {
		if !supportsAdaptiveThinking(p.capable) {
			t.Errorf("%q should support adaptive", p.capable)
		}
		if supportsAdaptiveThinking(p.notCapable) {
			t.Errorf("%q must not be treated as adaptive-capable (near-miss of %q)", p.notCapable, p.capable)
		}
	}
}

// An unknown model must degrade to no thinking rather than to a 400. A new
// release, a typo, or a customer alias should cost quality on one call, never
// the whole task.
func TestUnknownModelOmitsThinkingRatherThanGuessing(t *testing.T) {
	cfg := thinkingFor("some-model-released-next-year")
	if cfg.OfAdaptive != nil {
		t.Error("an unrecognised model must not be sent adaptive thinking")
	}

	// Zero union means the field is omitted from the request entirely, which is
	// valid on every model.
	if cfg.OfEnabled != nil || cfg.OfDisabled != nil {
		t.Error("expected the thinking field to be omitted, not set to another mode")
	}
}

func TestThinkingForSetsAdaptiveOnSupportedModels(t *testing.T) {
	cfg := thinkingFor("claude-opus-4-8")
	if cfg.OfAdaptive == nil {
		t.Fatal("claude-opus-4-8 supports adaptive thinking and should be sent it")
	}
}
