package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

func TestEngineEmitsPhaseEvents(t *testing.T) {
	dir := t.TempDir()
	// A file the MockProvider knows how to "fix": a divide with no zero-check.
	target := filepath.Join(dir, "math_utils.go")
	os.WriteFile(target, []byte("package mathutils\n\nfunc Divide(a, b int) int { return a / b }\n"), 0644)
	// A test that fails until fixed (panics on divide by zero → output contains "divide by zero").
	os.WriteFile(filepath.Join(dir, "math_utils_test.go"), []byte(
		"package mathutils\n\nimport \"testing\"\n\nfunc TestD(t *testing.T){ _ = Divide(1,0) }\n"), 0644)

	eng := NewEngine(provider.NewMockProvider(), 5)
	eng.Critic = provider.NewMockCritic()
	devnull, _ := os.Open(os.DevNull)
	eng.LogOut = devnull // silence engine logs
	var events []TaskEvent
	eng.EventCallback = func(ev TaskEvent) { events = append(events, ev) }

	_ = eng.RunTask(context.Background(), "task-x", dir, "fix divide by zero", target, "go test "+dir)

	phases := map[string]bool{}
	for _, e := range events {
		phases[e.Phase] = true
		if e.DurationMs < 0 {
			t.Errorf("negative duration on %s", e.Phase)
		}
	}
	for _, want := range []string{"initial_test", "actor", "critic"} {
		if !phases[want] {
			t.Errorf("missing phase event %q; got events: %+v", want, events)
		}
	}
}
