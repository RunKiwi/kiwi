package provider

import "testing"

// Compile-time proof AnthropicProvider satisfies all three interfaces.
var (
	_ Provider      = (*AnthropicProvider)(nil)
	_ Critic        = (*AnthropicProvider)(nil)
	_ UsageReporter = (*AnthropicProvider)(nil)
)

func TestNewAnthropicProviderConstructs(t *testing.T) {
	p := NewAnthropicProvider("test-key-not-used")
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.LastCostUSD() != 0 {
		t.Fatalf("expected zero initial cost, got %v", p.LastCostUSD())
	}
}
