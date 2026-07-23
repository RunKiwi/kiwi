package planner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrIdempotentConflict = errors.New("idempotent conflict")

// Service turns a high-level task into an immutable manifest and enqueues its
// worker specs onto the lease queue for daemons to pick up. Credentials are NOT
// attached here — they are sealed to the specific daemon's public key at
// delivery time (heartbeat), not at plan time.
type Service struct {
	store   store.Store
	planner Planner
	embed   provider.Embedder
	// indexSync runs learning indexing inline instead of in a background
	// goroutine. Production leaves it false (best-effort, non-blocking); tests
	// set it true so the write is observable without racing a goroutine.
	indexSync bool
}

func NewService(s store.Store, p Planner, e provider.Embedder) *Service {
	return &Service{store: s, planner: p, embed: e}
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

	if req.IdempotencyKey != "" {
		var sub store.PlanSubmission
		if err := s.store.DB().WithContext(ctx).Where("org_id = ? AND idempotency_key = ?", req.OrgID, req.IdempotencyKey).First(&sub).Error; err == nil {
			var tasks []store.QueuedTask
			if err := s.store.DB().WithContext(ctx).Where("org_id = ? AND job_id = ?", req.OrgID, sub.JobID).Order("id asc").Find(&tasks).Error; err != nil {
				return nil, err
			}
			taskIDs := make([]string, len(tasks))
			for i, t := range tasks {
				taskIDs[i] = t.ID
			}
			return &SubmitResult{
				ManifestID: "deduplicated",
				JobID:      sub.JobID,
				TaskIDs:    taskIDs,
				Summary:    "Deduplicated run",
			}, nil
		}
	}

	// Resolve prior-work learnings before planning. Everything is org-scoped in
	// the store queries — a caller can never reference another tenant's jobs.
	// taskVec is captured so an "auto" submit reuses its query embedding for the
	// post-plan indexing write instead of paying for a second embed call.
	var resolved []store.JobLearning
	var taskVec []float32
	switch req.ReferenceMode {
	case "manual":
		if len(req.ReferenceJobIDs) > 0 {
			resolved, _ = s.store.GetJobLearnings(ctx, req.OrgID, req.ReferenceJobIDs)
			if len(resolved) > 3 {
				resolved = resolved[:3]
			}
		}
	case "auto":
		if s.embed != nil {
			if vec, err := s.embed.Embed(ctx, req.Task); err == nil {
				taskVec = vec
				resolved, _ = s.store.SearchJobLearnings(ctx, req.OrgID, vec, 3, "")
			}
		}
	}
	req.ResolvedLearnings = resolved

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
			"files":      w.Files,
			"model":      w.Model,
			"test_cmd":   workerTestCmd(w, req),
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
		if req.IdempotencyKey != "" {
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&store.PlanSubmission{
				OrgID:          req.OrgID,
				IdempotencyKey: req.IdempotencyKey,
				JobID:          jobID,
			})
			if res.Error != nil {
				return fmt.Errorf("persist idempotency key: %w", res.Error)
			}
			if res.RowsAffected == 0 {
				return ErrIdempotentConflict
			}
		}

		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(m).Error; err != nil {
			return fmt.Errorf("persist manifest: %w", err)
		}
		for _, w := range plan.Workers {
			taskID := jobID + "-" + w.ID
			spec := map[string]interface{}{
				"id":         taskID,
				"task":       w.Task,
				"job_task":   req.Task,
				"file":       w.File,
				"model":      w.Model,
				"test_cmd":   workerTestCmd(w, req),
				"depends_on": w.DependsOn,
				"repo_url":   req.RepoURL,
				"ref":        req.Ref,
				"job_id":     jobID,
			}
			if err := tx.Create(&store.QueuedTask{
				ID:      taskID,
				OrgID:   req.OrgID,
				JobID:   jobID,
				FleetID: req.FleetID,
				Status:  store.TaskQueued,
				Spec:    spec,
			}).Error; err != nil {
				return fmt.Errorf("enqueue task %s: %w", taskID, err)
			}
			taskIDs = append(taskIDs, taskID)
		}
		return nil
	})
	if err == ErrIdempotentConflict {
		var sub store.PlanSubmission
		if err := s.store.DB().WithContext(ctx).Where("org_id = ? AND idempotency_key = ?", req.OrgID, req.IdempotencyKey).First(&sub).Error; err == nil {
			var tasks []store.QueuedTask
			if err := s.store.DB().WithContext(ctx).Where("org_id = ? AND job_id = ?", req.OrgID, sub.JobID).Order("id asc").Find(&tasks).Error; err != nil {
				return nil, err
			}
			taskIDs := make([]string, len(tasks))
			for i, t := range tasks {
				taskIDs[i] = t.ID
			}
			return &SubmitResult{
				ManifestID: "deduplicated",
				JobID:      sub.JobID,
				TaskIDs:    taskIDs,
				Summary:    "Deduplicated run",
			}, nil
		}
	}
	if err != nil {
		return nil, err
	}

	// Best-effort, non-fatal indexing of this job as a learning for future
	// reference. It must never fail an already-accepted submission, so errors are
	// swallowed and (in production) it runs off the request goroutine.
	index := func(taskVec []float32) {
		// A detached, bounded context: the request's context may already be
		// canceled by the time this runs.
		ictx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Reuse the auto-mode query embedding when we have it; only embed here
		// when we don't (manual/off submits), so auto never embeds twice.
		if taskVec == nil && s.embed != nil {
			if v, err := s.embed.Embed(ictx, req.Task); err == nil {
				taskVec = v
			}
		}
		var vec *pgvector.Vector
		if taskVec != nil {
			pv := pgvector.NewVector(taskVec)
			vec = &pv
		}

		// One learning per job: key the row on the job id so a re-index updates in
		// place rather than duplicating (UpsertJobLearning conflicts on job_id).
		learning := &store.JobLearning{
			ID:        jobID,
			JobID:     jobID,
			OrgID:     req.OrgID,
			Repo:      store.ShortRepo(req.RepoURL),
			Task:      req.Task,
			Summary:   plan.Summary,
			Embedding: vec,
		}
		_ = s.store.UpsertJobLearning(ictx, learning)
	}
	if s.indexSync {
		index(taskVec)
	} else {
		go index(taskVec)
	}

	return &SubmitResult{
		ManifestID: manifestID,
		JobID:      jobID,
		TaskIDs:    taskIDs,
		Summary:    plan.Summary,
	}, nil
}

// workerTestCmd resolves the test command for a worker: a per-worker command
// from the planner takes precedence, otherwise the plan-wide command from the
// request. Empty when neither is set (the daemon then cannot run a verifying
// loop for that worker).
func workerTestCmd(w PlannedWorker, req PlanRequest) string {
	if w.TestCmd != "" {
		return w.TestCmd
	}
	return req.TestCmd
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
