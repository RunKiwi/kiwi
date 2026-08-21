// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// The three GitHub calls a comment trigger needs: is this person allowed to
// spend the org's money, tell them we heard them, and tell them what happened.
//
// Each takes its base URL and token as parameters rather than reading them
// from the server, so they can be exercised against an httptest server without
// a GitHub App, a database or a signed request — which is the only way the
// permission rule gets tested at all.

const githubAPIDefault = "https://api.github.com"

var githubCallClient = &http.Client{Timeout: 15 * time.Second}

func githubRequest(ctx context.Context, method, url, token string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return githubCallClient.Do(req)
}

// collaboratorPermission reports what a user may do in a repository:
// admin, maintain, write, triage or read.
//
// An error is returned for every failure rather than an empty permission,
// because "" would be indistinguishable from a refusal. A GitHub blip would
// then quietly stop a team's reviews working, with nothing in the logs saying
// why, and quiet is the one thing this must never be.
func collaboratorPermission(ctx context.Context, api, token, owner, repo, login string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/collaborators/%s/permission", api, owner, repo, login)
	resp, err := githubRequest(ctx, http.MethodGet, url, token, nil)
	if err != nil {
		return "", fmt.Errorf("collaborator permission for %s: %w", login, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return "", fmt.Errorf("collaborator permission for %s returned %d: %s", login, resp.StatusCode, string(body))
	}

	var out struct {
		Permission string `json:"permission"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode collaborator permission: %w", err)
	}
	return out.Permission, nil
}

// addReaction acknowledges a comment the moment it is accepted, so a reviewer
// knows within seconds rather than whenever the round finishes.
//
// The endpoint differs by comment kind, and getting it wrong reacts to a
// different comment entirely: issue comments and review comments have separate
// id spaces. A review body has no reaction endpoint at all, so it is skipped
// rather than posted to a comment id that is really a review id.
func addReaction(ctx context.Context, api, token, owner, repo, event string, commentID int64, content string) error {
	var url string
	switch event {
	case "issue_comment":
		url = fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d/reactions", api, owner, repo, commentID)
	case "pull_request_review_comment":
		url = fmt.Sprintf("%s/repos/%s/%s/pulls/comments/%d/reactions", api, owner, repo, commentID)
	default:
		return nil
	}

	resp, err := githubRequest(ctx, http.MethodPost, url, token, map[string]string{"content": content})
	if err != nil {
		return fmt.Errorf("react to comment %d: %w", commentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return fmt.Errorf("react to comment %d returned %d: %s", commentID, resp.StatusCode, string(body))
	}
	return nil
}

// createIssueComment posts into the pull request's conversation. Pull requests
// are issues as far as this endpoint is concerned, which is why the path says
// "issues" for something that is not one.
func createIssueComment(ctx context.Context, api, token, owner, repo string, number int, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", api, owner, repo, number)
	resp, err := githubRequest(ctx, http.MethodPost, url, token, map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("comment on %s/%s#%d: %w", owner, repo, number, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return fmt.Errorf("comment on %s/%s#%d returned %d: %s", owner, repo, number, resp.StatusCode, string(b))
	}
	return nil
}

// getPullRequest resolves a pull request to its merge commit SHA, title, and
// whether it was actually merged. The SHA lookup is needed for GitHub's own
// Revert-button PR body ("Reverts owner/repo#N"), which names the reverted
// PR by number rather than by SHA — unlike a manually authored `git revert`
// commit message, which embeds the SHA directly and needs no lookup. The
// title is the fallback intent signal for an external_pr monitor, which by
// construction has no Job row to recover a task description from (see
// postMergeMonitorIntent, github_webhook.go).
func getPullRequest(ctx context.Context, api, token, owner, repo string, number int) (mergeCommitSHA, title string, merged bool, err error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", api, owner, repo, number)
	resp, err := githubRequest(ctx, http.MethodGet, url, token, nil)
	if err != nil {
		return "", "", false, fmt.Errorf("get pull request %s/%s#%d: %w", owner, repo, number, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return "", "", false, fmt.Errorf("get pull request %s/%s#%d returned %d: %s", owner, repo, number, resp.StatusCode, string(b))
	}

	var out struct {
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		Title          string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", false, fmt.Errorf("decode pull request %s/%s#%d: %w", owner, repo, number, err)
	}
	return out.MergeCommitSHA, out.Title, out.Merged, nil
}

// createIssue files a new issue and returns its html_url. Used only by the
// Slack investigation-only completion path (ee/orchestrator/slack_completion.go),
// and only when the triggering instruction explicitly asked for one.
func createIssue(ctx context.Context, api, token, owner, repo, title, body string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues", api, owner, repo)
	resp, err := githubRequest(ctx, http.MethodPost, url, token, map[string]string{"title": title, "body": body})
	if err != nil {
		return "", fmt.Errorf("create issue on %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("create issue on %s/%s returned %d: %s", owner, repo, resp.StatusCode, string(respBody))
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("decode created issue: %w", err)
	}
	return out.HTMLURL, nil
}
