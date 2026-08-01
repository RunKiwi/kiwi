package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

type githubClient interface {
	CreatePR(ctx context.Context, owner, repo, base, head, title, body string) (htmlURL string, err error)
	FindOpenPR(ctx context.Context, owner, repo, head string) (htmlURL string, err error)
}

// jobBranchName is the single branch a whole job shares — every worker commits
// to it in dependency order so the job yields one branch and one PR (#126).
// errNoChanges reports that the loop finished without modifying anything, so
// there is no commit to make and no pull request to open.
//
// This is not a successful outcome. A user submits a task expecting a change;
// producing none, and reporting it green, is worse than failing — it teaches
// them that a green tick means nothing. It happens when the verification
// command already passes on unmodified code, which is the normal case for
// additive work: "add an example" does not make `go build` start failing, so
// the loop finds the repository already satisfying its definition of done and
// correctly concludes there is nothing to do.
var errNoChanges = errors.New("no changes")

// committedSince reports whether HEAD has moved past baseSHA — that is,
// whether someone already committed the work this delivery is meant to carry.
// An empty baseSHA means the caller has no starting point to compare against,
// so nothing can be claimed and the answer is no.
func committedSince(runGit func(args ...string) (string, error), baseSHA string) bool {
	if strings.TrimSpace(baseSHA) == "" {
		return false
	}
	head, err := runGit("rev-parse", "HEAD")
	if err != nil {
		return false
	}
	return strings.TrimSpace(head) != strings.TrimSpace(baseSHA)
}

func jobBranchName(spec agent.WorkerSpec) string {
	jobID := spec.JobID
	if jobID == "" {
		jobID = spec.ID
	}
	return "kiwi/" + jobID
}

type restGitHub struct {
	token string
	api   string // default: "https://api.github.com"
}

func (c *restGitHub) CreatePR(ctx context.Context, owner, repo, base, head, title, body string) (string, error) {
	api := c.api
	if api == "" {
		api = "https://api.github.com"
	}
	u := fmt.Sprintf("%s/repos/%s/%s/pulls", api, owner, repo)

	payload := map[string]string{
		"title": title,
		"head":  head,
		"base":  base,
		"body":  body,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	// One job = one PR (#126): every successful worker of a job pushes to the
	// same branch and calls CreatePR. GitHub returns 422 when a PR already exists
	// for that head, so treat that as success and return the existing PR instead
	// of failing the later workers.
	if resp.StatusCode == http.StatusUnprocessableEntity {
		if existing, e := c.FindOpenPR(ctx, owner, repo, head); e == nil && existing != "" {
			return existing, nil
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Include what GitHub actually said. A bare status number sent us
		// guessing: 422 alone covers a missing base branch, a missing head
		// branch, an existing PR and an empty diff, which need different fixes.
		// The body names the field every time, and it is the only place that
		// does — the user sees this string as the whole explanation of why
		// their task produced no pull request.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return "", fmt.Errorf("github api returned status %d creating %s->%s in %s/%s: %s",
			resp.StatusCode, head, base, owner, repo, describeGitHubError(body))
	}

	var res struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return res.HTMLURL, nil
}

// GitHub's limits on the pull request endpoint. The title cap is the one that
// bit us; the body cap is documented as 65536 and left with headroom.
const (
	maxPRTitleLen = 256
	maxPRBodyLen  = 60000
)

// prTitleAndBody turns a task description into a pull request title and body.
//
// The title used to be "Kiwi: " + the whole task, which failed in production:
//
//	title is too long (maximum is 256 characters)
//
// A planner-authored task is not a title. It is a numbered instruction block —
// the one that broke here ran to 1370 characters across 13 lines — so using it
// whole was always going to hit the cap on any task the planner expanded, which
// is every task that is not a one-liner. The task then completed the entire
// loop, pushed a real branch, and reported failure with no pull request.
//
// The first line is the summary the planner wrote, so that becomes the title,
// and the full description moves into the body where it is actually useful —
// the body was the fixed string "Generated by Kiwi worker.", which told a
// reviewer nothing about what the change was meant to do.
func prTitleAndBody(task string) (title, body string) {
	// The first non-blank line, not the first line: a description that opens
	// with a newline would otherwise be titled by an empty string.
	var first string
	for _, line := range strings.Split(task, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			first = s
			break
		}
	}
	if first == "" {
		first = "apply the requested change"
	}

	title = truncateRunes("Kiwi: "+first, maxPRTitleLen)

	body = "Generated by Kiwi worker.\n\n## Task\n\n" + strings.TrimSpace(task) + "\n"
	body = truncateRunes(body, maxPRBodyLen)
	return title, body
}

