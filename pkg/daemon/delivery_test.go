package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

func TestParseGitHubRepo(t *testing.T) {
	tests := []struct {
		url   string
		owner string
		repo  string
		ok    bool
	}{
		{"https://github.com/RunKiwi/kiwi", "RunKiwi", "kiwi", true},
		{"https://github.com/RunKiwi/kiwi.git", "RunKiwi", "kiwi", true},
		{"https://github.com/OWNER/REPO/", "OWNER", "REPO", true},
		{"https://gitlab.com/OWNER/REPO", "", "", false},
		{"file:///local/repo", "", "", false},
	}

	for _, tc := range tests {
		o, r, ok := parseGitHubRepo(tc.url)
		if ok != tc.ok {
			t.Errorf("url %q: ok = %v, want %v", tc.url, ok, tc.ok)
		}
		if o != tc.owner || r != tc.repo {
			t.Errorf("url %q: got %s/%s, want %s/%s", tc.url, o, r, tc.owner, tc.repo)
		}
	}
}

func TestRestGitHub_CreatePR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/pulls" {
			t.Errorf("bad request: %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer token123" {
			t.Errorf("bad auth: %q", auth)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["head"] != "kiwi/j1" {
			t.Errorf("bad head: %v", body["head"])
		}

		json.NewEncoder(w).Encode(map[string]string{
			"html_url": "https://github.com/o/r/pull/1",
		})
	}))
	defer srv.Close()

	gh := &restGitHub{token: "token123", api: srv.URL}
	url, err := gh.CreatePR(context.Background(), "o", "r", "main", "kiwi/j1", "title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/o/r/pull/1" {
		t.Errorf("got %q", url)
	}
}

type fakeGH struct {
	called       bool
	findCalled   bool
	owner        string
	repo         string
	existingOpen string
}

func (f *fakeGH) CreatePR(ctx context.Context, owner, repo, base, head, title, body string) (string, error) {
	f.called = true
	f.owner = owner
	f.repo = repo
	return "https://github.com/" + owner + "/" + repo + "/pull/1", nil
}

func (f *fakeGH) FindOpenPR(ctx context.Context, owner, repo, head string) (string, error) {
	f.findCalled = true
	f.owner = owner
	f.repo = repo
	return f.existingOpen, nil
}

func TestPublishResult(t *testing.T) {
	// Create bare remote
	tmp := t.TempDir()
	bareDir := filepath.Join(tmp, "remote.git")
	exec.Command("git", "init", "--bare", bareDir).Run()

	// Create worktree clone
	workDir := filepath.Join(tmp, "work")
	exec.Command("git", "clone", bareDir, workDir).Run()

	// Create an initial commit on main in worktree and push
	os.WriteFile(filepath.Join(workDir, "README"), []byte("initial"), 0644)
	exec.Command("git", "-C", workDir, "add", "-A").Run()
	exec.Command("git", "-C", workDir, "-c", "user.email=b@b.com", "-c", "user.name=B", "commit", "-m", "init").Run()
	exec.Command("git", "-C", workDir, "push", "origin", "HEAD:main").Run()

	spec := agent.WorkerSpec{
		ID:      "task1",
		JobID:   "job1",
		Task:    "test task",
		RepoURL: "https://github.com/owner/repo",
	}

	gh := &fakeGH{}

	// Test 1: no changes
	pr, detail, err := publishResult(context.Background(), workDir, spec, "tok", gh, bareDir)
	if err != nil {
		t.Fatal(err)
	}
	if detail != "no changes" {
		t.Errorf("expected no changes, got %q", detail)
	}

	// Test 2: with changes
	os.WriteFile(filepath.Join(workDir, "test.txt"), []byte("data"), 0644)
	pr, detail, err = publishResult(context.Background(), workDir, spec, "tok", gh, bareDir)
	if err != nil {
		t.Fatal(err)
	}
	if detail != "created PR" {
		t.Errorf("got detail: %q", detail)
	}
	if pr != "https://github.com/owner/repo/pull/1" {
		t.Errorf("got pr url: %q", pr)
	}
	if !gh.called {
		t.Error("gh client not called")
	}

	// Verify branch exists in bare remote
	out, err := exec.Command("git", "--git-dir="+bareDir, "rev-parse", "refs/heads/kiwi/job1").CombinedOutput()
	if err != nil {
		t.Errorf("branch kiwi/job1 not in bare remote: %v %s", err, out)
	}
	
	// Test 3: idempotency (PR already exists)
	gh.called = false
	gh.existingOpen = "https://github.com/owner/repo/pull/123"
	os.WriteFile(filepath.Join(workDir, "test2.txt"), []byte("data2"), 0644)
	pr, detail, err = publishResult(context.Background(), workDir, spec, "tok", gh, bareDir)
	if err != nil {
		t.Fatal(err)
	}
	if detail != "updated existing PR" {
		t.Errorf("got detail: %q", detail)
	}
	if pr != "https://github.com/owner/repo/pull/123" {
		t.Errorf("got pr url: %q", pr)
	}
	if gh.called {
		t.Error("CreatePR was called even though PR exists")
	}
	if !gh.findCalled {
		t.Error("FindOpenPR was not called")
	}
}

// TestPublishResult_PushFailureIsError ensures a failed push surfaces as an
// error (so the daemon reports the task FAILED instead of a false green) and
// does not leak the git token in the error message.
func TestPublishResult_PushFailureIsError(t *testing.T) {
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "work")
	if err := exec.Command("git", "init", workDir).Run(); err != nil {
		t.Fatal(err)
	}
	// An initial commit, then an UNCOMMITTED change so publishResult has
	// something to commit and proceeds to the (failing) push.
	os.WriteFile(filepath.Join(workDir, "f.txt"), []byte("x"), 0644)
	exec.Command("git", "-C", workDir, "add", "-A").Run()
	exec.Command("git", "-C", workDir, "-c", "user.email=b@b.com", "-c", "user.name=B", "commit", "-m", "c").Run()
	os.WriteFile(filepath.Join(workDir, "pending.txt"), []byte("new work"), 0644)

	spec := agent.WorkerSpec{ID: "t1", JobID: "job1", Task: "t", RepoURL: "https://github.com/owner/repo"}
	// Point the push at a non-existent path so it fails deterministically offline.
	badRemote := filepath.Join(tmp, "does-not-exist.git")

	_, _, err := publishResult(context.Background(), workDir, spec, "secretTOKEN", &fakeGH{}, badRemote)
	if err == nil {
		t.Fatal("expected an error when push fails, got nil (would be reported as false SUCCEEDED)")
	}
	if strings.Contains(err.Error(), "secretTOKEN") {
		t.Errorf("push error leaked the git token: %v", err)
	}
}
