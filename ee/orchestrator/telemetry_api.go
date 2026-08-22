// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"net/http"

	"github.com/ibreakthecloud/kiwi/ee/auth"
)

// handleSandboxCacheStats serves GET /api/v1/sandbox/cache/stats, aggregating
// the caller's org's daemons' most recent heartbeat cache telemetry. A daemon
// that has never reported (older binary, or hasn't heartbeat since upgrade)
// contributes nothing rather than a fabricated zero.
func (s *Server) handleSandboxCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	daemons, err := s.storage.ListDaemons(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "failed to list daemons", http.StatusInternalServerError)
		return
	}

	var totalRepos, totalActive int
	var hits, misses int64
	var reporting int
	for _, d := range daemons {
		if d.LastCacheStats == nil {
			continue
		}
		reporting++
		totalRepos += d.LastCacheStats.TotalRepos
		totalActive += d.LastCacheStats.TotalActiveWorktrees
		hits += d.LastCacheStats.HitCount
		misses += d.LastCacheStats.MissCount
	}

	hitRate := 0.0
	if hits+misses > 0 {
		hitRate = float64(hits) / float64(hits+misses) * 100
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cache_hit_rate_pct":     hitRate,
		"total_cached_trees":     totalRepos,
		"total_active_worktrees": totalActive,
		"daemons_reporting":      reporting,
		"daemons_total":          len(daemons),
	})
}
