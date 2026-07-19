package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		return "", fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var res struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return res.HTMLURL, nil
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
func publishResult(ctx context.Context, worktreePath string, spec agent.WorkerSpec, gitToken string, gh githubClient, pushRemoteOverride string) (prURL string, detail string, err error) {
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
		return "", "no changes", nil
	}

	if _, err := runGit("-c", "user.email=bot@runkiwi.com", "-c", "user.name=Kiwi", "commit", "-m", "kiwi: "+spec.Task); err != nil {
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
	title := "Kiwi: " + spec.Task
	body := "Generated by Kiwi worker."

	pr, err := gh.CreatePR(ctx, owner, repo, base, branchName, title, body)
	if err != nil {
		return "", "", fmt.Errorf("create PR: %w", err)
	}

	return pr, "created PR", nil
}
