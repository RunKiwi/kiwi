package daemon

import (
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
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
