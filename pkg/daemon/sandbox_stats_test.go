package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestParseDockerStatsOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  []ContainerMemStats
		shouldErr bool
	}{
		{
			name:  "standard multi-container output",
			input: "a1b2c3d4e5f6 512.3MiB / 4GiB\nb2c3d4e5f6a7 1.2GiB / 4GiB\n",
			expected: []ContainerMemStats{
				{
					ContainerID: "a1b2c3d4e5f6",
					RSSMB:       512,
					LimitMB:     4096,
				},
				{
					ContainerID: "b2c3d4e5f6a7",
					RSSMB:       1228,
					LimitMB:     4096,
				},
			},
			shouldErr: false,
		},
		{
			name:      "empty output",
			input:     "",
			expected:  nil,
			shouldErr: false,
		},
		{
			name:      "whitespace only output",
			input:     "   \n\n  \t\n",
			expected:  nil,
			shouldErr: false,
		},
		{
			name:  "various units: GiB, MiB, KiB, bytes/B, TiB",
			input: "c1 100KiB / 2048MiB\nc2 1048576B / 2097152B\nc3 500B / 1GiB\nc4 0B / 0B\nc5 1TiB / 2TiB\n",
			expected: []ContainerMemStats{
				{
					ContainerID: "c1",
					RSSMB:       0,
					LimitMB:     2048,
				},
				{
					ContainerID: "c2",
					RSSMB:       1,
					LimitMB:     2,
				},
				{
					ContainerID: "c3",
					RSSMB:       0,
					LimitMB:     1024,
				},
				{
					ContainerID: "c4",
					RSSMB:       0,
					LimitMB:     0,
				},
				{
					ContainerID: "c5",
					RSSMB:       1048576,
					LimitMB:     2097152,
				},
			},
			shouldErr: false,
		},
		{
			name:  "SI units: kB, MB, GB",
			input: "c6 500MB / 2GB\nc7 2048kB / 100MB\n",
			expected: []ContainerMemStats{
				{
					ContainerID: "c6",
					RSSMB:       500,
					LimitMB:     2048,
				},
				{
					ContainerID: "c7",
					RSSMB:       2,
					LimitMB:     100,
				},
			},
			shouldErr: false,
		},
		{
			name:      "malformed: missing slash",
			input:     "a1b2c3d4e5f6 512.3MiB 4GiB\n",
			shouldErr: true,
		},
		{
			name:      "malformed: missing usage",
			input:     "a1b2c3d4e5f6 / 4GiB\n",
			shouldErr: true,
		},
		{
			name:      "malformed: missing limit",
			input:     "a1b2c3d4e5f6 512.3MiB /\n",
			shouldErr: true,
		},
		{
			name:      "malformed: invalid usage number",
			input:     "a1b2c3d4e5f6 invalid / 4GiB\n",
			shouldErr: true,
		},
		{
			name:      "malformed: invalid limit number",
			input:     "a1b2c3d4e5f6 512.3MiB / invalid\n",
			shouldErr: true,
		},
		{
			name:      "malformed: unknown unit",
			input:     "a1b2c3d4e5f6 512foo / 4GiB\n",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDockerStatsOutput(tt.input)
			if tt.shouldErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d stats, got %d", len(tt.expected), len(got))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("stats[%d] = %+v, want %+v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseDockerMemSize(t *testing.T) {
	tests := []struct {
		input     string
		expected  int64
		shouldErr bool
	}{
		{"0B", 0, false},
		{"0", 0, false},
		{"500B", 0, false},
		{"1048576B", 1, false},
		{"1024KiB", 1, false},
		{"2048KiB", 2, false},
		{"512MiB", 512, false},
		{"512.3MiB", 512, false},
		{"1.2GiB", 1228, false},
		{"4GiB", 4096, false},
		{"1TiB", 1048576, false},
		{"2GB", 2048, false},
		{"100MB", 100, false},
		{"  256 MiB  ", 256, false},
		{"", 0, true},
		{"xyz", 0, true},
		{"-100MB", 0, true},
		{"512unknown", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDockerMemSize(tt.input)
			if tt.shouldErr {
				if err == nil {
					t.Fatalf("parseDockerMemSize(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDockerMemSize(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("parseDockerMemSize(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

type fakeDockerStatsRunner struct {
	output []byte
	err    error
}

func (f fakeDockerStatsRunner) run(ctx context.Context) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return f.output, f.err
}

func TestCurrentSandboxMemStatsFailsSoft(t *testing.T) {
	// Calling currentSandboxMemStats must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("currentSandboxMemStats panicked: %v", r)
		}
	}()

	// Save original runner and restore after test
	orig := statsRunner
	defer func() { statsRunner = orig }()

	// Test normal stats request with fake output
	statsRunner = fakeDockerStatsRunner{
		output: []byte("a1b2c3d4e5f6 512.3MiB / 4GiB\n"),
		err:    nil,
	}
	stats, err := currentSandboxMemStats(context.Background())
	if err != nil {
		t.Errorf("expected no error with valid output, got %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("expected 1 stat, got %d", len(stats))
	}

	// Test error case
	statsRunner = fakeDockerStatsRunner{
		output: []byte("error output"),
		err:    fmt.Errorf("docker command failed"),
	}
	_, err = currentSandboxMemStats(context.Background())
	if err == nil {
		t.Error("expected error when docker command fails, got nil")
	}
	if !strings.Contains(err.Error(), "docker stats") {
		t.Errorf("expected error to wrap 'docker stats', got %v", err)
	}

	// Test with cancelled context fails gracefully with properly wrapped error
	statsRunner = fakeDockerStatsRunner{
		output: nil,
		err:    nil,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, cancelErr := currentSandboxMemStats(ctx)
	if cancelErr == nil {
		t.Error("expected error with cancelled context, got nil")
	}
	if !strings.Contains(cancelErr.Error(), "docker stats") {
		t.Errorf("expected cancel error to wrap 'docker stats', got %v", cancelErr)
	}
}