// truncateRunes shortens s to at most max characters, marking that it was cut.
// It counts runes rather than bytes so a multi-byte character is never split
// into invalid UTF-8, and so a limit GitHub states in characters is applied in
// characters.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	const ellipsis = "..."
	if max <= len(ellipsis) {
		return string(r[:max])
	}
	return string(r[:max-len(ellipsis)]) + ellipsis
}

// describeGitHubError renders a GitHub API error body as one readable line.
//
// The shape is {"message":"Validation Failed","errors":[{...}]}, where each
// entry carries either a free-text `message` ("A pull request already exists
// for o:b.", "No commits between main and main") or a `field`/`code` pair
// ("base"/"invalid" when the base branch does not exist). Both forms matter, so
// both are rendered. A body that is not JSON at all is passed through
// truncated rather than swallowed.
func describeGitHubError(body []byte) string {
	var parsed struct {
		Message string `json:"message"`
		Errors  []struct {
			Resource string `json:"resource"`
			Field    string `json:"field"`
			Code     string `json:"code"`
			Message  string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Message == "" {
		s := strings.TrimSpace(string(body))
		if s == "" {
			return "(empty response body)"
		}
		if len(s) > 500 {
			s = s[:500] + "..."
		}
		return s
	}

	parts := make([]string, 0, len(parsed.Errors))
	for _, e := range parsed.Errors {
		switch {
		case e.Message != "":
			parts = append(parts, e.Message)
		case e.Field != "":
			parts = append(parts, fmt.Sprintf("%s is %s", e.Field, orUnknown(e.Code)))
		case e.Code != "":
			parts = append(parts, e.Code)
		}
	}
	if len(parts) == 0 {
		return parsed.Message
	}
	return parsed.Message + ": " + strings.Join(parts, "; ")
}

func orUnknown(code string) string {
	if code == "" {
		return "invalid"
	}
	return code
}

// FindOpenPR returns the html_url of the open PR whose head is `head` in
// owner/repo, or "" if none. Used to make CreatePR idempotent per job branch.
func (c *restGitHub) FindOpenPR(ctx context.Context, owner, repo, head string) (string, error) {
	api := c.api
	if api == "" {
		api = "https://api.github.com"
	}
	u := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&head=%s:%s", api, owner, repo, owner, head)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("list pulls returned status %d", resp.StatusCode)
	}
	var prs []struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return "", err
	}
	if len(prs) == 0 {
		return "", nil
	}
	return prs[0].HTMLURL, nil
}

// parseGitHubRepo handles https://github.com/OWNER/REPO, .../REPO.git,
// and rejects non-GitHub hosts.
func parseGitHubRepo(repoURL string) (owner, repo string, ok bool) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", false
	}
	if u.Host != "github.com" && u.Host != "www.github.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	return owner, repo, true
}

// publishResult runs git add/commit/push and opens a PR.
// publishResult delivers work that is sitting uncommitted in the worktree —
// the single-file loop's shape, where the loop edits files and never touches
// git.
func publishResult(ctx context.Context, worktreePath string, spec agent.WorkerSpec, gitToken string, gh githubClient, pushRemoteOverride string) (prURL string, detail string, err error) {
	return publishResultFrom(ctx, worktreePath, spec, gitToken, gh, pushRemoteOverride, "")
}

