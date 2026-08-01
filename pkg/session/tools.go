package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// Tool names. They are referenced by the rails in session.go, so they are
// constants rather than string literals scattered across two files.
const (
	ToolListFiles = "list_files"
	ToolReadFile  = "read_file"
	ToolWriteFile = "write_file"
	ToolGrep      = "grep"
	ToolRun       = "run"
	ToolInstall   = "install"
	ToolFinish    = "finish"
)

// ToolHost executes the Implementer's tool calls.
//
// Returning an error means the round cannot continue — the sandbox broke, the
// workspace vanished. A tool that merely failed (no such file, command exited
// non-zero) is a ToolResult with IsError set: that is ordinary feedback the
// model should see and react to, and it is the same distinction loop.TestFunc
// draws between a failing test and a broken sandbox.
type ToolHost interface {
	Defs() []provider.ToolDef
	Call(ctx context.Context, call provider.ToolCall) (provider.ToolResult, error)
}

// ExecFunc runs a shell command in the sandbox and reports its combined output.
// The session package never imports pkg/sandbox: the caller injects this, the
// same way pkg/loop takes a TestFunc, so this package stays importable from
// anywhere and carries no container dependency.
type ExecFunc func(ctx context.Context, command string) (output string, ok bool, err error)

// InstallFunc runs the repository's dependency install with network access and
// no credentials — phase A of the two-phase sandbox. The Implementer cannot do
// this itself, and must not: its own shell is offline, and the phase that has
// the network is deliberately the one holding nothing worth stealing.
type InstallFunc func(ctx context.Context) (output string, ok bool, err error)

// FileTools is the default ToolHost: file operations executed on the host in
// Go, and shell commands executed in the sandbox.
//
// The split is deliberate. Reading, writing, listing and grepping do not
// execute anything, so running them in the daemon costs no isolation and saves
// a container round-trip per call — and it means the model cannot reach a shell
// through a filename. Only `run` crosses into the sandbox, which is where
// model-chosen commands belong.
type FileTools struct {
	// Root is the worktree. Every path is resolved inside it and no tool can
	// escape it.
	Root string
	// Exec runs a command in the sandbox. Required for ToolRun.
	Exec ExecFunc
	// Install runs the brokered dependency install. Nil disables ToolInstall,
	// which is correct for a repository that declares no install step.
	Install InstallFunc
	// MaxOutputBytes caps what one tool result may return. Zero uses a default.
	// A model that cats a 40MB lockfile should get a truncated answer, not a
	// round that dies on the provider's input limit.
	MaxOutputBytes int
	// MaxWriteBytes caps a single write_file. Zero uses a default.
	MaxWriteBytes int

	// finished records the note passed to ToolFinish, if any.
	finished bool
	note     string
}

const (
	defaultMaxOutputBytes = 64 * 1024
	defaultMaxWriteBytes  = 1024 * 1024
	maxListEntries        = 2000
	maxGrepMatches        = 200
)

// Finished reports whether the Implementer called finish, and its handoff note.
func (t *FileTools) Finished() (bool, string) { return t.finished, t.note }

// Reset clears the per-round finish state so one FileTools can serve several
// rounds — the worktree is the same, only the conversation is new.
func (t *FileTools) Reset() {
	t.finished = false
	t.note = ""
}

