// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

var velocityRanges = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

func (s *Server) handleVelocityAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	rangeParam := r.URL.Query().Get("range")
	window, ok := velocityRanges[rangeParam]
	if !ok {
		window = velocityRanges["7d"]
	}
	since := time.Now().Add(-window)

	recs, err := s.storage.ListExecutionRecordsByOrgAndVer(r.Context(), claims.OrgID, ver.SchemaVersion, since)
	if err != nil {
		http.Error(w, "failed to load execution records", http.StatusInternalServerError)
		return
	}

	var zeroShot, selfHealed, humanGuided int
	for _, rec := range recs {
		var body ver.Record
		if err := json.Unmarshal(rec.Body, &body); err != nil {
			continue
		}
		if body.Verification.FinalOutcome != "pass" {
			continue
		}

		humanGuidedThread, herr := s.jobThreadHasHumanContinuation(r.Context(), claims.OrgID, rec.JobID)
		if herr != nil {
			continue
		}
		switch {
		case humanGuidedThread:
			humanGuided++
		default:
			rejections := 0
			for _, worker := range body.Execution.Workers {
				rejections += worker.CriticRejections
			}
			if rejections == 0 {
				zeroShot++
			} else {
				selfHealed++
			}
		}
	}

	total := zeroShot + selfHealed + humanGuided
	pct := func(n int) float64 {
		if total == 0 {
			return 0
		}
		return float64(n) / float64(total) * 100
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"test_pass_metrics": map[string]interface{}{
			"zero_shot_pct":    pct(zeroShot),
			"self_healed_pct":  pct(selfHealed),
			"human_guided_pct": pct(humanGuided),
		},
		"jobs_counted": total,
	})
}

func (s *Server) jobThreadHasHumanContinuation(ctx context.Context, orgID, jobID string) (bool, error) {
	tasks, err := s.storage.GetJobTasks(ctx, orgID, jobID)
	if err != nil {
		return false, err
	}
	seenRoots := map[string]bool{}
	for _, t := range tasks {
		if t.RootTaskID == "" || seenRoots[t.RootTaskID] {
			continue
		}
		seenRoots[t.RootTaskID] = true
		thread, terr := s.storage.ThreadTasks(ctx, orgID, t.RootTaskID)
		if terr != nil {
			return false, terr
		}
		for _, th := range thread {
			if th.Origin == store.OriginPRComment {
				return true, nil
			}
		}
	}
	return false, nil
}
