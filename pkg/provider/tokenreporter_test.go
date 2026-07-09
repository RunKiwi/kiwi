package provider

import "testing"

var _ TokenReporter = (*AnthropicProvider)(nil)

func TestAnthropicLastUsage(t *testing.T) {
	p := NewAnthropicProvider("test-key")
	in, out := p.LastUsage()
	if in != 0 || out != 0 {
		t.Fatalf("initial usage should be zero, got in=%d out=%d", in, out)
	}
	p.lastInput = 1200
	p.lastOutput = 340
	in, out = p.LastUsage()
	if in != 1200 || out != 340 {
		t.Errorf("LastUsage got in=%d out=%d want 1200/340", in, out)
	}
}