// Defs describes the tools to the model.
func (t *FileTools) Defs() []provider.ToolDef {
	defs := []provider.ToolDef{
		{
			Name:        ToolListFiles,
			Description: "List files in the repository, relative to the repository root. Use this first to orient yourself.",
			Properties: map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory to list, relative to the repository root. Defaults to the root."},
			},
		},
		{
			Name:        ToolReadFile,
			Description: "Read a file from the repository.",
			Properties: map[string]any{
				"path": map[string]any{"type": "string", "description": "File path relative to the repository root."},
			},
			Required: []string{"path"},
		},
		{
			Name:        ToolGrep,
			Description: "Search the repository for a Go regular expression. Returns matching lines with their file and line number.",
			Properties: map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Go regular expression."},
				"path":    map[string]any{"type": "string", "description": "Optional directory to restrict the search to."},
			},
			Required: []string{"pattern"},
		},
		{
			Name:        ToolWriteFile,
			Description: "Write a file, creating it and any parent directories if needed. Supply the file's complete new contents.",
			Properties: map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path relative to the repository root."},
				"content": map[string]any{"type": "string", "description": "The complete new contents of the file."},
			},
			Required: []string{"path", "content"},
		},
		{
			Name: ToolFinish,
			Description: "Call this when the round's work is done. Pass a handoff note: what you changed, what you found, " +
				"and anything the next round should know. Your conversation ends here, so put in the note anything worth remembering.",
			Properties: map[string]any{
				"note": map[string]any{"type": "string", "description": "Handoff note for the reviewer and the next round."},
			},
			Required: []string{"note"},
		},
	}

	if t.Exec != nil {
		defs = append(defs, provider.ToolDef{
			Name: ToolRun,
			Description: "Run a shell command in the sandbox, in the repository root. " +
				"There is NO network access and NO credentials in this environment. " +
				"Use it to build, test and inspect.",
			Properties: map[string]any{
				"command": map[string]any{"type": "string", "description": "Shell command to run."},
			},
			Required: []string{"command"},
		})
	}
	if t.Install != nil {
		defs = append(defs, provider.ToolDef{
			Name: ToolInstall,
			Description: "Install the repository's declared dependencies. This is the only operation with network access, " +
				"it runs the repository's own install command, and it holds no credentials. " +
				"Call it after changing a dependency manifest.",
			Properties: map[string]any{},
		})
	}
	return defs
}