// publishResultFrom is publishResult with a known starting point.
//
// A session commits at the end of every round, so by delivery time the worktree
// is clean and the work is already in the branch's history. That is
// indistinguishable, to `git status`, from a run that did nothing — and
// reporting errNoChanges there would throw away a finished task. baseSHA is
// what tells the two apart: a clean tree whose HEAD has moved past the point
// the session started from has work to deliver. An empty baseSHA keeps the
// original behaviour exactly, which is what the single-file path passes.
func publishResultFrom(ctx context.Context, worktreePath string, spec agent.WorkerSpec, gitToken string, gh githubClient, pushRemoteOverride, baseSHA string) (prURL string, detail string, err error) {
	runGit := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = worktreePath
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w\n%s", args[0], err, out)
		}
		return string(out), nil
	}

	if _, err := runGit("add", "-A"); err != nil {
		return "", "", err
	}
	statusOut, err := runGit("status", "--porcelain")
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(statusOut) == "" {
		// A clean tree means one of two things, and only baseSHA distinguishes
		// them: nothing was produced, or the producer already committed.
		if !committedSince(runGit, baseSHA) {
			// Nothing was produced. Returned as an error rather than a benign
			// detail string so the caller cannot mistake it for a delivered
			// result — which is exactly what happened: the task reported
			// SUCCEEDED with no pull request and the word "no changes" as its
			// only explanation.
			return "", "", errNoChanges
		}
	} else if _, err := runGit("-c", "user.email=bot@runkiwi.dev", "-c", "user.name=Kiwi", "commit", "-m", "kiwi: "+spec.Task); err != nil {
		return "", "", err
	}

	branchName := jobBranchName(spec)

	pushRemote := spec.RepoURL
	if pushRemoteOverride != "" {
		pushRemote = pushRemoteOverride
	} else {
		u, err := url.Parse(spec.RepoURL)
		if err != nil {
			return "", "", err
		}
		if u.Scheme == "https" && u.Host == "github.com" {
			u.User = url.UserPassword("x-access-token", gitToken)
			pushRemote = u.String()
		}
	}

	// Never log pushRemote as it contains the token. Log spec.RepoURL instead.
	log.Printf("Pushing to %s on branch %s...", spec.RepoURL, branchName)

	// Use + (force push) because a redelivered task will have a different commit hash
	// than the previous attempt, resulting in a non-fast-forward push.
	if _, err := runGit("push", pushRemote, "+HEAD:refs/heads/"+branchName); err != nil {
		// git may echo the authenticated remote URL (with the token) in its error
		// output; scrub the token before it reaches logs or the result detail.
		msg := err.Error()
		if gitToken != "" {
			msg = strings.ReplaceAll(msg, gitToken, "***")
		}
		return "", "", fmt.Errorf("push failed: %s", msg)
	}

	owner, repo, isGH := parseGitHubRepo(spec.RepoURL)
	if !isGH {
		// Tests might pass a local repo for pushRemoteOverride which parseGitHubRepo will reject.
		// Return unsupported host instead of erroring.
		return "", "unsupported host", nil
	}

	// If this task was redelivered after previously opening a PR, adopt the existing PR.
	if existing, err := gh.FindOpenPR(ctx, owner, repo, branchName); err == nil && existing != "" {
		return existing, "updated existing PR", nil
	}

	base := spec.Ref
	if base == "" {
		base = "main"
	}
	title, body := prTitleAndBody(spec.Task)

	pr, err := gh.CreatePR(ctx, owner, repo, base, branchName, title, body)
	if err != nil {
		return "", "", fmt.Errorf("create PR: %w", err)
	}

	return pr, "created PR", nil
}
