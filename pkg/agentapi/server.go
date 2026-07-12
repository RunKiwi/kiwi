package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/checkpoint"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// SecretResolver fetches a just-in-time secret for a job (e.g. via the reverse
// credential tunnel). Implemented by the control plane and injected as a Dep so
// this package stays decoupled from the tunnel.
type SecretResolver interface {
	Resolve(ctx context.Context, jobID, key string) (string, error)
}

// Deps are the control-plane collaborators the Agent API needs.
type Deps struct {
	Store   store.Store
	Events  *checkpoint.Service // per-job monotonic event log
	Secrets SecretResolver
}

// Server hosts the sandbox-facing HTTP/JSON API.
type Server struct {
	deps Deps
}

func NewServer(deps Deps) *Server { return &Server{deps: deps} }

// Handler returns the http.Handler for the Agent API. It is mounted at /agent/
// OUTSIDE the org AuthMiddleware because it authenticates with a per-job scoped
// token rather than an org API key.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agent/{jobID}/events", s.handleAppendEvent)
	mux.HandleFunc("POST /agent/{jobID}/checkpoints", s.handleCheckpoint)
	mux.HandleFunc("POST /agent/{jobID}/secrets", s.handleFetchSecret)
	mux.HandleFunc("POST /agent/{jobID}/result", s.handleReportResult)
	return mux
}

// authorize validates the bearer job token and enforces that it is scoped to the
// job named in the path. A cross-job request is rejected with 403.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) (*JobClaims, bool) {
	claims, err := ValidateJobToken(s.deps.Store.DB(), bearer(r))
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
		return nil, false
	}
	if jobID := r.PathValue("jobID"); jobID != claims.JobID {
		http.Error(w, "forbidden: token is not scoped to this job", http.StatusForbidden)
		return nil, false
	}
	return claims, true
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// --- handlers ---

type appendEventReq struct {
	Phase   string                 `json:"phase"`
	Payload map[string]interface{} `json:"payload"`
}

func (s *Server) handleAppendEvent(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var req appendEventReq
	if !decode(w, r, &req) {
		return
	}
	ev, err := s.deps.Events.AppendEvent(r.Context(), claims.JobID, req.Phase, req.Payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"seq": ev.Seq})
}

type checkpointReq struct {
	EventSeq     int64                  `json:"event_seq"`
	State        map[string]interface{} `json:"state"`
	SnapshotURI  string                 `json:"snapshot_uri"`
	SnapshotHash string                 `json:"snapshot_hash"`
}

// handleCheckpoint records checkpoint metadata the agent produced. The workspace
// blob itself is uploaded out-of-band (LocalSnapshotter today; object store in
// #66) — this endpoint records the anchor + state so the job is resumable.
func (s *Server) handleCheckpoint(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var req checkpointReq
	if !decode(w, r, &req) {
		return
	}
	if req.State == nil {
		req.State = map[string]interface{}{}
	}
	cp := &store.Checkpoint{
		ID:       fmt.Sprintf("%s-cp-%d", claims.JobID, req.EventSeq),
		JobID:    claims.JobID,
		EventSeq: req.EventSeq,
		State:    req.State,
	}
	if req.SnapshotURI != "" {
		cp.SnapshotURI = &req.SnapshotURI
	}
	if req.SnapshotHash != "" {
		cp.SnapshotHash = &req.SnapshotHash
	}
	if err := s.deps.Store.SaveCheckpoint(r.Context(), cp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"checkpoint_id": cp.ID})
}

type fetchSecretReq struct {
	Key string `json:"key"`
}

func (s *Server) handleFetchSecret(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var req fetchSecretReq
	if !decode(w, r, &req) {
		return
	}
	if s.deps.Secrets == nil {
		http.Error(w, "secret resolver not configured", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	val, err := s.deps.Secrets.Resolve(ctx, claims.JobID, req.Key)
	if err != nil {
		http.Error(w, "secret unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"value": val})
}

type reportResultReq struct {
	Status string `json:"status"` // SUCCEEDED | FAILED
	Error  string `json:"error"`
}

func (s *Server) handleReportResult(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var req reportResultReq
	if !decode(w, r, &req) {
		return
	}
	status := "SUCCEEDED"
	if strings.EqualFold(req.Status, "FAILED") {
		status = "FAILED"
	}
	updates := map[string]interface{}{"status": status}
	if req.Error != "" {
		updates["error"] = req.Error
	}
	if err := s.deps.Store.DB().WithContext(r.Context()).
		Model(&store.Job{}).Where("id = ?", claims.JobID).Updates(updates).Error; err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": status})
}

// --- helpers ---

func decode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
