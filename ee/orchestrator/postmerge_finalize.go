// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ibreakthecloud/kiwi/ee/planner"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// finalizeMonitor is the single path every Phase 1a trigger (revert-PR
// detection, check-run failure, the window-elapsed sweep) funnels through.
// It performs, in order: the atomic single-fire status transition, the
// signed kiwi.ver/postmerge/v1 record, the PR comment, and — only on
// REGRESSION with the org opted in — a remediation continuation. Each step
// after the status transition is best-effort: a failure is logged, not
// returned, because the transition already committed and must not be
// retried by a caller that thinks it failed outright.
func (s *Server) finalizeMonitor(ctx context.Context, mon *store.PostMergeMonitor, verdict, evidence string) {
	won, err := s.storage.FinalizeMonitor(ctx, mon.ID, verdict, evidence)
	if err != nil {
		log.Printf("[postmerge] finalize monitor %s: %v", mon.ID, err)
		return
	}
	if !won {
		// Another trigger already finalized this monitor first — expected
		// under a race (e.g. a check run fails right as the window elapses).
		return
	}

	s.appendPostMergeRecord(ctx, mon, verdict, evidence)
	s.notifyMonitorVerdict(ctx, mon, verdict, evidence)
	s.notifySlackVerdict(ctx, mon, verdict, evidence)

	if verdict != store.MonitorStatusRegression {
		return
	}
	autoRemediate, err := s.storage.AutoRemediate(ctx, mon.OrgID)
	if err != nil {
		log.Printf("[postmerge] check auto_remediate for org %s: %v", mon.OrgID, err)
		return
	}
	if !autoRemediate {
		return
	}
	s.submitRemediation(ctx, mon, evidence)
}

// appendPostMergeRecord chains a signed verdict record off the job's kiwi.ver
// history, mirroring exactly how the merge webhook chains MergeRecord off the
// original record (github_webhook.go).
func (s *Server) appendPostMergeRecord(ctx context.Context, mon *store.PostMergeMonitor, verdict, evidence string) {
	originalRec, err := s.storage.GetExecutionRecord(ctx, mon.OrgID, mon.JobID)
	if err != nil {
		log.Printf("[postmerge] find original record for job %s: %v", mon.JobID, err)
		return
	}

	cpKey, keyErr := s.cpSigningKey()
	if keyErr != nil {
		log.Printf("[postmerge] no signing key; recording verdict for job %s unsigned: %v", mon.JobID, keyErr)
	}

	_, err = s.storage.AppendExecutionRecord(ctx, mon.OrgID, mon.JobID, ver.PostMergeVerifySchemaVersion, func(prevHash string) (*store.ExecutionRecord, error) {
		rec := &ver.PostMergeVerificationRecord{
			Ver:              ver.PostMergeVerifySchemaVersion,
			RecordID:         "rec_" + uuid.New().String(),
			OriginalRecordID: originalRec.RecordID,
			OrgID:            mon.OrgID,
			JobID:            mon.JobID,
			PrevRecordHash:   prevHash,
			Attestation:      ver.AttestationUnsigned,
			Verdict:          verdict,
			Evidence:         evidence,
			FinalizedAt:      time.Now().Format(time.RFC3339),
		}

		signature := ""
		signingKeyID := ""
		if keyErr == nil {
			sig, err := ver.SignPostMergeVerificationRecord(rec, cpKey.ID, cpKey.Priv)
			if err != nil {
				return nil, err
			}
			rec.RecordSignature = sig
			signature = sig.Sig
			signingKeyID = cpKey.ID
		}

		hash, err := ver.PostMergeVerificationRecordHash(rec)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(rec)
		if err != nil {
			return nil, err
		}

		return &store.ExecutionRecord{
			RecordID:        rec.RecordID,
			OrgID:           rec.OrgID,
			JobID:           rec.JobID,
			Ver:             rec.Ver,
			PrevRecordHash:  rec.PrevRecordHash,
			RecordHash:      hash,
			RecordSignature: signature,
			SigningKeyID:    signingKeyID,
			Body:            body,
			CreatedAt:       time.Now(),
		}, nil
	})
	if err != nil {
		log.Printf("[postmerge] append verdict record for job %s: %v", mon.JobID, err)
	}
}

// notifyMonitorVerdict posts the verdict to the PR thread. Best-effort, same
// as the existing comment posting in pr_comment_trigger.go — a failed GitHub
// API call must not roll back the already-committed verdict.
func (s *Server) notifyMonitorVerdict(ctx context.Context, mon *store.PostMergeMonitor, verdict, evidence string) {
	token, ok := s.installationToken(ctx, mon.OrgID)
	if !ok {
		log.Printf("[postmerge] no github installation token for org %s", mon.OrgID)
		return
	}
	owner, repo, repoOK := strings.Cut(mon.Repo, "/")
	if !repoOK {
		log.Printf("[postmerge] malformed repo %q on monitor %s", mon.Repo, mon.ID)
		return
	}
	body := "**Post-Merge Verification: " + verdict + "**\n\n" + evidence
	if err := createIssueComment(ctx, githubAPIDefault, token, owner, repo, mon.PRNumber, body); err != nil {
		log.Printf("[postmerge] post verdict comment on %s#%d: %v", mon.Repo, mon.PRNumber, err)
	}
}

