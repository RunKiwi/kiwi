// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

var (
	ErrPRNotMerged          = errors.New("pull request is not merged")
	ErrMonitorAlreadyExists = errors.New("a monitor already exists for this merge commit")
)

// createExternalMonitor is Phase 1a's createPostMergeMonitor's counterpart
// for a PR Kiwi didn't author: same resulting row shape, reached from a
// dashboard request or a PR comment instead of a merge webhook delivery.
// Requires the PR to already be merged — an open PR returns ErrPRNotMerged
// rather than tracking a pending "watch until merged" state, which is out
// of scope for this plan.
//
// api takes an explicit base URL rather than reading githubAPIDefault
// internally, following the same test-seam convention resolveRevertedSHA
// (github_webhook.go) already established for this exact class of problem:
// only the production caller hardcodes the real constant.
func (s *Server) createExternalMonitor(ctx context.Context, orgID, owner, repo string, number int, api string) (*store.PostMergeMonitor, error) {
	token, ok := s.installationToken(ctx, orgID)
	if !ok {
		return nil, fmt.Errorf("no GitHub installation token available for org %s", orgID)
	}

	sha, title, merged, err := getPullRequest(ctx, api, token, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("resolve %s/%s#%d: %w", owner, repo, number, err)
	}
	if !merged {
		return nil, ErrPRNotMerged
	}

	if existing, err := s.storage.GetMonitorByMergeCommit(ctx, orgID, sha); err == nil && existing != nil {
		return nil, ErrMonitorAlreadyExists
	}

	now := time.Now()
	mon := &store.PostMergeMonitor{
		ID:             "mon_" + uuid.New().String(),
		OrgID:          orgID,
		JobID:          "",
		Origin:         store.MonitorOriginExternalPR,
		Repo:           owner + "/" + repo,
		PRNumber:       number,
		MergeCommitSHA: sha,
		Status:         store.MonitorStatusMonitoring,
		DeployedAt:     now,
		WindowEndsAt:   now.Add(24 * time.Hour),
	}
	if err := s.storage.CreateMonitor(ctx, mon); err != nil {
		return nil, fmt.Errorf("create monitor: %w", err)
	}
	// An external_pr monitor has no Job row to recover a task description
	// from (postMergeMonitorIntent's primary source), so the PR title —
	// its own fallback when that lookup fails — is the only intent signal
	// available here. enqueueTelemetryPolls is a no-op when the org has no
	// telemetry_metrics configured for this repo, matching Phase 1a's own
	// unconditional call site (github_webhook.go).
	s.enqueueTelemetryPolls(ctx, mon, title)
	return mon, nil
}
