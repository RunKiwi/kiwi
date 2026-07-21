package orchestrator

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// accountUsageResponse is the compact plan + current-month usage summary the
// dashboard renders as a usage meter and suspended notice. It is distinct from
// the legacy /usage cost report (billing.GetOrgUsage).
type accountUsageResponse struct {
	Plan                  string  `json:"plan"`
	ActivationState       string  `json:"activation_state"`
	AgentMinutesUsed      float64 `json:"agent_minutes_used"`
	AgentMinutesLimit     float64 `json:"agent_minutes_limit"` // 0 = unlimited
	ConcurrentJobsRunning int64   `json:"concurrent_jobs_running"`
	MaxConcurrentJobs     int     `json:"max_concurrent_jobs"`
}

// handleAccountUsage serves GET /api/v1/usage: the caller org's plan, activation
// state, and current billing-month agent-minutes/concurrency against its limits.
func (s *Server) handleAccountUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var org auth.Organization
	if err := s.db.First(&org, "id = ?", claims.OrgID).Error; err != nil {
		http.Error(w, "organization not found", http.StatusInternalServerError)
		return
	}

	limits, err := auth.GetOrgLimits(s.db, claims.OrgID)
	if err != nil {
		http.Error(w, "failed to load limits", http.StatusInternalServerError)
		return
	}

	// Agent-minutes accrue on Job.agent_minutes; meter the current billing month,
	// matching the lease-time enforcement in LeaseNextTask.
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var used float64
	if err := s.db.Model(&store.Job{}).
		Where("org_id = ? AND created_at >= ?", claims.OrgID, monthStart).
		Select("COALESCE(SUM(agent_minutes), 0)").Scan(&used).Error; err != nil {
		http.Error(w, "failed to aggregate usage", http.StatusInternalServerError)
		return
	}

	var running int64
	if err := s.db.Model(&store.QueuedTask{}).
		Where("org_id = ? AND status = ?", claims.OrgID, store.TaskLeased).
		Count(&running).Error; err != nil {
		http.Error(w, "failed to count running jobs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(accountUsageResponse{
		Plan:                  org.Plan,
		ActivationState:       org.ActivationState,
		AgentMinutesUsed:      used,
		AgentMinutesLimit:     limits.MaxAgentMinutesPerMonth,
		ConcurrentJobsRunning: running,
		MaxConcurrentJobs:     limits.MaxConcurrentJobs,
	})
}
