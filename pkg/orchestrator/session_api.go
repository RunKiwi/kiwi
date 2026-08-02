package orchestrator

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleDaemonSession serves POST /api/v1/daemon/session — a session's durable
// checkpoint plus the events that belong to it.
//
// This is what makes a task-long agentic conversation survivable on a lease
// queue built for disposable work. The daemon holds the conversation; the
// Control Plane holds the only copy of where it got to, so a crashed daemon's
// task resumes at its last finished round instead of starting from nothing.
//
// Unlike progress telemetry, this is NOT best-effort from the daemon's point of
// view — a lost checkpoint costs a repeated round — but it is still fenced by
// the lease token for the same reason every other daemon write is: a daemon
// whose lease was reassigned must not write over the run that replaced it.
func (s *Server) handleDaemonSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req daemon.SessionCheckpointReq
	_, _, err := readSignedBody(r, func(b []byte) (string, error) {
		if err := json.Unmarshal(b, &req); err != nil {
			return "", errors.New("invalid request body")
		}
		return req.SignPubKey, nil
	})
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if req.SessionID == "" || req.TaskID == "" || req.LeaseID == "" {
		http.Error(w, "session_id, task_id and lease_id are required", http.StatusBadRequest)
		return
	}

	// The org comes from the signing identity, never from the body: these rows
	// are org-scoped and a spoofed caller must not be able to write into another
	// tenant's history.
	d, err := s.storage.GetDaemonBySignPubKey(r.Context(), req.SignPubKey)
	if err != nil {
		if errors.Is(err, store.ErrDaemonNotFound) {
			http.Error(w, "daemon not registered", http.StatusForbidden)
			return
		}
		log.Printf("[daemon] session checkpoint lookup failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Ownership. RenewLease is a cheap, exact statement of "do you still hold
	// this task" and it is already the authority everywhere else; reusing it
	// keeps one answer rather than two that can disagree.
	held, err := s.storage.RenewLease(r.Context(), req.TaskID, req.LeaseID, leaseTTL)
	if err != nil {
		log.Printf("[daemon] session checkpoint lease check for %s: %v", req.TaskID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !held {
		// 409 rather than 204: unlike progress, the daemon has something useful
		// to do with this — stop the run rather than keep spending on a task
		// whose result will be rejected anyway.
		http.Error(w, "lease no longer held", http.StatusConflict)
		return
	}

	// A status-only request closes the session out.
	if req.Status != "" {
		if err := s.storage.FinishAgentSession(r.Context(), d.OrgID, req.SessionID, req.Status); err != nil {
			log.Printf("[daemon] finish session %s: %v", req.SessionID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	sess := &store.AgentSession{
		ID:             req.SessionID,
		OrgID:          d.OrgID,
		JobID:          req.JobID,
		TaskID:         req.TaskID,
		RepoURL:        req.RepoURL,
		Branch:         req.Branch,
		BaseSHA:        req.BaseSHA,
		HeadSHA:        req.HeadSHA,
		Phase:          req.Phase,
		Round:          req.Round,
		RoundAttempts:  req.Attempts,
		MaxRounds:      req.MaxRounds,
		Rejections:     req.Rejections,
		ArchitectModel: req.ArchitectModel,
		WorkerModel:    req.WorkerModel,
		CostUSD:        req.CostUSD,
		TokensIn:       req.TokensIn,
		TokensOut:      req.TokensOut,
		Status:         store.SessionRunning,
	}
	if len(req.State) > 0 {
		var state map[string]interface{}
		if err := json.Unmarshal(req.State, &state); err != nil {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		sess.State = state
	}

	events := make([]store.AgentSessionEvent, 0, len(req.Events))
	for _, e := range req.Events {
		events = append(events, store.AgentSessionEvent{
			Round:      e.Round,
			Seq:        e.Seq,
			Kind:       e.Kind,
			Outcome:    e.Outcome,
			Tool:       e.Tool,
			Detail:     summarize(e.Detail, 4000),
			DurationMs: e.DurationMs,
			TokensIn:   e.TokensIn,
			TokensOut:  e.TokensOut,
			CostUSD:    e.CostUSD,
		})
	}

	if err := s.storage.SaveAgentSession(r.Context(), sess, events); err != nil {
		log.Printf("[daemon] save session %s: %v", req.SessionID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDaemonSessionLoad serves POST /api/v1/daemon/session/load — "does this
// task already have a session, and where did it get to?"
//
// It is a POST because it is signed, like every daemon call: the signature is
// over the body, and a GET has none to sign.
func (s *Server) handleDaemonSessionLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req daemon.SessionCheckpointReq
	_, _, err := readSignedBody(r, func(b []byte) (string, error) {
		if err := json.Unmarshal(b, &req); err != nil {
			return "", errors.New("invalid request body")
		}
		return req.SignPubKey, nil
	})
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if req.TaskID == "" {
		http.Error(w, "task_id is required", http.StatusBadRequest)
		return
	}

	d, err := s.storage.GetDaemonBySignPubKey(r.Context(), req.SignPubKey)
	if err != nil {
		if errors.Is(err, store.ErrDaemonNotFound) {
			http.Error(w, "daemon not registered", http.StatusForbidden)
			return
		}
		log.Printf("[daemon] session load lookup failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sess, err := s.storage.GetAgentSessionByTask(r.Context(), d.OrgID, req.TaskID)
	if err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			// The common case: a task's first lease. Not a fault.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Printf("[daemon] load session for task %s: %v", req.TaskID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	res := daemon.SessionStateRes{
		Found:     true,
		SessionID: sess.ID,
		Round:     sess.Round,
		Attempts:  sess.RoundAttempts,
		Status:    sess.Status,
	}
	if sess.State != nil {
		if b, err := json.Marshal(sess.State); err == nil {
			res.State = b
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
