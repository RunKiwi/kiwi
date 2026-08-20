// ee/orchestrator/slack_context_test.go
package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestAssembleSlackContextUsesFixedLookbackWhenSufficient(t *testing.T) {
	history := []string{"U1: the login page 500s on bad passwords", "U2: seeing it in prod too"}
	complete := func(ctx context.Context, system, user string) (string, error) {
		if strings.Contains(user, "500s on bad passwords") {
			return `{"sufficient": true}`, nil
		}
		t.Fatalf("unexpected prompt: %s", user)
		return "", nil
	}
	got, err := assembleContext(context.Background(), complete, history, "fix this bug")
	if err != nil {
		t.Fatalf("assembleContext: %v", err)
	}
	if !strings.Contains(got, "500s on bad passwords") || !strings.Contains(got, "fix this bug") {
		t.Fatalf("got %q", got)
	}
}

func TestAssembleSlackContextEscalatesWhenInsufficient(t *testing.T) {
	calls := 0
	complete := func(ctx context.Context, system, user string) (string, error) {
		calls++
		if calls == 1 {
			return `{"sufficient": false}`, nil
		}
		return `{"sufficient": true}`, nil
	}
	history := []string{"U1: something's wrong"}
	escalated := []string{"U1: something's wrong", "U2: it's the login flow, 500 on bad password"}
	got, err := assembleContextEscalating(context.Background(), complete, history, escalated, "fix this")
	if err != nil {
		t.Fatalf("assembleContextEscalating: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected exactly one escalation call, got %d total calls", calls)
	}
	if !strings.Contains(got, "500 on bad password") {
		t.Fatalf("expected the escalated history in the result, got %q", got)
	}
}
