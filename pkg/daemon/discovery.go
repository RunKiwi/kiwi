package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

func isBinaryOrSkip(path string) bool {
	parts := strings.Split(path, string(filepath.Separator))
	for _, p := range parts {
		if p == "vendor" || p == "node_modules" || p == ".git" {
			return true
		}
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".ico", ".pdf", ".exe", ".dll", ".so", ".dylib", ".bin", ".zip", ".tar", ".gz", ".bz2", ".7z":
		return true
	}
	return false
}

func repoTree(worktreePath string) ([]string, error) {
	var paths []string

	cmd := exec.Command("git", "ls-files")
	cmd.Dir = worktreePath
	out, err := cmd.Output()

	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || isBinaryOrSkip(line) {
				continue
			}
			paths = append(paths, line)
			if len(paths) >= 2000 {
				break
			}
		}
		if len(paths) > 0 {
			return paths, nil
		}
	}

	paths = nil
	err = filepath.WalkDir(worktreePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "node_modules" || d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(worktreePath, path)
		if err != nil {
			return nil
		}

		if isBinaryOrSkip(rel) {
			return nil
		}
		paths = append(paths, rel)
		if len(paths) >= 2000 {
			return filepath.SkipAll
		}
		return nil
	})

	return paths, err
}

// resolveHint maps a planner-supplied path onto one that actually exists in the
// repo, returning "" when it cannot.
//
// The planner never sees the repository — it is handed the repo URL, not its
// contents — so every path it emits is a guess from the model's priors about
// how a project is usually laid out. Those guesses are often nearly right:
// "components/Footer.tsx" against a repo that keeps it at
// "src/components/Footer.tsx" is a miss by one path segment. Trusting the guess
// verbatim creates a duplicate component nothing imports; resolving it first
// edits the file the user actually meant.
//
// Matching is exact first, then on the file name alone. A name that matches
// several files is resolved by whichever candidate shares the most trailing
// path segments with the hint, and left unresolved when that is still a tie —
// an ambiguous guess is worth less than asking the model to choose from the
// real tree, which is what the caller does next.
func resolveHint(hint string, tree []string) string {
	want := path.Clean(filepath.ToSlash(strings.TrimSpace(hint)))
	if want == "" || want == "." || want == "/" {
		return ""
	}

	for _, p := range tree {
		if path.Clean(filepath.ToSlash(p)) == want {
			return want
		}
	}

	base := path.Base(want)
	var candidates []string
	for _, p := range tree {
		q := path.Clean(filepath.ToSlash(p))
		if path.Base(q) == base {
			candidates = append(candidates, q)
		}
	}
	switch len(candidates) {
	case 0:
		return ""
	case 1:
		return candidates[0]
	}

	best, bestScore, tied := "", -1, false
	for _, c := range candidates {
		switch score := commonSuffixSegments(want, c); {
		case score > bestScore:
			best, bestScore, tied = c, score, false
		case score == bestScore:
			tied = true
		}
	}
	if tied {
		return ""
	}
	return best
}

// commonSuffixSegments counts the trailing path segments two slash-separated
// paths share, so "src/components/Footer.tsx" scores higher against the hint
// "components/Footer.tsx" than "legacy/Footer.tsx" does.
func commonSuffixSegments(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for i, j := len(as)-1, len(bs)-1; i >= 0 && j >= 0 && as[i] == bs[j]; i, j = i-1, j-1 {
		n++
	}
	return n
}

func discoverTargetFiles(ctx context.Context, actor provider.Provider, task string, tree []string) ([]string, error) {
	if len(tree) == 0 {
		return nil, nil
	}

	system := "You are an expert software engineer. Given a task and a list of repository files, return a JSON array of the most relevant repo-relative paths to edit, ordered by most-likely first. Respond with ONLY the JSON array."
	user := fmt.Sprintf("Task: %s\n\nRepository Files:\n%s\n", task, strings.Join(tree, "\n"))

	resp, err := actor.Complete(ctx, system, user)
	if err != nil {
		return nil, fmt.Errorf("discovery complete failed: %w", err)
	}

	start := strings.IndexByte(resp, '[')
	end := strings.LastIndexByte(resp, ']')
	if start == -1 || end == -1 || start >= end {
		return nil, nil
	}

	jsonStr := resp[start : end+1]

	var discovered []string
	if err := json.Unmarshal([]byte(jsonStr), &discovered); err != nil {
		return nil, nil
	}

	treeMap := make(map[string]bool)
	for _, p := range tree {
		treeMap[p] = true
	}

	var valid []string
	for _, p := range discovered {
		if !treeMap[p] {
			continue
		}
		if !filepath.IsLocal(p) {
			continue
		}
		valid = append(valid, p)
		if len(valid) >= 6 {
			break
		}
	}

	return valid, nil
}