// Call dispatches one tool call.
func (t *FileTools) Call(ctx context.Context, call provider.ToolCall) (provider.ToolResult, error) {
	res := func(content string, isErr bool) provider.ToolResult {
		return provider.ToolResult{CallID: call.ID, Content: t.cap(content), IsError: isErr}
	}

	switch call.Name {
	case ToolListFiles:
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return res(fmt.Sprintf("could not parse arguments: %v", err), true), nil
		}
		out, err := t.list(args.Path)
		if err != nil {
			return res(err.Error(), true), nil
		}
		return res(out, false), nil

	case ToolReadFile:
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return res(fmt.Sprintf("could not parse arguments: %v", err), true), nil
		}
		abs, err := t.resolve(args.Path)
		if err != nil {
			return res(err.Error(), true), nil
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return res(fmt.Sprintf("could not read %s: %v", args.Path, err), true), nil
		}
		return res(string(b), false), nil

	case ToolGrep:
		var args struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return res(fmt.Sprintf("could not parse arguments: %v", err), true), nil
		}
		out, err := t.grep(args.Pattern, args.Path)
		if err != nil {
			return res(err.Error(), true), nil
		}
		return res(out, false), nil

	case ToolWriteFile:
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return res(fmt.Sprintf("could not parse arguments: %v", err), true), nil
		}
		if err := t.write(args.Path, args.Content); err != nil {
			return res(err.Error(), true), nil
		}
		return res(fmt.Sprintf("wrote %s (%d bytes)", args.Path, len(args.Content)), false), nil

	case ToolRun:
		if t.Exec == nil {
			return res("the run tool is not available in this session", true), nil
		}
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return res(fmt.Sprintf("could not parse arguments: %v", err), true), nil
		}
		if strings.TrimSpace(args.Command) == "" {
			return res("command is empty", true), nil
		}
		out, ok, err := t.Exec(ctx, args.Command)
		if err != nil {
			// The sandbox itself broke. This is not something the model can act
			// on, so it aborts the round rather than becoming feedback.
			return provider.ToolResult{}, fmt.Errorf("sandbox exec failed: %w", err)
		}
		if out == "" {
			out = "(no output)"
		}
		return res(out, !ok), nil

	case ToolInstall:
		if t.Install == nil {
			return res("this repository declares no dependency install step", true), nil
		}
		out, ok, err := t.Install(ctx)
		if err != nil {
			return provider.ToolResult{}, fmt.Errorf("dependency install failed: %w", err)
		}
		if out == "" {
			out = "(no output)"
		}
		return res(out, !ok), nil

	case ToolFinish:
		var args struct {
			Note string `json:"note"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return res(fmt.Sprintf("could not parse arguments: %v", err), true), nil
		}
		t.finished = true
		t.note = args.Note
		return res("acknowledged", false), nil
	}

	return res(fmt.Sprintf("unknown tool %q", call.Name), true), nil
}

// resolve maps a model-supplied path to an absolute path inside Root.
//
// Two things are refused. Anything that escapes the worktree, which is the same
// guarantee executeTask already enforces on planner-supplied paths with
// filepath.IsLocal — the difference being that here the paths come from a model
// mid-round rather than from a spec checked once. And anything under .git: the
// daemon owns the repository's history, because git is the one operation that
// needs the credential the sandbox is not allowed to hold, and an agent that
// can rewrite .git can rewrite what the daemon is about to push.
func (t *FileTools) resolve(rel string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(rel))
	if clean == "" || clean == "." {
		return t.Root, nil
	}
	if filepath.IsAbs(clean) {
		// Tolerate a model that echoes an absolute path inside the worktree;
		// refuse one that points anywhere else.
		if r, err := filepath.Rel(t.Root, clean); err == nil && filepath.IsLocal(r) {
			clean = r
		} else {
			return "", fmt.Errorf("path %q is outside the repository", rel)
		}
	}
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("path %q escapes the repository", rel)
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git"+string(os.PathSeparator)) {
		return "", fmt.Errorf("the .git directory is managed by Kiwi and cannot be read or written here")
	}
	return filepath.Join(t.Root, clean), nil
}

func (t *FileTools) write(rel, content string) error {
	limit := t.MaxWriteBytes
	if limit <= 0 {
		limit = defaultMaxWriteBytes
	}
	if len(content) > limit {
		return fmt.Errorf("refusing to write %d bytes to %s: the limit is %d", len(content), rel, limit)
	}
	abs, err := t.resolve(rel)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(abs); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("could not create parent directory for %s: %v", rel, err)
		}
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return fmt.Errorf("could not write %s: %v", rel, err)
	}
	return nil
}

func (t *FileTools) list(rel string) (string, error) {
	abs, err := t.resolve(rel)
	if err != nil {
		return "", err
	}
	var out []string
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable corner is not worth failing the listing
		}
		name := d.Name()
		if d.IsDir() {
			if skipDir(name) && p != abs {
				return filepath.SkipDir
			}
			return nil
		}
		r, err := filepath.Rel(t.Root, p)
		if err != nil {
			return nil
		}
		out = append(out, r)
		if len(out) >= maxListEntries {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("could not list %s: %v", rel, err)
	}
	if len(out) == 0 {
		return "(no files)", nil
	}
	sort.Strings(out)
	if len(out) >= maxListEntries {
		out = append(out, fmt.Sprintf("... (truncated at %d entries; list a subdirectory to see more)", maxListEntries))
	}
	return strings.Join(out, "\n"), nil
}

func (t *FileTools) grep(pattern, rel string) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("pattern is empty")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regular expression %q: %v", pattern, err)
	}
	root, err := t.resolve(rel)
	if err != nil {
		return "", err
	}

	var out []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 2*1024*1024 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || isBinary(b) {
			return nil
		}
		relPath, err := filepath.Rel(t.Root, p)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if !re.MatchString(line) {
				continue
			}
			out = append(out, fmt.Sprintf("%s:%d:%s", relPath, i+1, strings.TrimRight(line, "\r")))
			if len(out) >= maxGrepMatches {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("search failed: %v", walkErr)
	}
	if len(out) == 0 {
		return "(no matches)", nil
	}
	if len(out) >= maxGrepMatches {
		out = append(out, fmt.Sprintf("... (truncated at %d matches; narrow the pattern or the path)", maxGrepMatches))
	}
	return strings.Join(out, "\n"), nil
}

// cap truncates a tool result, keeping the tail. The end of a failing build is
// the part that says why — the same reasoning as loop.tailOf.
func (t *FileTools) cap(s string) string {
	limit := t.MaxOutputBytes
	if limit <= 0 {
		limit = defaultMaxOutputBytes
	}
	if len(s) <= limit {
		return s
	}
	cut := s[len(s)-limit:]
	for len(cut) > 0 && cut[0]&0xC0 == 0x80 {
		cut = cut[1:]
	}
	return "... (truncated; showing the last " + fmt.Sprint(limit) + " bytes)\n" + cut
}

// skipDir reports directories no repository search should descend into. .git is
// excluded for the reason resolve gives; the rest are build output and vendored
// dependencies, which drown a listing and teach the model nothing.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "target", "dist", "build", ".next", ".venv", "__pycache__", ".mypy_cache", ".pytest_cache":
		return true
	}
	return false
}

func isBinary(b []byte) bool {
	n := len(b)
	if n > 512 {
		n = 512
	}
	for i := 0; i < n; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}
