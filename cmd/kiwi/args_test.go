package main

import (
	"flag"
	"testing"
)

func TestParseFlagsAnywhere(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantPos   []string
		wantKind  string
		wantToken string
	}{
		{
			name:      "flags after positionals (the bug)",
			args:      []string{"git", "ghp_abc", "-kind", "git", "-token", "T"},
			wantPos:   []string{"git", "ghp_abc"},
			wantKind:  "git",
			wantToken: "T",
		},
		{
			name:      "flags before positionals",
			args:      []string{"-token", "T", "-kind", "llm", "anthropic", "sk-1"},
			wantPos:   []string{"anthropic", "sk-1"},
			wantKind:  "llm",
			wantToken: "T",
		},
		{
			name:      "interleaved",
			args:      []string{"git", "-token", "T", "ghp_abc", "-kind", "git"},
			wantPos:   []string{"git", "ghp_abc"},
			wantKind:  "git",
			wantToken: "T",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			kind := fs.String("kind", "generic", "")
			token := fs.String("token", "", "")
			pos, err := parseFlagsAnywhere(fs, tc.args)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(pos) != len(tc.wantPos) || pos[0] != tc.wantPos[0] || pos[1] != tc.wantPos[1] {
				t.Errorf("positionals = %v, want %v", pos, tc.wantPos)
			}
			if *kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", *kind, tc.wantKind)
			}
			if *token != tc.wantToken {
				t.Errorf("token = %q, want %q", *token, tc.wantToken)
			}
		})
	}
}
