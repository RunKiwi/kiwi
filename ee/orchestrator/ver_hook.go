// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// recordTaskEvents persists the telemetry a daemon reported for one task. These
// rows are the evidence an execution record is later assembled from: the daemon
// runs the Actor–Critic loop in its own process, so nothing else observes it.
func (s *Server) recordTaskEvents(ctx context.Context, orgID, taskID string, events []ver.TaskEvent) {
	if len(events) == 0 {
		return
	}
	rows := make([]TaskEvent, 0, len(events))
	for _, e := range events {
		rows = append(rows, TaskEvent{
			TaskID:  taskID,
			OrgID:   orgID,
			Step:    e.Step,
			Phase:   e.Phase,
			Outcome: e.Outcome,
			Detail:  summarize(e.Detail, 4000),
			// Arguments are kept from the front: a tool call says what it is at
			// its start, unlike output, which explains itself at its end.
			Input:        headOf(e.Input, 1000),
			DurationMs:   e.DurationMs,
			InputTokens:  e.InputTokens,
			OutputTokens: e.OutputTokens,
			CostUSD:      e.CostUSD,
			// The daemon stamps each event as it happens; a flush carries several
			// at once, so falling back to now would collapse them onto one instant.
			CreatedAt: eventTime(e.At),
		})
	}
	if err := s.db.WithContext(ctx).Create(&rows).Error; err != nil {
		log.Printf("[ver] persist %d events for task %s: %v", len(rows), taskID, err)
	}
}

// taskEventsFor loads the persisted telemetry for one task, in execution order.
func (s *Server) taskEventsFor(ctx context.Context, orgID, taskID string) []ver.TaskEvent {
	var rows []TaskEvent
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND task_id = ?", orgID, taskID).
		Order("id ASC").Find(&rows).Error; err != nil {
		log.Printf("[ver] load events for task %s: %v", taskID, err)
		return nil
	}
	out := make([]ver.TaskEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, ver.TaskEvent{
			Step:         r.Step,
			Phase:        r.Phase,
			Outcome:      r.Outcome,
			Detail:       r.Detail,
			Input:        r.Input,
			DurationMs:   r.DurationMs,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			CostUSD:      r.CostUSD,
			At:           r.CreatedAt,
		})
	}
	return out
}

// execContext is what the reporting daemon told us about how it ran the task.
type execContext struct {
	FleetID        string
	DaemonID       string
	DaemonPubKey   string
	SandboxRuntime string
	Mode           string
	ExecSignature  *ver.Signature
}

