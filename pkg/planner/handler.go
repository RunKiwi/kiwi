package planner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"gorm.io/gorm/clause"
)

// HandlePlan serves POST /api/v1/planner/plan. Auth and org scoping come from
// the AuthMiddleware claims already in the request context; the org is never
// taken from the request body.
func (s *Service) HandlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req PlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.OrgID = claims.OrgID
	req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	if req.Task == "" {
		http.Error(w, "task is required", http.StatusBadRequest)
		return
	}

	var org auth.Organization
	if err := s.store.DB().WithContext(r.Context()).First(&org, "id = ?", claims.OrgID).Error; err != nil {
		http.Error(w, "organization not found: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// A suspended org (e.g. auto-suspended for abuse via SuspendOrg) cannot submit
	// work. Note we gate on "suspended" specifically, not !CanRun(): free orgs are
	// created "inactive" and run without the paid activation step, so blocking
	// non-active orgs here would lock every free org out.
	if org.ActivationState == "suspended" {
		http.Error(w, "organization is suspended", http.StatusForbidden)
		return
	}
	if org.Plan == "free" {
		req.FleetID = auth.SharedFreeFleet
	}

	res, err := s.SubmitPlan(r.Context(), req)
	if err != nil {
		http.Error(w, "planning failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Free-tier cold-start: now that this org's work is queued (with fleet_id
	// shared-free), ensure a per-org daemon is (being) provisioned to lease it.
	// This is the daemon-fed submit path — `kiwi submit` posts here — so the
	// cold-start belongs here, not on the CP-side /tasks path.
	if org.Plan == "free" {
		s.ensureFreeDaemon(r.Context(), claims.OrgID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(res)
}

// ensureFreeDaemon enqueues a provision request so a per-org free daemon spins up
// to lease the org's work. The idx_prov_one_pending_provision partial unique
// index (provisioner.EnsureSchema) makes this idempotent under concurrent
// submits: a racing insert hits ON CONFLICT DO NOTHING. Best-effort and
// non-fatal — a failure here must not fail an already-accepted submission; the
// provisioner also retries nothing, so the next submit re-attempts.
func (s *Service) ensureFreeDaemon(ctx context.Context, orgID string) {
	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	req := auth.ProvisioningRequest{
		ID:        "prov_" + hex.EncodeToString(idBytes),
		OrgID:     orgID,
		Type:      "provision",
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	if err := s.store.DB().WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&req).Error; err != nil {
		log.Printf("[coldstart] enqueue provision for org %s: %v", orgID, err)
	}
}
