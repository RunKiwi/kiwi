package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInferTestCmd(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"go", map[string]string{"go.mod": "module x"}, "go test ./..."},
		{"rust", map[string]string{"Cargo.toml": "[package]"}, "cargo test"},
		{"node_with_test", map[string]string{"package.json": `{"scripts":{"test":"jest"}}`}, "npm test"},
		{"node_without_test", map[string]string{"package.json": `{"scripts":{"build":"tsc"}}`}, ""},
		{"python", map[string]string{"pyproject.toml": "[tool.poetry]"}, "pytest"},
		{"make", map[string]string{"Makefile": "test:\n\tgo test ./...\n"}, "make test"},
		{"empty", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			if got := inferTestCmd(dir); got != tc.want {
				t.Errorf("inferTestCmd = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProviderNameForModel(t *testing.T) {
	if got := providerNameForModel("gemini-flash-latest"); got != "Gemini" {
		t.Errorf("gemini model → %q, want Gemini", got)
	}
	if got := providerNameForModel("claude-opus-4-8"); got != "Anthropic" {
		t.Errorf("claude model → %q, want Anthropic", got)
	}
}