// maybeAssembleRecord appends the execution record for a job once every task in
// it has reached a terminal state.
//
// It never returns an error: a missing record is a gap in provenance, not a
// reason to fail the daemon's result report, which has already been accepted
// and must stay accepted. Failures are logged loudly instead.
func (s *Server) maybeAssembleRecord(ctx context.Context, orgID, taskID string, ec execContext) {
	defer func() {
		// This runs on its own goroutine; an unrecovered panic here would take
		// down the whole Control Plane over a telemetry side-effect.
		if r := recover(); r != nil {
			log.Printf("[ver] panic assembling record for task %s: %v", taskID, r)
		}
	}()

	task, err := s.storage.GetQueuedTask(ctx, taskID)
	if err != nil || task == nil {
		log.Printf("[ver] load task %s: %v", taskID, err)
		return
	}
	if task.OrgID != orgID {
		log.Printf("[ver] task %s does not belong to org %s", taskID, orgID)
		return
	}
	jobID := task.JobID
	if jobID == "" {
		// A task with no job (direct submission) has nothing to assemble against.
		return
	}

	tasks, err := s.storage.GetJobTasks(ctx, orgID, jobID)
	if err != nil {
		log.Printf("[ver] load tasks for job %s: %v", jobID, err)
		return
	}
	if len(tasks) == 0 {
		return
	}
	// Assemble only once the whole job is done, so the record covers every
	// worker rather than whichever one happened to report last.
	for _, t := range tasks {
		if t.Status == store.TaskSucceeded || t.Status == store.TaskFailed {
			continue
		}
		// A TaskPlanReview task is acceptable if it was superseded by a completed continuation.
		if t.Status == store.TaskPlanReview && hasCompletedSuccessor(t, tasks) {
			continue
		}
		return
	}

	job, err := s.storage.GetJob(ctx, jobID)
	if err != nil || job == nil {
		log.Printf("[ver] load job %s: %v", jobID, err)
		return
	}
	if job.OrgID != orgID {
		log.Printf("[ver] job %s does not belong to org %s", jobID, orgID)
		return
	}

	var manifest *store.Manifest
	if job.ManifestID != nil && *job.ManifestID != "" {
		if m, err := s.storage.GetManifest(ctx, *job.ManifestID); err == nil {
			manifest = m
		} else {
			log.Printf("[ver] load manifest %s: %v", *job.ManifestID, err)
		}
	}

	in := s.buildAssembleInput(ctx, orgID, jobID, job, manifest, tasks, ec)

	cpKey, keyErr := s.cpSigningKey()

	_, err = s.storage.AppendExecutionRecord(ctx, orgID, jobID, ver.SchemaVersion, func(prevHash string) (*store.ExecutionRecord, error) {
		in.PrevRecordHash = prevHash
		rec, err := ver.AssembleRecord(in)
		if err != nil {
			return nil, err
		}
		if ec.ExecSignature != nil {
			rec.ExecSignature = ec.ExecSignature
		}

		signature := ""
		signingKeyID := ""
		if keyErr == nil {
			// SignRecord marks the record signed as part of signing, because
			// Attestation is inside the signed payload.
			sig, err := ver.SignRecord(rec, cpKey.ID, cpKey.Priv)
			if err != nil {
				return nil, err
			}
			rec.RecordSignature = sig
			signature = sig.Sig
			signingKeyID = cpKey.ID
		} else {
			// No configured key: persist unsigned and say so, rather than
			// minting an ephemeral key whose signature nothing could check.
			rec.Attestation = ver.AttestationUnsigned
		}

		// Hashed last. The chain link excludes the signatures but covers every
		// other field — including Attestation — so it must be computed once the
		// record is final, or the chained hash is not the one a verifier gets.
		hash, err := ver.RecordHash(rec)
		if err != nil {
			return nil, err
		}

		body, err := json.Marshal(rec)
		if err != nil {
			return nil, err
		}
		execSig := ""
		if rec.ExecSignature != nil {
			execSig = rec.ExecSignature.Sig
		}
		return &store.ExecutionRecord{
			RecordID:        rec.RecordID,
			OrgID:           orgID,
			JobID:           jobID,
			Ver:             rec.Ver,
			PrevRecordHash:  prevHash,
			RecordHash:      hash,
			Body:            body,
			ExecSignature:   execSig,
			RecordSignature: signature,
			SigningKeyID:    signingKeyID,
			CreatedAt:       time.Now(),
		}, nil
	})
	switch {
	case err == nil:
		if keyErr != nil {
			log.Printf("[ver] recorded job %s UNSIGNED: %v", jobID, keyErr)
		} else {
			log.Printf("[ver] recorded job %s", jobID)
		}
	case errors.Is(err, store.ErrRecordExists):
		// Expected: every worker in a multi-worker job races to trigger this.
	default:
		log.Printf("[ver] append record for job %s: %v", jobID, err)
	}
}

// buildAssembleInput maps storage rows onto the pure assembler's input. Facts
// that cannot be observed are left empty on purpose — AssembleRecord marks them
// "unknown" rather than guessing, because a signed record's fields read as
// attested truth.
func (s *Server) buildAssembleInput(
	ctx context.Context,
	orgID, jobID string,
	job *store.Job,
	manifest *store.Manifest,
	tasks []store.QueuedTask,
	ec execContext,
) ver.AssembleInput {
	in := ver.AssembleInput{
		OrgID:          orgID,
		JobID:          jobID,
		Task:           jobInputString(job, "task"),
		Source:         jobInputString(job, "source"),
		SubmittedBy:    job.UserID,
		SubmittedAtRFC: job.CreatedAt.UTC().Format(time.RFC3339),
		TestCmd:        jobInputString(job, "test_cmd"),
		Repo:           jobInputString(job, "repo_url"),
		Ref:            jobInputString(job, "ref"),
		FleetID:        ec.FleetID,
		Mode:           ec.Mode,
		DaemonID:       ec.DaemonID,
		DaemonPubKey:   ec.DaemonPubKey,
		SandboxRT:      ec.SandboxRuntime,
		// The sandbox denies egress by default for the test command; this is a
		// property of how the daemon launches it, not a per-run measurement.
		SandboxNet: "default-deny",
	}
	if job.IdempotencyKey != nil {
		in.IdempotencyKey = *job.IdempotencyKey
	}
	if manifest != nil {
		in.PlanManifestHash = manifestHash(manifest)
		in.PlanSummary = mapString(manifest.Content, "summary")
		in.PlannerModel = mapString(manifest.Content, "planner_model")
		in.PlannerProvider = mapString(manifest.Content, "planner_provider")
		in.ReferenceMode = mapString(manifest.Content, "reference_mode")
		if refs, ok := manifest.Content["reference_job_ids"].([]interface{}); ok {
			for _, r := range refs {
				if str, ok := r.(string); ok {
					in.ReferenceJobIDs = append(in.ReferenceJobIDs, str)
				}
			}
		}
	}

	files := map[string]struct{}{}
	var lastFinished *store.QueuedTask
	anyFailed := false
	for i := range tasks {
		t := &tasks[i]
		if t.Status == store.TaskFailed {
			anyFailed = true
		}
		if lastFinished == nil || t.UpdatedAt.After(lastFinished.UpdatedAt) {
			lastFinished = t
		}

		workerID := specString(t.Spec, "worker_id")
		if workerID == "" {
			workerID = t.ID
		}
		w := ver.WorkerInput{
			WorkerID:    workerID,
			ActorModel:  specString(t.Spec, "model"),
			CriticModel: specString(t.Spec, "model"),
			Provider:    providerForModel(specString(t.Spec, "model")),
			Events:      s.taskEventsFor(ctx, orgID, t.ID),
		}
		if deps, ok := t.Spec["depends_on"].([]interface{}); ok {
			for _, d := range deps {
				if str, ok := d.(string); ok {
					w.DependsOn = append(w.DependsOn, str)
				}
			}
		}
		if f := specString(t.Spec, "file"); f != "" {
			files[f] = struct{}{}
		}
		if fs, ok := t.Spec["files"].([]interface{}); ok {
			for _, f := range fs {
				if str, ok := f.(string); ok && str != "" {
					files[str] = struct{}{}
				}
			}
		}
		in.Workers = append(in.Workers, w)
	}

	for f := range files {
		in.FilesTouched = append(in.FilesTouched, f)
	}
	sortStrings(in.FilesTouched)

	in.FinalOutcome = "pass"
	if anyFailed {
		in.FinalOutcome = "fail"
	}
	if lastFinished != nil {
		if lastFinished.ResultDetail != nil && *lastFinished.ResultDetail != "" {
			in.TestOutputHash = ver.HashString(*lastFinished.ResultDetail)
		}
		if lastFinished.ResultURL != nil {
			in.Delivery.PRURL = *lastFinished.ResultURL
			in.Delivery.OpenedAt = lastFinished.UpdatedAt.UTC().Format(time.RFC3339)
		}
		if lastFinished.StartedAt != nil && !lastFinished.UpdatedAt.IsZero() {
			in.VerifyDurationMs = lastFinished.UpdatedAt.Sub(*lastFinished.StartedAt).Milliseconds()
		}
	}
	return in
}

