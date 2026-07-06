package orchestrator

import (
	"strings"
	"testing"
)

func TestComposeActorInput(t *testing.T) {
	// No critic feedback → just the build output.
	if got := composeActorInput("build failed", ""); got != "build failed" {
		t.Errorf("no feedback: got %q", got)
	}
	// With feedback → build output plus a clearly delimited critic note.
	got := composeActorInput("build failed", "you forgot the zero check")
	if !strings.Contains(got, "build failed") || !strings.Contains(got, "you forgot the zero check") {
		t.Errorf("with feedback: got %q", got)
	}
	if !strings.Contains(got, "Critic feedback") {
		t.Errorf("expected a labelled critic section, got %q", got)
	}
}