// notifySlackVerdict is best-effort, same contract as notifyMonitorVerdict:
// a failed webhook post must never roll back the already-committed verdict,
// so every failure is logged, not returned.
func (s *Server) notifySlackVerdict(ctx context.Context, mon *store.PostMergeMonitor, verdict, evidence string) {
	url, err := s.storage.GetCredentialPlaintext(ctx, mon.OrgID, "SLACK_WEBHOOK_URL")
	if err != nil || url == "" {
		return // not configured — most orgs, no error
	}
	body, _ := json.Marshal(map[string]string{
		"text": "*Post-Merge Verification: " + verdict + "*\n" + mon.Repo + "#" + strconv.Itoa(mon.PRNumber) + "\n" + evidence,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[postmerge] build slack request for monitor %s: %v", mon.ID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := githubCallClient.Do(req) // reuse the existing shared HTTP client rather than allocate a new one
	if err != nil {
		log.Printf("[postmerge] post slack notification for monitor %s: %v", mon.ID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[postmerge] slack notification for monitor %s returned %d", mon.ID, resp.StatusCode)
	}
}

// submitRemediation opens a fix task via the same continuation path
// PR-comment fixes use, guarded exactly like handleCommentTrigger's "one
// continuation at a time" rule so a REGRESSION verdict never opens two.
func (s *Server) submitRemediation(ctx context.Context, mon *store.PostMergeMonitor, evidence string) {
	var parent store.QueuedTask
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND job_id = ?", mon.OrgID, mon.JobID).
		Order("created_at DESC, id DESC").
		First(&parent).Error; err != nil {
		log.Printf("[postmerge] find parent task for job %s: %v", mon.JobID, err)
		return
	}
	root := parent.RootTaskID
	if root == "" {
		root = parent.ID
	}
	if active, err := s.storage.ActiveTaskInThread(ctx, mon.OrgID, root); err == nil && active != nil {
		log.Printf("[postmerge] skipping remediation for monitor %s: a task is already active in thread %s", mon.ID, root)
		return
	}

	// The session, when there is one — same pattern as handleCommentTrigger
	// (pr_comment_trigger.go). Without this the remediation run starts from
	// round zero with no Architect history, discarding exactly the context
	// this feature is supposed to preserve.
	sessionID := ""
	if sess, err := s.storage.GetAgentSessionByTask(ctx, mon.OrgID, parent.ID); err == nil && sess != nil {
		sessionID = sess.ID
	}

	// Synthetic non-positive comment id: real GitHub comment ids are always
	// positive, so this can never collide with one. Masking to 63 bits before
	// negating matters — without it, a hash with its top bit set would
	// silently reinterpret as a positive int64 on negation (two's complement),
	// which would defeat the whole point. A collision between two monitors'
	// synthetic ids is a ~1-in-2^63 event; TriggerCommentID's unique index
	// turns that into a clean create error, not silent corruption, and this
	// whole call is already best-effort/logged.
	syntheticCommentID := -int64(fnv64a(mon.ID) & 0x7FFFFFFFFFFFFFFF)

	task, err := s.planner.SubmitContinuation(ctx, planner.ContinuationInput{
		OrgID:       mon.OrgID,
		ParentTask:  &parent,
		Instruction: "Production regression detected after this change merged: " + evidence + ". Investigate and fix.",
		CommentID:   syntheticCommentID,
		Origin:      store.OriginPostMergeRemediation,
		SessionID:   sessionID,
	})
	if err != nil {
		log.Printf("[postmerge] submit remediation for monitor %s: %v", mon.ID, err)
		return
	}
	if err := s.storage.SetMonitorRemediationTaskID(ctx, mon.ID, task.ID); err != nil {
		log.Printf("[postmerge] record remediation task id for monitor %s: %v", mon.ID, err)
	}
}

// fnv64a derives a synthetic negative comment id from a monitor id, via the
// standard library's FNV-1a rather than a hand-rolled one. Collision
// resistance, not cryptographic strength, is what matters here.
func fnv64a(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// FinalizePastWindowMonitors finalizes every MONITORING monitor whose window
// has elapsed as VERIFIED — no bad signal (revert, failed check run) arrived
// in time. Called on a periodic ticker from ee/cmd/kiwid/main.go, the same
// pattern as the existing 30s RequeueExpiredLeases/ExpireStaleQueuedTasks
// ticker.
func (s *Server) FinalizePastWindowMonitors(ctx context.Context) {
	monitors, err := s.storage.ListMonitorsPastWindow(ctx, time.Now())
	if err != nil {
		log.Printf("[postmerge] list monitors past window: %v", err)
		return
	}
	for i := range monitors {
		s.finalizeMonitor(ctx, &monitors[i], store.MonitorStatusVerified, "24h window elapsed with no regression signal")
	}
}
