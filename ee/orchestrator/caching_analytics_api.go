// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleCachingAnalytics serves GET /api/v1/analytics/caching, summing the
// cache/raw prompt token split (Task 1-3) across the org's tasks in the last
// 30 days and pricing the savings per-model via pkg/provider's existing cache
// rate table.
func (s *Server) handleCachingAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	since := time.Now().Add(-30 * 24 * time.Hour)
	var tasks []store.QueuedTask
	if err := s.db.WithContext(r.Context()).
		Where("org_id = ? AND created_at >= ?", claims.OrgID, since).
		Find(&tasks).Error; err != nil {
		http.Error(w, "failed to query tasks", http.StatusInternalServerError)
		return
	}

	var cached, raw int64
	var savings float64
	for _, t := range tasks {
		cached += t.CachedPromptTokens
		raw += t.RawPromptTokens
		model, _ := t.Spec["model"].(string)
		if model != "" && t.CachedPromptTokens > 0 {
			savings += provider.CacheDiscountUSD(model, t.CachedPromptTokens)
		}
	}

	var discountRate float64
	if total := cached + raw; total > 0 {
		discountRate = float64(cached) / float64(total)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cached_prompt_tokens":     cached,
		"raw_prompt_tokens":        raw,
		"cache_discount_rate":      discountRate,
		"total_dollar_savings_usd": savings,
	})
}
