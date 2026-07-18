package planner

import (
	"encoding/json"
	"net/http"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
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

	res, err := s.SubmitPlan(r.Context(), req)
	if err != nil {
		http.Error(w, "planning failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(res)
}
