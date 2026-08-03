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
	ToolEditFile  = "edit_file"
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
	// read records the paths read this round, so an edit can require that the
	// model has actually seen the file it is changing. Editing from memory is
	// how a confident wrong `old_string` turns into a silent no-op or, worse, a
	// match in the wrong place.
	read map[string]bool
}

const (
	defaultMaxOutputBytes = 64 * 1024
	defaultMaxWriteBytes  = 1024 * 1024
	maxListEntries        = 2000
	maxGrepMatches        = 200
	// defaultReadLines bounds a read in LINES rather than bytes. A line budget
	// is what the model reasons in, and it makes the truncation notice
	// meaningful: "lines 1-2000 of 5431" tells it what to ask for next, where
	// "the last 64KB" tells it nothing it can act on.
	defaultReadLines = 2000
	// maxGrepContext bounds the -C window, so a wide context on a common
	// pattern cannot return the repository.
	maxGrepContext = 20
)

// Finished reports whether the Implementer called finish, and its handoff note.
func (t *FileTools) Finished() (bool, string) { return t.finished, t.note }

// Reset clears the per-round finish state so one FileTools can serve several
// rounds — the worktree is the same, only the conversation is new.
//
// The read set is cleared with it, deliberately. A round starts a fresh
// conversation, so the model has not seen anything a previous round read; the
// edit precondition tracks what THIS conversation knows, not what the process
// happens to have opened at some point.
func (t *FileTools) Reset() {
	t.finished = false
	t.note = ""
	t.read = nil
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
			Name: ToolReadFile,
			Description: "Read a file from the repository. Output lines are prefixed with their line number and a tab; " +
				"strip that prefix before using the text anywhere else. " +
				"Large files are truncated from the end — read further with offset.",
			Properties: map[string]any{
				"path":   map[string]any{"type": "string", "description": "File path relative to the repository root."},
				"offset": map[string]any{"type": "integer", "description": "1-based line to start at. Defaults to the first line."},
				"limit":  map[string]any{"type": "integer", "description": "How many lines to read. Defaults to 2000."},
			},
			Required: []string{"path"},
		},
		{
			Name:        ToolGrep,
			Description: "Search the repository for a Go regular expression. Returns matching lines with their file and line number.",
			Properties: map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Go regular expression."},
				"path":    map[string]any{"type": "string", "description": "Optional directory to restrict the search to."},
				"glob":    map[string]any{"type": "string", "description": "Optional filename pattern to restrict the search to, e.g. \"*.go\"."},
				"context": map[string]any{"type": "integer", "description": "Lines of context to show around each match (max 20). Defaults to 0."},
			},
			Required: []string{"pattern"},
		},
		{
			Name: ToolEditFile,
			Description: "Replace an exact string in an existing file. Prefer this over write_file for any change to a file that already exists — " +
				"it is faster and leaves a smaller diff. " +
				"old_string must match the file byte for byte, including indentation, and must appear exactly once: " +
				"include the surrounding lines needed to make it unique. " +
				"Strip the line-number prefix from read_file output before matching.",
			Properties: map[string]any{
				"path":        map[string]any{"type": "string", "description": "File path relative to the repository root."},
				"old_string":  map[string]any{"type": "string", "description": "The exact text to replace."},
				"new_string":  map[string]any{"type": "string", "description": "The text to replace it with."},
				"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence instead of requiring exactly one. Defaults to false."},
			},
			Required: []string{"path", "old_string", "new_string"},
		},
		{
			Name: ToolWriteFile,
			Description: "Create a new file, or completely replace a short one. " +
				"Supply the file's complete new contents. " +
				"To change part of an existing file, use edit_file instead — rewriting a large file wholesale is slow and hides the real change.",
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
			Path   string `json:"path"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
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
			return res(fmt.Sprintf("could not read %s: %v — check the path with list_files", args.Path, err), true), nil
		}
		// Marked read BEFORE the window is applied: the model has now seen this
		// file, which is what edit_file's precondition is about. Requiring it to
		// have seen the specific lines it edits would be stricter than useful,
		// since grep is a legitimate way to find them.
		t.markRead(abs)
		// Reads are windowed and line-numbered here, and deliberately NOT passed
		// through t.cap: cap keeps the TAIL, which is right for a failing build
		// (the error is at the bottom) and wrong for source, where the imports
		// and declarations are at the top. A model that got the bottom of a file
		// and was then asked for its "complete new contents" reconstructed the
		// missing top from memory.
		return res(renderFileWindow(string(b), args.Offset, args.Limit), false), nil

	case ToolEditFile:
		var args struct {
			Path       string `json:"path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return res(fmt.Sprintf("could not parse arguments: %v", err), true), nil
		}
		out, err := t.edit(args.Path, args.OldString, args.NewString, args.ReplaceAll)
		if err != nil {
			return res(err.Error(), true), nil
		}
		return res(out, false), nil

	case ToolGrep:
		var args struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
			Glob    string `json:"glob"`
			Context int    `json:"context"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return res(fmt.Sprintf("could not parse arguments: %v", err), true), nil
		}
		out, err := t.grep(args.Pattern, args.Path, args.Glob, args.Context)
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
			return res("the run tool is not available in this session; inspect files with read_file and grep instead", true), nil
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

	names := make([]string, 0, len(t.Defs()))
	for _, d := range t.Defs() {
		names = append(names, d.Name)
	}
	return res(fmt.Sprintf("unknown tool %q; available tools are: %s", call.Name, strings.Join(names, ", ")), true), nil
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

// markRead records that a path has been read this round.
func (t *FileTools) markRead(abs string) {
	if t.read == nil {
		t.read = map[string]bool{}
	}
	t.read[abs] = true
}

// edit replaces an exact string in an existing file.
//
// Every refusal here is a deliberate choice to fail loudly rather than guess,
// because each of the quiet alternatives produces a plausible-looking edit that
// is wrong somewhere the test command may never look:
//
//   - Not found: the model is working from a stale or imagined copy. Editing
//     nothing and reporting success would let the round continue believing the
//     change landed.
//   - Ambiguous: replacing "the first one" is a coin flip on which call site,
//     loop body or struct field gets rewritten. The model can disambiguate by
//     including more context; it cannot recover from a silent wrong pick.
//   - Not read this round: an edit composed from memory rather than from the
//     file in front of it.
//
// The error strings say what to do next, not merely what went wrong. A tool
// result is the only channel the model has, so an unhelpful one costs a whole
// turn.
func (t *FileTools) edit(rel, oldStr, newStr string, replaceAll bool) (string, error) {
	if oldStr == "" {
		return "", fmt.Errorf("old_string is empty; to create a file or replace it entirely, use %s", ToolWriteFile)
	}
	if oldStr == newStr {
		return "", fmt.Errorf("old_string and new_string are identical, so this edit would change nothing")
	}
	abs, err := t.resolve(rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("could not read %s: %v — to create a new file, use %s", rel, err, ToolWriteFile)
	}
	if !t.read[abs] {
		return "", fmt.Errorf("read %s before editing it, so the edit is based on its current contents", rel)
	}

	content := string(b)
	n := strings.Count(content, oldStr)
	switch {
	case n == 0:
		return "", fmt.Errorf("old_string was not found in %s. It must match byte for byte, including indentation — "+
			"re-read the file and copy the exact text", rel)
	case n > 1 && !replaceAll:
		return "", fmt.Errorf("old_string appears %d times in %s; it must be unique. "+
			"Include the surrounding lines that make it unique, or set replace_all to change every occurrence", n, rel)
	}

	updated := content
	if replaceAll {
		updated = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		updated = strings.Replace(content, oldStr, newStr, 1)
	}

	limit := t.MaxWriteBytes
	if limit <= 0 {
		limit = defaultMaxWriteBytes
	}
	if len(updated) > limit {
		return "", fmt.Errorf("that edit would make %s %d bytes, over the %d-byte limit", rel, len(updated), limit)
	}
	if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("could not write %s: %v", rel, err)
	}

	if replaceAll && n > 1 {
		return fmt.Sprintf("edited %s (%d occurrences replaced)", rel, n), nil
	}
	return fmt.Sprintf("edited %s", rel), nil
}

// renderFileWindow numbers a file's lines and returns the requested window.
//
// Line numbers are what make the rest of the interface addressable: grep
// reports them, an error can name one, and the model can ask for a range around
// one. The tool description tells the model to strip the prefix before matching
// with edit_file, which is the one hazard the numbering introduces.
func renderFileWindow(content string, offset, limit int) string {
	if content == "" {
		return "(empty file)"
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	total := len(lines)

	if offset < 1 {
		offset = 1
	}
	if offset > total {
		return fmt.Sprintf("(offset %d is past the end of the file, which has %d lines)", offset, total)
	}
	if limit <= 0 {
		limit = defaultReadLines
	}
	end := offset + limit - 1
	if end > total {
		end = total
	}

	var b strings.Builder
	for i := offset; i <= end; i++ {
		fmt.Fprintf(&b, "%d\t%s\n", i, lines[i-1])
	}
	// Stated rather than implied. A truncation the model cannot see is how a
	// partial read becomes a whole-file rewrite that invents the missing part.
	if end < total {
		fmt.Fprintf(&b, "\n... (showing lines %d-%d of %d; read further with offset=%d)\n", offset, end, total, end+1)
	}
	return b.String()
}

func (t *FileTools) write(rel, content string) error {
	limit := t.MaxWriteBytes
	if limit <= 0 {
		limit = defaultMaxWriteBytes
	}
	if len(content) > limit {
		return fmt.Errorf("refusing to write %d bytes to %s: the limit is %d. "+
			"If you are changing part of an existing file, use %s instead of rewriting it whole", len(content), rel, limit, ToolEditFile)
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

// grep searches the repository, optionally restricted by directory and filename
// pattern, and optionally with surrounding lines.
//
// Context is the reason this grew arguments. A bare `path:line:text` match is
// rarely enough to act on, so every search used to be followed by a read_file
// of the whole surrounding file — two round-trips and a large result to learn
// what three lines would have said.
func (t *FileTools) grep(pattern, rel, glob string, ctxLines int) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("pattern is empty")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regular expression %q: %v — this is Go regexp syntax, "+
			"so a literal search needs regexp.QuoteMeta-style escaping", pattern, err)
	}
	if glob != "" {
		// Reject a bad pattern up front rather than silently matching nothing:
		// a search that returns "(no matches)" because the glob was malformed is
		// indistinguishable from one that genuinely found nothing.
		if _, err := filepath.Match(glob, "probe"); err != nil {
			return "", fmt.Errorf("invalid glob %q: %v", glob, err)
		}
	}
	if ctxLines < 0 {
		ctxLines = 0
	}
	if ctxLines > maxGrepContext {
		ctxLines = maxGrepContext
	}
	root, err := t.resolve(rel)
	if err != nil {
		return "", err
	}

	var out []string
	truncated := false
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
		if glob != "" {
			if ok, _ := filepath.Match(glob, d.Name()); !ok {
				return nil
			}
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
		lines := strings.Split(string(b), "\n")
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			if ctxLines == 0 {
				out = append(out, fmt.Sprintf("%s:%d:%s", relPath, i+1, strings.TrimRight(line, "\r")))
			} else {
				// A separator between hunks, so two matches twenty lines apart
				// do not read as one contiguous block.
				if len(out) > 0 {
					out = append(out, "--")
				}
				lo := max(0, i-ctxLines)
				hi := min(len(lines)-1, i+ctxLines)
				for j := lo; j <= hi; j++ {
					// ':' on the matching line, '-' on context — the convention
					// grep itself uses, so the match stays findable by eye.
					sep := "-"
					if j == i {
						sep = ":"
					}
					out = append(out, fmt.Sprintf("%s%s%d%s%s", relPath, sep, j+1, sep, strings.TrimRight(lines[j], "\r")))
				}
			}
			if len(out) >= maxGrepMatches {
				truncated = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("search failed: %v", walkErr)
	}
	if len(out) == 0 {
		if glob != "" {
			return fmt.Sprintf("(no matches for %q in files matching %q)", pattern, glob), nil
		}
		return fmt.Sprintf("(no matches for %q)", pattern), nil
	}
	if truncated {
		out = append(out, fmt.Sprintf("... (truncated at %d lines; narrow the pattern, the path or the glob)", maxGrepMatches))
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