// manifestHash is the content hash of the plan, which is what "governing spec"
// must pin — the manifest's row ID identifies it but does not bind its content.
func manifestHash(m *store.Manifest) string {
	h, err := ver.Hash(m.Content)
	if err != nil {
		return ""
	}
	return h
}

func jobInputString(job *store.Job, key string) string {
	if job == nil {
		return ""
	}
	return mapString(job.Inputs, key)
}

func mapString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func specString(spec map[string]interface{}, key string) string { return mapString(spec, key) }

// providerForModel names the provider a model ran on, for the signed execution
// record. It shares provider.ProviderOf with the daemon that actually made the
// calls: a record that attested to a different provider than the one billed
// would be a false attestation, not a cosmetic mismatch.
//
// An empty model stays empty — the record must not claim a provider for a step
// that named no model.
func providerForModel(model string) string {
	if model == "" {
		return ""
	}
	return provider.ProviderOf(model)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// decodeDaemonPubKey renders a daemon's Ed25519 identity for the record.
func decodeDaemonPubKey(b64 string) string {
	if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
		return ""
	}
	return b64
}

// replaceTaskEvents makes the daemon's final report the authoritative history
// for a task, discarding anything streamed while it ran.
//
// Progress updates deliver events incrementally so a run can be watched, and
// the final report re-sends the whole list. Appending both would duplicate
// every phase in the timeline, and — because the execution record is assembled
// from these rows — would double the step count and the cost attributed to the
// run. Replacing is also self-healing: a task whose progress updates were lost
// to a network blip still ends with a complete history.
//
// Deleting first is safe because a task's events are only ever written by the
// daemon holding its lease, and this runs at completion, after which no further
// progress update can apply.
func (s *Server) replaceTaskEvents(ctx context.Context, orgID, taskID string, events []ver.TaskEvent) {
	if len(events) == 0 {
		// A run that reported nothing must not erase what streaming captured.
		return
	}
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND task_id = ?", orgID, taskID).
		Delete(&TaskEvent{}).Error; err != nil {
		// Falling through to the insert would duplicate rather than replace, so
		// stop: the streamed history is already the better of the two outcomes.
		log.Printf("[ver] clear streamed events for task %s: %v", taskID, err)
		return
	}
	s.recordTaskEvents(ctx, orgID, taskID, events)
}

// hasCompletedSuccessor reports whether a superseded task (such as a TaskPlanReview
// task) has a completed continuation in the same job.
func hasCompletedSuccessor(t store.QueuedTask, tasks []store.QueuedTask) bool {
	for _, other := range tasks {
		if other.ID == t.ID {
			continue
		}
		if other.Status != store.TaskSucceeded && other.Status != store.TaskFailed {
			continue
		}
		if (other.ParentTaskID != nil && *other.ParentTaskID == t.ID) ||
			(other.RootTaskID != "" && (other.RootTaskID == t.RootTaskID || other.RootTaskID == t.ID) && other.Origin == store.OriginPlanApproved) {
			return true
		}
	}
	return false
}
