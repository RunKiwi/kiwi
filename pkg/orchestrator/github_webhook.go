package orchestrator

import (
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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

type githubWebhookPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		HTMLURL        string `json:"html_url"`
		Merged         bool   `json:"merged"`
		MergedAt       string `json:"merged_at"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		MergedBy       struct {
			Login string `json:"login"`
		} `json:"merged_by"`
	} `json:"pull_request"`
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
	case err == nil, errors.Is(err, store.ErrRecordExists):
		// A concurrent redelivery losing the race is success, not an error.
		w.WriteHeader(http.StatusOK)
	default:
		log.Printf("[webhook] append merge record for job %s: %v", jobID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
