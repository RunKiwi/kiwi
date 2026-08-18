// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

type githubWebhookPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	PullRequest struct {
		Number         int    `json:"number"`
		HTMLURL        string `json:"html_url"`
		Title          string `json:"title"`
		Body           string `json:"body"`
		Merged         bool   `json:"merged"`
		MergedAt       string `json:"merged_at"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		MergedBy       struct {
			Login string `json:"login"`
		} `json:"merged_by"`
	} `json:"pull_request"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

func (s *Server) handleGithubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" {
		log.Println("[webhook] GITHUB_WEBHOOK_SECRET not set; rejecting webhook (fail closed)")
		http.Error(w, "Webhook not configured", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	signatureHeader := r.Header.Get("X-Hub-Signature-256")
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		http.Error(w, "Invalid signature format", http.StatusUnauthorized)
		return
	}
	signatureHex := strings.TrimPrefix(signatureHeader, "sha256=")
	signatureBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		http.Error(w, "Invalid signature hex", http.StatusUnauthorized)
		return
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)

	if subtle.ConstantTimeCompare(signatureBytes, expectedMAC) != 1 {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	event := r.Header.Get("X-GitHub-Event")

	// An App the customer removed must stop being offered as an auth path
	// straight away. Without this the row survives until a task tries to use it,
	// which routes that task away from the GIT_TOKEN fallback and into a failure
	// that arrives minutes later and blames the runner.
	if event == "installation" {
		var p struct {
			Action       string `json:"action"`
			Installation struct {
				ID int64 `json:"id"`
			} `json:"installation"`
		}
		if err := json.Unmarshal(body, &p); err == nil && p.Installation.ID != 0 {
			switch p.Action {
			case "deleted", "suspend":
				if err := s.storage.DeleteGitHubInstallation(r.Context(), p.Installation.ID); err != nil {
					log.Printf("[githubapp] removing installation %d after %q: %v", p.Installation.ID, p.Action, err)
				} else {
					log.Printf("[githubapp] installation %d removed after %q", p.Installation.ID, p.Action)
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// A review comment continues the task that opened the pull request. Handled
	// before the merge path below because the events are disjoint, and because
	// every rejection here must also be a 200.
	if trigger, ok := parseCommentEvent(event, body); ok {
		s.handleCommentTrigger(r, event, trigger)
		w.WriteHeader(http.StatusOK)
		return
	}

	if event == "check_run" {
		s.handleCheckRun(r.Context(), body)
		w.WriteHeader(http.StatusOK)
		return
	}

	if event != "pull_request" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload githubWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if payload.Action != "closed" || !payload.PullRequest.Merged {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Revert detection requires the reverting PR to be MERGED, not merely
	// opened — see checkForRevert's doc comment for why. A no-op unless the
	// body matches the revert pattern, so this never interferes with the
	// merge-record logic below.
	s.checkForRevert(r.Context(), payload)

	prURL := payload.PullRequest.HTMLURL

	// Resolve PR URL to its job via QueuedTask
	var qt store.QueuedTask
	if err := s.storage.DB().WithContext(r.Context()).
		Where("result_url = ?", prURL).First(&qt).Error; err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	jobID := qt.JobID
	if jobID == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	orgID := qt.OrgID

	// Already recorded this merge — GitHub redelivers, and a second record for
	// the same job would fork the chain. Org-scoped like every other lookup.
	if _, err := s.storage.GetExecutionRecordByVer(r.Context(), orgID, jobID, ver.MergeSchemaVersion); err == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	// The merge record points at the execution record it completes.
	originalRec, err := s.storage.GetExecutionRecord(r.Context(), orgID, jobID)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	// A deployment without a signing key still records the merge, unsigned, for
	// the same reason the execution record does: the approver is observable only
	// now, and refusing to store it would lose it permanently. Returning an
	// error here would 500 every delivery and make GitHub disable the hook.
	cpKey, keyErr := s.cpSigningKey()
	if keyErr != nil {
		log.Printf("[webhook] no signing key; recording merge for job %s unsigned: %v", jobID, keyErr)
	}

	_, err = s.storage.AppendExecutionRecord(r.Context(), orgID, jobID, ver.MergeSchemaVersion, func(prevHash string) (*store.ExecutionRecord, error) {
		mergeRec := &ver.MergeRecord{
			Ver:              ver.MergeSchemaVersion,
			RecordID:         "rec_" + uuid.New().String(),
			OriginalRecordID: originalRec.RecordID,
			OrgID:            orgID,
			JobID:            jobID,
			PrevRecordHash:   prevHash,
			Attestation:      ver.AttestationUnsigned,
			ApprovedBy:       "gh:" + payload.PullRequest.MergedBy.Login,
			MergedAt:         payload.PullRequest.MergedAt,
			MergeCommit:      payload.PullRequest.MergeCommitSHA,
		}

		signature := ""
		signingKeyID := ""
		if keyErr == nil {
			// SignMergeRecord marks the record signed as part of signing, since
			// Attestation is inside the signed payload.
			sig, err := ver.SignMergeRecord(mergeRec, cpKey.ID, cpKey.Priv)
			if err != nil {
				return nil, err
			}
			mergeRec.RecordSignature = sig
			signature = sig.Sig
			signingKeyID = cpKey.ID
		}

		// Hashed last, over the signing payload (signature excluded), in the same
		// "sha256:<hex>" form as an execution record — this hash becomes the next
		// record's prev_record_hash, so the chain must not mix formats.
		hash, err := ver.MergeRecordHash(mergeRec)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(mergeRec)
		if err != nil {
			return nil, err
		}

		return &store.ExecutionRecord{
			RecordID:        mergeRec.RecordID,
			OrgID:           mergeRec.OrgID,
			JobID:           mergeRec.JobID,
			Ver:             mergeRec.Ver,
			PrevRecordHash:  mergeRec.PrevRecordHash,
			RecordHash:      hash,
			Body:            body,
			ExecSignature:   "",
			RecordSignature: signature,
			SigningKeyID:    signingKeyID,
			CreatedAt:       time.Now(),
		}, nil
	})

	switch {
	case err == nil:
		s.createPostMergeMonitor(r.Context(), orgID, jobID, payload)
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, store.ErrRecordExists):
		// A concurrent redelivery losing the race is success, not an error.
		// Still attempt monitor creation: the delivery that wrote the record
		// may have failed at CreateMonitor (logged, not returned, so it never
		// surfaced here), and the unique (org_id, job_id) index makes a
		// repeat call safe — CreateMonitor's own error path just logs again.
		s.createPostMergeMonitor(r.Context(), orgID, jobID, payload)
		w.WriteHeader(http.StatusOK)
	default:
		log.Printf("[webhook] append merge record for job %s: %v", jobID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// createPostMergeMonitor starts Phase 1a monitoring for a freshly merged job.
// Best-effort: a failure here must not turn the merge-record append (already
// committed) into a 500, so errors are logged, not returned. Phase 1a treats
// merge as deploy — window_ends_at is always mergedAt + 24h, with no deploy-
// webhook override (out of scope for Phase 1a; see the plan's Global
// Constraints).
func (s *Server) createPostMergeMonitor(ctx context.Context, orgID, jobID string, payload githubWebhookPayload) {
	mergedAt, err := time.Parse(time.RFC3339, payload.PullRequest.MergedAt)
	if err != nil {
		log.Printf("[webhook] monitor for job %s: parse merged_at %q: %v", jobID, payload.PullRequest.MergedAt, err)
		return
	}
	repo := payload.Repository.Owner.Login + "/" + payload.Repository.Name
	mon := &store.PostMergeMonitor{
		ID:             "mon_" + uuid.New().String(),
		OrgID:          orgID,
		JobID:          jobID,
		Repo:           repo,
		PRNumber:       payload.PullRequest.Number,
		MergeCommitSHA: payload.PullRequest.MergeCommitSHA,
		Status:         store.MonitorStatusMonitoring,
		DeployedAt:     mergedAt,
		WindowEndsAt:   mergedAt.Add(24 * time.Hour),
		Origin:         store.MonitorOriginKiwiPR,
	}
	if err := s.storage.CreateMonitor(ctx, mon); err != nil {
		log.Printf("[webhook] create monitor for job %s: %v", jobID, err)
		return
	}
	s.enqueueTelemetryPolls(ctx, mon, postMergeMonitorIntent(ctx, s.storage, jobID, payload))
}

// postMergeMonitorIntent resolves the text metric selection should judge
// relevance against. payload.PullRequest.Title is available unconditionally
// but is a materially weaker signal than the task's actual stated Intent — a
// PR title is often generic ("Fix bug", "Update widgets.go") where the
// submitted task description says what the change is actually meant to do.
// ee/planner/service.go (and the two other Job-creation call sites,
// ee/orchestrator/webhook.go and the legacy submit path in server.go) all
// write the original task text into Job.Inputs["task"] at admission time, so
// GetJob(ctx, jobID) recovers it cheaply — jobID is already threaded through
// this whole call path, and store.Store.GetJob requires no new plumbing.
// Falls back to the PR title when no Job row is found (an unexpected state)
// or its Inputs carries no "task" key (e.g. a nil Inputs map on an older or
// hand-inserted row) so a monitor is never left with no intent at all.
func postMergeMonitorIntent(ctx context.Context, storage store.Store, jobID string, payload githubWebhookPayload) string {
	if job, err := storage.GetJob(ctx, jobID); err == nil && job != nil {
		if task, ok := job.Inputs["task"].(string); ok && task != "" {
			return task
		}
	}
	return payload.PullRequest.Title
}

// GetMonitorByMergeCommit does exact equality against a stored 40-character
// SHA, so a shorter capture could never match anything — git revert always
// writes the full 40-char SHA in the commit message this pattern matches
// anyway. Requiring exactly 40 hex characters just makes that explicit.
var revertCommitPattern = regexp.MustCompile(`This reverts commit ([0-9a-f]{40})\.`)

// revertsPRPattern matches GitHub's own Revert-button body. GitHub's
// documented auto-template is "Reverts $PRNUM" — a bare "#123" with no
// owner/repo prefix, since the button only ever reverts a PR in the same
// repository — but the owner/repo-qualified form is accepted too in case a
// human edits the generated body or a future GitHub version adds it back.
// Either way this names the reverted PR by number, not by SHA, so resolving
// it costs one GitHub API call — see resolveRevertedSHA.
var revertsPRPattern = regexp.MustCompile(`(?m)^Reverts (?:([\w.-]+)/([\w.-]+))?#(\d+)`)

// checkForRevert looks for either shape of GitHub's revert-PR body and, if it
// references the merge commit of an active monitor, finalizes that monitor as
// REGRESSION — a human reverting a Kiwi PR is the highest-confidence,
// zero-telemetry-cost regression signal there is.
//
// Requires the reverting PR to be MERGED, not merely opened. An opened PR
// needs no authorization at all — on a public repository, anyone can open one
// with a forged revert body and force a REGRESSION verdict, a signed record,
// and (with auto_remediate on) a billable run. Requiring a merge reuses the
// trust boundary the rest of this file already relies on for the original
// merge record: GitHub's merge action is the authorization, since merging
// requires real write access or passing branch protection, enforced by
// GitHub, not by Kiwi.
func (s *Server) checkForRevert(ctx context.Context, payload githubWebhookPayload) {
	// The org isn't known from this payload alone (unlike the merge path,
	// which resolves org via the QueuedTask.result_url lookup) — resolved
	// instead via the webhook's own "installation" field, which GitHub sends
	// on every App delivery. This is a hard tenant boundary rather than a
	// heuristic: the newest matching QueuedTask.result_url this file used to
	// fall back to could belong to a different org's PR against the same
	// public repo, misattributing the signal. An installation id is minted
	// once per (App, account) and cannot be spoofed by an unrelated org.
	inst, err := s.storage.GetGitHubInstallationByID(ctx, payload.Installation.ID)
	if err != nil {
		return // no installation on file for this delivery — nothing to resolve against
	}

	revertedSHA, ok := s.resolveRevertedSHA(ctx, payload, inst, githubAPIDefault)
	if !ok {
		return
	}

	mon, err := s.storage.GetMonitorByMergeCommit(ctx, inst.OrgID, revertedSHA)
	if err != nil {
		return // no active monitor for this commit — nothing to do
	}
	evidence := "reverted by " + payload.Repository.Owner.Login + "/" + payload.Repository.Name + "#" + strconv.Itoa(payload.PullRequest.Number)
	s.finalizeMonitor(ctx, mon, store.MonitorStatusRegression, evidence)
}

// resolveRevertedSHA extracts the merge commit SHA a revert-authorized PR is
// reverting. Two body shapes carry this signal: a manually authored `git
// revert` commit message embeds the SHA directly, no lookup needed; GitHub's
// own Revert button instead writes "Reverts #N" (see revertsPRPattern),
// which names the reverted PR rather than its SHA and must be resolved via
// the API — so it costs an installation token mint and one GitHub call the
// first shape avoids entirely.
//
// A qualified "owner/repo#N" body is required to match payload.Repository —
// the button only ever references the same repo, so an owner/repo pair that
// doesn't is either a stale/hand-edited body or a forged one, and either way
// resolving it against a different repository would be unverified behavior,
// not a signal to act on.
//
// Every failure past a body match is logged. A revert going undetected is
// exactly the kind of quiet failure this system exists to catch, and this
// path was designed once already (checkForRevert's original SHA-only form)
// without production verification — see the doc comment on revertsPRPattern.
//
// api is threaded through explicitly (rather than read from a package
// constant) so this can be exercised against an httptest server the same way
// the other GitHub-calling functions in this file are, without a live
// network call in every test run.
func (s *Server) resolveRevertedSHA(ctx context.Context, payload githubWebhookPayload, inst *store.GitHubInstallation, api string) (string, bool) {
	if m := revertCommitPattern.FindStringSubmatch(payload.PullRequest.Body); m != nil {
		return m[1], true
	}

	m := revertsPRPattern.FindStringSubmatch(payload.PullRequest.Body)
	if m == nil {
		return "", false
	}
	owner, repo, numStr := m[1], m[2], m[3]
	if owner != "" && (owner != payload.Repository.Owner.Login || repo != payload.Repository.Name) {
		log.Printf("[webhook] revert body references %s/%s, not the reverting PR's own repo %s/%s — ignoring",
			owner, repo, payload.Repository.Owner.Login, payload.Repository.Name)
		return "", false
	}
	owner, repo = payload.Repository.Owner.Login, payload.Repository.Name
	number, err := strconv.Atoi(numStr)
	if err != nil {
		return "", false
	}

	if s.githubApp == nil {
		log.Printf("[webhook] revert body references %s/%s#%d but no GitHub App is configured to resolve it", owner, repo, number)
		return "", false
	}
	tok, err := s.githubApp.InstallationToken(ctx, inst.InstallationID)
	if err != nil {
		log.Printf("[webhook] mint installation token to resolve revert of %s/%s#%d: %v", owner, repo, number, err)
		return "", false
	}
	sha, merged, err := getPullRequest(ctx, api, tok.Value, owner, repo, number)
	if err != nil {
		log.Printf("[webhook] resolve revert of %s/%s#%d: %v", owner, repo, number, err)
		return "", false
	}
	if !merged || sha == "" {
		log.Printf("[webhook] revert body references %s/%s#%d, which is not merged — ignoring", owner, repo, number)
		return "", false
	}
	return sha, true
}

type checkRunWebhookPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	CheckRun struct {
		HeadSHA    string `json:"head_sha"`
		Conclusion string `json:"conclusion"`
	} `json:"check_run"`
}

// handleCheckRun finalizes a monitor as REGRESSION the moment any check run
// on its merge commit fails. Coarse by design for Phase 1a: one failing
// check finalizes immediately rather than waiting to see if a retry
// succeeds, which trades a rare false positive (a genuinely flaky check) for
// not missing a real one — refining this is future work, not blocking here.
//
// "Failure" covers every conclusion GitHub's own vocabulary treats as a
// failing outcome, not just the literal "failure" value: "timed_out" and
// "action_required" are failing outcomes too, and missing them would let a
// stuck or blocked check silently fall through to VERIFIED.
func (s *Server) handleCheckRun(ctx context.Context, body []byte) {
	var payload checkRunWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}
	if payload.Action != "completed" {
		return
	}
	switch payload.CheckRun.Conclusion {
	case "failure", "timed_out", "action_required":
		// fall through to finalize
	default:
		return
	}

	// Org isn't in this payload either (like the revert-PR path) — resolved
	// the same way checkForRevert now does: via the webhook's own
	// "installation" field, a hard tenant boundary rather than a
	// same-repo-name heuristic. See checkForRevert's comment for why.
	inst, err := s.storage.GetGitHubInstallationByID(ctx, payload.Installation.ID)
	if err != nil {
		return // no installation on file for this delivery — nothing to resolve against
	}

	mon, err := s.storage.GetMonitorByMergeCommit(ctx, inst.OrgID, payload.CheckRun.HeadSHA)
	if err != nil {
		return // no active monitor for this commit — nothing to do
	}
	s.finalizeMonitor(ctx, mon, store.MonitorStatusRegression, "check run failed on "+payload.CheckRun.HeadSHA)
}
