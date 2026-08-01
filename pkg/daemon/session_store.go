package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/session"
)

// cpSessionStore is session.Store backed by the Control Plane.
//
// The daemon has no database — it is a pull-model process that may be running
// in a customer's cloud — so durability travels over the same signed, lease-
// fenced channel every other daemon report uses. The fencing token matters as
// much here as it does for a result: a daemon whose lease was reassigned must
// not be able to write checkpoints over the run that replaced it.
type cpSessionStore struct {
	client  *Client
	taskID  string
	leaseID string
	// signPubKey identifies this daemon to the Control Plane.
	signPubKey string
	// identity fields, sent on every checkpoint so the row can be created on the
	// first one without a separate "open a session" call.
	jobID          string
	repoURL        string
	branch         string
	architectModel string
	workerModel    string
	maxRounds      int
}

func (s *cpSessionStore) Load(ctx context.Context, sessionID string) (*session.Checkpoint, error) {
	res, err := s.client.LoadSession(ctx, s.taskID, sessionID, s.signPubKey)
	if err != nil {
		return nil, err
	}
	if res == nil || !res.Found || len(res.State) == 0 {
		return nil, nil
	}
	// A concluded session is not resumed. A task leased again after its session
	// succeeded or failed is a retry, and continuing the old conversation would
	// hand the Architect a history that ends in a verdict it already gave.
	if res.Status != "" && res.Status != "RUNNING" {
		return nil, nil
	}
	var cp session.Checkpoint
	if err := json.Unmarshal(res.State, &cp); err != nil {
		return nil, fmt.Errorf("decode session checkpoint: %w", err)
	}
	return &cp, nil
}

func (s *cpSessionStore) Save(ctx context.Context, sessionID string, cp session.Checkpoint, events []session.Event) error {
	state, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("encode session checkpoint: %w", err)
	}

	total := cp.Architect
	total.Add(cp.Implementer)

	req := SessionCheckpointReq{
		SessionID:      sessionID,
		TaskID:         s.taskID,
		LeaseID:        s.leaseID,
		SignPubKey:     s.signPubKey,
		JobID:          s.jobID,
		RepoURL:        s.repoURL,
		Branch:         s.branch,
		BaseSHA:        cp.BaseSHA,
		HeadSHA:        cp.HeadSHA,
		Round:          cp.Round,
		Attempts:       cp.Attempts,
		MaxRounds:      s.maxRounds,
		Rejections:     cp.Rejections,
		State:          state,
		ArchitectModel: s.architectModel,
		WorkerModel:    s.workerModel,
		CostUSD:        total.CostUSD,
		TokensIn:       total.InputTokens + total.CacheReadTokens + total.CacheWriteTokens,
		TokensOut:      total.OutputTokens,
		Events:         wireEvents(events),
	}
	if cp.Spec.Verdict != "" {
		req.Phase = cp.Spec.Verdict
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.client.CheckpointSession(ctx, req)
}

func (s *cpSessionStore) Finish(ctx context.Context, sessionID string, success bool) error {
	status := "FAILED"
	if success {
		status = "SUCCEEDED"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.client.CheckpointSession(ctx, SessionCheckpointReq{
		SessionID:  sessionID,
		TaskID:     s.taskID,
		LeaseID:    s.leaseID,
		SignPubKey: s.signPubKey,
		Status:     status,
	})
}

func wireEvents(events []session.Event) []SessionEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]SessionEvent, 0, len(events))
	for _, e := range events {
		out = append(out, SessionEvent{
			Round:      e.Round,
			Seq:        e.Seq(),
			Kind:       e.Phase,
			Outcome:    e.Outcome,
			Tool:       e.Tool,
			Detail:     e.Detail,
			DurationMs: e.DurationMs,
			TokensIn:   e.InputTokens,
			TokensOut:  e.OutputTokens,
			CostUSD:    e.CostUSD,
		})
	}
	return out
}

// sessionIDFor derives a stable session id from the task it belongs to.
//
// Deterministic on purpose: a resumed run must arrive at the same id without
// having to be told it, and the task id is the one identifier both sides
// already agree on. It also makes the one-session-per-task rule enforceable by
// a unique index rather than by convention.
func sessionIDFor(taskID string) string { return "sess_" + taskID }
