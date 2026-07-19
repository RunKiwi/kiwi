package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os/exec"
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
