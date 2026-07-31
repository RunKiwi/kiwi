package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// passWhenExistsAndContains is passWhenContains for a target that may not exist
// yet: an absent file is a failing test, not an infrastructure error. A real
// test command behaves the same way — `npm test` reports a missing component as
// a test failure, it does not fail to run.
func passWhenExistsAndContains(path, marker string) TestFunc {
	return func(ctx context.Context) (string, bool, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return "FAIL: " + path + " does not exist", false, nil
			}
			return "", false, err
		}
		if strings.Contains(string(b), marker) {
			return "ok", true, nil
		}
		return "FAIL: want " + marker + ", have: " + string(b), false, nil
	}
}

// "Add a cookie consent popup" plans a component that does not exist yet. The
// loop used to die at step 1 on os.ReadFile with "no such file or directory",
// before the Actor was asked anything — so every additive task failed, however
// well the model would have done. A missing target is a file to create.
func TestLoop_CreatesTargetThatDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CookieConsent.tsx")

	prov := &scriptedProvider{edits: []string{"export const CookieConsent = () => null; // DONE"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 2}}

	res, err := r.Run(context.Background(), Task{
		Description:  "add a cookie consent popup",
		FilePath:     path,
		WorktreeRoot: dir,
	}, passWhenExistsAndContains(path, "DONE"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("target was not created: %v", err)
	}
	if !strings.Contains(string(b), "DONE") {
		t.Errorf("target content: got %q", string(b))
	}
}

// The path a real plan produces is nested — "src/components/CookieConsent.tsx" —
// and os.WriteFile does not create parent directories. Without MkdirAll the
// creation fails one step later with the same error the read used to give.
func TestLoop_CreatesMissingParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "components", "CookieConsent.tsx")

	prov := &scriptedProvider{edits: []string{"// DONE"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 2}}

	res, err := r.Run(context.Background(), Task{
		Description:  "add a cookie consent popup",
		FilePath:     path,
		WorktreeRoot: dir,
	}, passWhenExistsAndContains(path, "DONE"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nested target was not created: %v", err)
	}
}

// Only a missing file means "create it". A path that cannot be read for any
// other reason — here a directory sitting where the target should be — is a
// genuine fault and must still stop the loop rather than be silently treated as
// an empty file and overwritten.
func TestLoop_UnreadableTargetIsStillFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{edits: []string{"SHOULD NOT BE CALLED"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 2}}

	// The test never passes, so the loop must reach the read and fail there.
	_, err := r.Run(context.Background(), Task{
		Description:  "x",
		FilePath:     path,
		WorktreeRoot: dir,
	}, func(ctx context.Context) (string, bool, error) { return "fail", false, nil })
	if err == nil {
		t.Fatal("expected an error when the target cannot be read")
	}
	if !strings.Contains(err.Error(), "read target file") {
		t.Errorf("expected a read-target error, got %v", err)
	}
	if prov.calls != 0 {
		t.Errorf("Actor should not have been called, got %d call(s)", prov.calls)
	}
}

// Multi-file mode dropped missing targets from validFiles, which doubles as the
// write allowlist — so the Actor could propose the new file and the result was
// silently discarded. A file that does not exist yet has to be offered to the
// model AND be writable.
func TestLoop_MultiFileCreatesMissingTarget(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "layout.tsx")
	if err := os.WriteFile(existing, []byte("<html/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "src", "CookieConsent.tsx")

	prov := &scriptedProvider{edits: []string{
		`{"files":[{"path":"src/CookieConsent.tsx","content":"// DONE"}]}`,
	}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 2}}

	res, err := r.Run(context.Background(), Task{
		Description:  "add a cookie consent popup",
		FilePath:     existing,
		Files:        []string{existing, missing},
		WorktreeRoot: dir,
	}, passWhenExistsAndContains(missing, "DONE"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if _, err := os.Stat(missing); err != nil {
		t.Fatalf("missing multi-file target was not created: %v", err)
	}
}
