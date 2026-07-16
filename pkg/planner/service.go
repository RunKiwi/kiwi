package planner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// Service turns a high-level task into an immutable manifest and enqueues its
// worker specs onto the lease queue for daemons to pick up. Credentials are NOT
// attached here — they are sealed to the specific daemon's public key at
// delivery time (heartbeat), not at plan time.
type Service struct {
	store   store.Store
	planner Planner
}

func NewService(s store.Store, p Planner) *Service {
	return &Service{store: s, planner: p}
}

// SubmitResult reports what the planner generated.
type SubmitResult struct {
	ManifestID string   `json:"manifest_id"`
	JobID      string   `json:"job_id"`
	TaskIDs    []string `json:"task_ids"`
	Summary    string   `json:"summary"`
}

// SubmitPlan runs the planner, persists an immutable content-addressed
// manifest, and enqueues one QueuedTask per planned worker.
func (s *Service) SubmitPlan(ctx context.Context, req PlanRequest) (*SubmitResult, error) {
	if req.OrgID == "" {
		return nil, fmt.Errorf("org id is required")
	}

	plan, err := s.planner.Plan(ctx, req)
	if err != nil {
		return nil, err
	}

	workers := make([]map[string]interface{}, 0, len(plan.Workers))
	for _, w := range plan.Workers {
		workers = append(workers, map[string]interface{}{
			"id":         w.ID,
			"task":       w.Task,
			"file":       w.File,
			"model":      w.Model,
			"depends_on": w.DependsOn,
		})
	}
	content := map[string]interface{}{
		"task":     req.Task,
		"repo_url": req.RepoURL,
		"ref":      req.Ref,
		"summary":  plan.Summary,
		"workers":  workers,
	}

	manifestID, err := contentHash(content)
	if err != nil {
		return nil, err
	}
	m := &store.Manifest{
		ID:            manifestID,
		OrgID:         req.OrgID,
		SchemaVersion: "1.0",
		Content:       content,
		Producer:      "planner",
		CreatedAt:     time.Now(),
	}
	// Persist the manifest and enqueue all worker tasks atomically: if any
	// enqueue fails, the manifest is rolled back too — no partial plans.
	jobID := "job_" + randHex(8)
	taskIDs := make([]string, 0, len(plan.Workers))
	err = s.store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(m).Error; err != nil {
			return fmt.Errorf("persist manifest: %w", err)
		}
		for _, w := range plan.Workers {
			taskID := jobID + "-" + w.ID
			spec := map[string]interface{}{
				"id":         taskID,
				"task":       w.Task,
				"file":       w.File,
				"model":      w.Model,
				"depends_on": w.DependsOn,
				"repo_url":   req.RepoURL,
				"ref":        req.Ref,
			}
			if err := tx.Create(&store.QueuedTask{
				ID:     taskID,
				OrgID:  req.OrgID,
				JobID:  jobID,
				Status: store.TaskQueued,
				Spec:   spec,
			}).Error; err != nil {
				return fmt.Errorf("enqueue task %s: %w", taskID, err)
			}
			taskIDs = append(taskIDs, taskID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &SubmitResult{
		ManifestID: manifestID,
		JobID:      jobID,
		TaskIDs:    taskIDs,
		Summary:    plan.Summary,
	}, nil
}

// contentHash returns the SHA-256 of the canonical JSON encoding of content
// (Go sorts map keys, giving a stable, content-addressed manifest id).
func contentHash(content map[string]interface{}) (string, error) {
	b, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
