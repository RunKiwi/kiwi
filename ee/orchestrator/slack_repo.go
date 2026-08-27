// ee/orchestrator/slack_repo.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// inlineOverrideRe matches "repo:owner/name" anywhere in the message —
// the explicit-override syntax, chosen for being unambiguous to both grep
// and a human skimming the message (unlike a bare "owner/name" token, which
// collides with normal English like "click and/or").
var inlineOverrideRe = regexp.MustCompile(`repo:([\w.-]+/[\w.-]+)`)

// inlineRepoOverride extracts an explicit repo:owner/name token, the
// highest-priority repo signal (spec §5, priority 1).
func inlineRepoOverride(text string) (string, bool) {
	m := inlineOverrideRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// inferRepo asks the LLM to pick the most likely target repo from the org's
// GitHub-installed repos, or report ambiguity rather than guess. Confidence
// threshold is deliberately conservative (only "high" auto-picks) — a
// tuning knob per the spec, widen based on real usage.
func inferRepo(ctx context.Context, complete completeFunc, repoNames []string, instruction string) (repo string, ambiguous bool, err error) {
	system := "You pick which repository a task refers to, from a fixed list. " +
		`Respond with ONLY JSON: {"repo": "<name or empty>", "confidence": "high|medium|low", "candidates": ["..."]}. ` +
		`Use "high" only when one repo is clearly the right target.`
	user := fmt.Sprintf("Candidate repositories:\n%s\n\nInstruction: %s", strings.Join(repoNames, "\n"), instruction)
	resp, cerr := complete(ctx, system, user)
	if cerr != nil {
		return "", false, cerr
	}
	start, end := strings.IndexByte(resp, '{'), strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return "", false, fmt.Errorf("no JSON object in repo-inference response")
	}
	var out struct {
		Repo       string `json:"repo"`
		Confidence string `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(resp[start:end+1]), &out); err != nil {
		return "", false, fmt.Errorf("parse repo-inference response: %w", err)
	}
	if out.Confidence != "high" || out.Repo == "" {
		return "", true, nil
	}
	return out.Repo, false, nil
}

// resolveSlackRepo runs the full priority order from the spec: inline
// override, then channel binding, then LLM inference, then disambiguation.
// A non-empty ambiguousReply means the caller must reply with it and NOT
// submit a task.
//
// text and instruction are deliberately different inputs. text is the raw
// current message: inlineRepoOverride's explicit "repo:owner/name" token is
// only ever honored from the message actually being acted on, not from
// something said earlier in the thread. instruction is the context-assembled
// task description built by fetchSlackContext, which — unlike text — folds
// in prior thread history. A user who names the repo in one message ("docs
// repo is runkiwi/docs") and only says "work on this" in the @mention that
// follows has zero repo signal in text alone; inferRepo needs instruction to
// ever see the message that actually answers the question. Passing text here
// instead was a real bug: repo inference silently ignored thread history it
// had already fetched for a completely different purpose.
func (s *Server) resolveSlackRepo(ctx context.Context, orgID, text, instruction string, binding *store.SlackChannelBinding) (repoURL, ambiguousReply string) {
	if override, ok := inlineRepoOverride(text); ok {
		return "https://github.com/" + override, ""
	}
	if binding != nil {
		return binding.RepoURL, ""
	}

	installs, err := s.storage.ListGitHubInstallations(ctx, orgID)
	if err != nil || len(installs) == 0 || s.githubApp == nil {
		return "", "This channel isn't bound to a repository, and this org has no GitHub connection to infer one from — connect GitHub or bind this channel under Integrations."
	}

	var names []string
	nameToURL := map[string]string{}
	for _, inst := range installs {
		repos, err := s.githubApp.ListRepositories(ctx, inst.InstallationID)
		if err != nil {
			continue
		}
		for _, r := range repos {
			names = append(names, r.FullName)
			nameToURL[r.FullName] = r.HTMLURL
		}
	}
	if len(names) == 0 {
		return "", "Couldn't find any repositories to infer from — bind this channel to a repository under Integrations."
	}

	complete, cerr := s.slackCompleter(ctx)
	if cerr != nil {
		return "", "Couldn't determine which repository this is about — bind this channel to a repository under Integrations."
	}
	return pickRepoFromCandidates(ctx, complete, names, nameToURL, instruction)
}

// pickRepoFromCandidates is resolveSlackRepo's I/O-free tail: given repos
// already fetched from the GitHub App and a completer already built, decide.
// Split out so "the completer sees instruction, not the bare mention" is
// testable without a live githubApp client, which s.githubApp — a concrete
// *githubapp.Client, not an interface — otherwise makes impossible to fake.
func pickRepoFromCandidates(ctx context.Context, complete completeFunc, names []string, nameToURL map[string]string, instruction string) (repoURL, ambiguousReply string) {
	picked, ambiguous, err := inferRepo(ctx, complete, names, instruction)
	if err != nil || ambiguous || picked == "" {
		return "", fmt.Sprintf("Not sure which repository this is about — try mentioning it explicitly, e.g. `repo:%s`, or bind this channel under Integrations.", firstOf(names))
	}
	return nameToURL[picked], ""
}
