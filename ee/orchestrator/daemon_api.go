// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/ee/entitlement"
	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// The daemon API is the Data Plane <-> Control Plane seam (issue #115).
//
// It is mounted OUTSIDE auth.AuthMiddleware: a daemon has no org API key. It
// authenticates by signing the exact request body with its Ed25519 identity
// key, and the Control Plane verifies that signature against the public key
// registered for it. That registered row is the only thing that resolves a
// request to an org — org_id is never read from a request body.

const (
	// maxDaemonBody bounds request bodies. Signatures are computed over the raw
	// bytes, so the read must be bounded before anything else touches it.
	maxDaemonBody = 1 << 20 // 1 MiB

	// heartbeatSkew is how far a heartbeat's timestamp may sit from our clock.
	// It bounds the replay window: a captured heartbeat is only reusable inside
	// it. Generous enough to tolerate real clock drift on customer VMs.
	heartbeatSkew = 5 * time.Minute

	// leaseTTL is how long a daemon owns a task before the lease lapses and the
	// task returns to the queue. Sized for an agent run; renewal is issue #115's
	// follow-up (RenewLease is already implemented in the store).
	leaseTTL = 10 * time.Minute

	// joinTokenTTL bounds how long a freshly-minted join token is usable.
	joinTokenTTL = time.Hour
)

// DaemonRegisterReq is the first handshake: a daemon presents a join token and
// its two public keys. The body is signed with the Ed25519 private key, which
// proves the daemon actually holds the identity it is claiming.
type DaemonRegisterReq struct {
	JoinToken  string `json:"join_token"`
	PubKey     string `json:"pub_key"`      // base64 X25519 (seal target)
	SignPubKey string `json:"sign_pub_key"` // base64 Ed25519 (identity)
}

// DaemonRegisterRes returns the assigned daemon id. The org is deliberately not
// echoed back — the daemon has no need to know it, and it is not a claim the
// daemon gets to make.
type DaemonRegisterRes struct {
	DaemonID string `json:"daemon_id"`
}

// readSignedBody reads a bounded body and verifies the X-Kiwi-Signature header
// over the exact bytes received, using the Ed25519 key claimed in that body.
//
// Verifying against the *claimed* key only proves internal consistency — that
// the sender holds the private key for the key they named. It does NOT prove
// they are a registered daemon. The caller must still resolve that key to a
// registered row; that lookup is what establishes org and authorization.
func readSignedBody(r *http.Request, claimedSignPubKey func([]byte) (string, error)) ([]byte, ed25519.PublicKey, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxDaemonBody))
	if err != nil {
		return nil, nil, errors.New("cannot read body")
	}

	sigB64 := r.Header.Get("X-Kiwi-Signature")
	if sigB64 == "" {
		return nil, nil, errors.New("missing X-Kiwi-Signature")
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, nil, errors.New("malformed X-Kiwi-Signature")
	}

	keyB64, err := claimedSignPubKey(raw)
	if err != nil {
		return nil, nil, err
	}
	keyBytes, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil || len(keyBytes) != ed25519.PublicKeySize {
		return nil, nil, errors.New("malformed sign_pub_key")
	}
	pub := ed25519.PublicKey(keyBytes)

	if !crypto.Verify(pub, raw, sig) {
		return nil, nil, errors.New("signature verification failed")
	}
	return raw, pub, nil
}

// handleDaemonRegister serves POST /api/v1/daemon/register.
//
// Redeems a single-use join token and binds the daemon's identity to that
// token's org. The org comes only from the token — never from the body.
func (s *Server) handleDaemonRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DaemonRegisterReq
	raw, _, err := readSignedBody(r, func(b []byte) (string, error) {
		if err := json.Unmarshal(b, &req); err != nil {
			return "", errors.New("invalid request body")
		}
		return req.SignPubKey, nil
	})
	if err != nil {
		// Unauthenticated caller: do not disclose which check failed.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = raw

	if req.JoinToken == "" || req.PubKey == "" || req.SignPubKey == "" {
		http.Error(w, "join_token, pub_key and sign_pub_key are required", http.StatusBadRequest)
		return
	}
	// Reject a malformed seal target now rather than sealing to garbage later.
	if _, err := decodeX25519(req.PubKey); err != nil {
		http.Error(w, "malformed pub_key", http.StatusBadRequest)
		return
	}

	d, err := s.storage.RegisterDaemon(r.Context(), req.JoinToken, req.SignPubKey, req.PubKey)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrJoinTokenInvalid),
			errors.Is(err, store.ErrJoinTokenExpired),
			errors.Is(err, store.ErrJoinTokenUsed):
			// One generic answer for every token failure: an attacker probing
			// the endpoint learns nothing about which tokens exist.
			http.Error(w, "invalid join token", http.StatusForbidden)
		default:
			log.Printf("[daemon] register failed: %v", err)
			http.Error(w, "registration failed", http.StatusInternalServerError)
		}
		return
	}

	log.Printf("[daemon] registered %s for org %s", d.ID, d.OrgID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(DaemonRegisterRes{DaemonID: d.ID})
}

// handleDaemonHeartbeat serves POST /api/v1/daemon/heartbeat.
//
// This is the seam. In order: verify the signature over the raw body, resolve
// the identity to a registered daemon (and thus an org), lease one task for
// that org, seal the org's credentials to the daemon's registered X25519 key,
// and return both. 204 when there is no work.
func (s *Server) handleDaemonHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req daemon.HeartbeatReq
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

	// Bound replay. The timestamp is inside the signed body, so it cannot be
	// altered without invalidating the signature.
	if req.Timestamp != 0 {
		age := time.Since(time.Unix(req.Timestamp, 0))
		if age > heartbeatSkew || age < -heartbeatSkew {
			http.Error(w, "stale heartbeat timestamp", http.StatusUnauthorized)
			return
		}
	}

	// The signature proved possession of the claimed key. This lookup is what
	// proves the key is one we know, and yields the org.
	d, err := s.storage.GetDaemonBySignPubKey(r.Context(), req.SignPubKey)
	if err != nil {
		if errors.Is(err, store.ErrDaemonNotFound) {
			http.Error(w, "daemon not registered", http.StatusForbidden)
			return
		}
		log.Printf("[daemon] lookup failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// A daemon that rotated its seal key without re-registering would otherwise
	// get credentials it cannot open — a silent failure. Fail loudly instead.
	if req.PubKey != "" && req.PubKey != d.EncPubKey {
		http.Error(w, "daemon encryption key does not match registration; re-register", http.StatusConflict)
		return
	}

	if err := s.storage.TouchDaemon(r.Context(), d.ID); err != nil {
		// Liveness is not worth failing a heartbeat over.
		log.Printf("[daemon] touch %s: %v", d.ID, err)
	}

	task, err := s.storage.LeaseNextTask(r.Context(), d.OrgID, d.ID, d.FleetID, leaseTTL)
	if err != nil {
		log.Printf("[daemon] lease for org %s: %v", d.OrgID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if task == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	spec, err := specFromQueuedTask(task)
	if err != nil {
		log.Printf("[daemon] task %s has an unusable spec: %v", task.ID, err)
		// Do not strand the lease on a spec we cannot parse: fail it now so it
		// does not sit LEASED until expiry and then retry to the same end.
		if task.LeaseID != nil {
			if _, cerr := s.storage.CompleteTask(r.Context(), store.TaskCompletion{
				TaskID:      task.ID,
				LeaseID:     *task.LeaseID,
				FinalStatus: store.TaskFailed,
				Detail:      "dead-lettered due to daemon disconnect",
			}); cerr != nil {
				log.Printf("[daemon] failing unusable task %s: %v", task.ID, cerr)
			}
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var limits store.OrgLimits
	if err := s.db.First(&limits, "org_id = ?", d.OrgID).Error; err == nil {
		spec.TimeoutSeconds = limits.TaskTimeoutSeconds
	} else {
		spec.TimeoutSeconds = 1800 // Fallback
	}

	// Seal to the REGISTERED key, not one from the body: the body is signed, but
	// binding delivery to the registered row keeps rotation an explicit,
	// join-token-gated act rather than something a heartbeat can do silently.
	encPub, err := decodeX25519(d.EncPubKey)
	if err != nil {
		log.Printf("[daemon] daemon %s has a malformed enc_pub_key: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// The task and its model are known here, so the platform key can be scoped
	// to the one provider this task needs rather than bundled unconditionally.
	// Both of session mode's models, because each may be Kiwi-funded on a
	// different provider. specArchitectModel is empty outside session mode and
	// for a session that reuses the worker's model, which costs one no-op.
	extra := s.platformCredsFor(r.Context(), d, specModel(spec), specArchitectModel(spec))
	sealed, err := s.storage.SealCredentialsForDaemon(r.Context(), d.OrgID, encPub, extra)
	if err != nil {
		log.Printf("[daemon] sealing credentials for org %s: %v", d.OrgID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	res := daemon.HeartbeatRes{
		Specs:          []agent.WorkerSpec{spec},
		EncryptedCreds: sealed,
	}
	if task.LeaseID != nil {
		res.LeaseID = *task.LeaseID
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

// handleDaemonRenew serves POST /api/v1/daemon/renew.
func (s *Server) handleDaemonRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req daemon.RenewReq
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

	if req.TaskID == "" || req.LeaseID == "" {
		http.Error(w, "task_id and lease_id required", http.StatusBadRequest)
		return
	}

	// Prove the daemon is known.
	d, err := s.storage.GetDaemonBySignPubKey(r.Context(), req.SignPubKey)
	if err != nil {
		if errors.Is(err, store.ErrDaemonNotFound) {
			http.Error(w, "daemon not registered", http.StatusForbidden)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// A renewal is proof of life, and it used not to count as one.
	//
	// The daemon runs a task synchronously on its poll goroutine, so it sends no
	// heartbeat for the whole run — and the heartbeat was the only thing that
	// touched this row. A daemon working hard therefore went stale after
	// daemonStaleAfter and was reported as offline: "no runner is connected"
	// on a queued sibling, and an offline badge in the fleet UI, both while it
	// was in fact busy doing exactly what was asked.
	//
	// Best-effort: liveness bookkeeping must never fail a renewal, because
	// losing a renewal loses the task.
	if err := s.storage.TouchDaemon(r.Context(), d.ID); err != nil {
		log.Printf("[daemon] touch on renew for %s: %v", d.ID, err)
	}

	ok, err := s.storage.RenewLease(r.Context(), req.TaskID, req.LeaseID, leaseTTL)
	if err != nil {
		log.Printf("[daemon] renew %s failed: %v", req.TaskID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		// HTTP 409 Conflict if lease was lost
		http.Error(w, "lease lost", http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDaemonResult serves POST /api/v1/daemon/result. A daemon reports a
// task's terminal outcome, presenting the lease fencing token from the
// heartbeat. This is what closes the lease — without it a leased task would sit
// until expiry and then be retried even on success.
func (s *Server) handleDaemonResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req daemon.ResultReq
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

	if req.TaskID == "" || req.LeaseID == "" {
		http.Error(w, "task_id and lease_id are required", http.StatusBadRequest)
		return
	}
	// Only terminal statuses may be reported.
	if req.Status != store.TaskSucceeded && req.Status != store.TaskFailed {
		http.Error(w, "status must be SUCCEEDED or FAILED", http.StatusBadRequest)
		return
	}

	// The daemon must be registered; the lease id (a fencing token) does the
	// real authorization inside CompleteTask, but resolving the identity first
	// keeps unregistered callers off the endpoint entirely.
	d, err := s.storage.GetDaemonBySignPubKey(r.Context(), req.SignPubKey)
	if err != nil {
		if errors.Is(err, store.ErrDaemonNotFound) {
			http.Error(w, "daemon not registered", http.StatusForbidden)
			return
		}
		log.Printf("[daemon] result lookup failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if req.Abuse {
		// Don't suspend on a single strike — the daemon's heuristic also fires for
		// a legitimately slow test suite. Accumulate strikes in a rolling window
		// and suspend only past the threshold.
		suspended, strikes, err := auth.RecordAbuseStrike(s.db, d.OrgID, 0, 0)
		switch {
		case err != nil:
			log.Printf("[daemon] record abuse strike for org %s: %v", d.OrgID, err)
		case suspended:
			log.Printf("[daemon] auto-suspended org %s after %d abuse strikes", d.OrgID, strikes)
		default:
			log.Printf("[daemon] abuse strike %d recorded for org %s (below suspend threshold)", strikes, d.OrgID)
		}
	}

	var costUSD float64
	var tokensIn, tokensOut int64
	// Session mode runs two models, which may sit in different price tiers, so
	// their tokens are counted apart as well as together. The daemon already
	// tags the Architect's calls as the "critic" phase (see sessionPhase), so
	// the split needs no extra wire field — charging a session's whole usage to
	// one tier would drain a small frontier allowance with cheap implementer
	// work, or bill frontier work at economy rates.
	var architectIn, architectOut int64
	for _, ev := range req.Events {
		costUSD += ev.CostUSD
		tokensIn += ev.InputTokens
		tokensOut += ev.OutputTokens
		if ev.Phase == "critic" {
			architectIn += ev.InputTokens
			architectOut += ev.OutputTokens
		}
	}

	ok, err := s.storage.CompleteTask(r.Context(), store.TaskCompletion{
		TaskID:      req.TaskID,
		LeaseID:     req.LeaseID,
		FinalStatus: req.Status,
		ResultURL:   req.ResultURL,
		Detail:      req.Detail,
		CostUSD:     costUSD,
		TokensIn:    tokensIn,
		TokensOut:   tokensOut,
	})
	if err != nil {
		log.Printf("[daemon] complete task %s: %v", req.TaskID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		// Stale fencing token: the lease was reassigned (e.g. this daemon stalled
		// past expiry and another picked the task up). The report is rejected so
		// a zombie cannot overwrite the winner's outcome.
		http.Error(w, "lease no longer valid", http.StatusConflict)
		return
	}

	var task store.QueuedTask
	if err := s.db.WithContext(r.Context()).Where("id = ?", req.TaskID).First(&task).Error; err == nil {
		s.meterKiwiUsage(r.Context(), &task, tokensIn, tokensOut, architectIn, architectOut)
	}

	log.Printf("[daemon] task %s reported %s", req.TaskID, req.Status)

	// Persist the loop telemetry the daemon observed, and verify the daemon's
	// attestation over it before storing anything derived from it. A bad
	// signature means the events are not provably from this daemon, so they are
	// recorded without the attestation rather than treated as attested.
	execSig := req.ExecSignature
	if execSig != nil {
		pub, err := base64.StdEncoding.DecodeString(d.SignPubKey)
		if err != nil || ver.VerifyExecution(ed25519.PublicKey(pub), execSig, req.TaskID, req.Status, req.Events) != nil {
			log.Printf("[daemon] task %s: execution attestation did not verify; recording unattested", req.TaskID)
			execSig = nil
		}
	}
	s.replaceTaskEvents(r.Context(), d.OrgID, req.TaskID, req.Events)

	w.WriteHeader(http.StatusNoContent)

	// Assembly is a side-effect of reporting, not part of it: the result is
	// already committed and must not be rolled back if provenance fails. Run it
	// detached, with its own timeout so a slow query cannot leak a goroutine.
	ec := execContext{
		FleetID:        d.FleetID,
		DaemonID:       d.ID,
		DaemonPubKey:   decodeDaemonPubKey(d.SignPubKey),
		SandboxRuntime: req.SandboxRuntime,
		Mode:           s.executionMode(),
		ExecSignature:  execSig,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.maybeAssembleRecord(ctx, d.OrgID, req.TaskID, ec)
	}()
}

// executionMode reports whether this Control Plane operates the data plane
// ("managed") or the customer does ("byoc"). It is recorded per job because it
// is exactly the distinction that decides whether zero-knowledge holds.
func (s *Server) executionMode() string {
	if os.Getenv("KIWI_EXECUTION_MODE") != "" {
		return os.Getenv("KIWI_EXECUTION_MODE")
	}
	if os.Getenv("KIWI_PROVISIONER") != "" {
		return "managed"
	}
	return "byoc"
}

// meterKiwiUsage records the token usage against the org's allowance if the
// work was performed on a Kiwi-owned platform key.
// architectIn/architectOut are the subset of the totals spent by session mode's
// Architect, which may sit in a different price tier from the Implementer. They
// are zero when a task ran both roles on one model.
func (s *Server) meterKiwiUsage(ctx context.Context, task *store.QueuedTask, tokensIn, tokensOut, architectIn, architectOut int64) {
	if task == nil || task.Funding != store.FundingKiwi || (tokensIn == 0 && tokensOut == 0) {
		return
	}

	// Charge the Architect against its own tier first, then whatever is left
	// against the Implementer's. Both roles are Kiwi-funded whenever the task
	// is: admission refuses a session whose two models have different payers,
	// precisely so this single Funding field means something.
	if archTier, ok := task.Spec["architect_tier"].(string); ok && archTier != "" && architectIn+architectOut > 0 {
		s.consumeTier(ctx, task, archTier, architectIn+architectOut)
		tokensIn -= architectIn
		tokensOut -= architectOut
		if tokensIn < 0 {
			tokensIn = 0
		}
		if tokensOut < 0 {
			tokensOut = 0
		}
		if tokensIn+tokensOut == 0 {
			return
		}
	}

	// The tier is read from the spec, where the planner pinned it at submit,
	// rather than resolved again here.
	//
	// Re-resolving would charge whatever the catalog says *now*: a refresh that
	// reprices a model between lease and result flips KiwiProvided to false and
	// the usage goes uncharged, or moves it to another tier and the wrong bucket
	// is debited. The key was handed out against the tier admitted at submit, so
	// that is the tier that must pay for it.
	tier, _ := task.Spec["tier"].(string)
	if tier == "" {
		// A task queued before the tier was pinned. Fall back to the catalog and
		// say so — a silent skip here is a free run on Kiwi's key.
		res, err := s.storage.ResolveModel(ctx, task.OrgID, specString(task.Spec, "model"))
		if err != nil {
			log.Printf("[daemon] meter %s: no pinned tier and catalog lookup failed, %d tokens go uncharged: %v",
				task.ID, tokensIn+tokensOut, err)
			return
		}
		if !res.KiwiProvided {
			log.Printf("[daemon] meter %s: funding=kiwi but model %q is not Kiwi-provided; %d tokens go uncharged",
				task.ID, specString(task.Spec, "model"), tokensIn+tokensOut)
			return
		}
		tier = res.Tier
	}

	s.consumeTier(ctx, task, tier, tokensIn+tokensOut)
}

// consumeTier draws tokens down against one tier's allowance, logging rather
// than returning: the task did finish, and failing the result report would
// strand a completed run.
func (s *Server) consumeTier(ctx context.Context, task *store.QueuedTask, tier string, tokens int64) {
	if tier == "" || tokens <= 0 {
		return
	}
	plan, err := s.storage.GetOrgPlan(ctx, task.OrgID)
	if err != nil {
		log.Printf("[daemon] meter %s: failed to get plan for org %s: %v", task.ID, task.OrgID, err)
		return
	}
	checker := &entitlement.Checker{Store: s.storage}
	if err := checker.Consume(ctx, task.OrgID, plan, tier, tokens); err != nil {
		log.Printf("[daemon] meter %s: failed to draw down %s allowance: %v", task.ID, tier, err)
	}
}

// decodeX25519 turns the base64(raw 32-byte) wire form of an X25519 public key
// into the *ecdh.PublicKey that SealCredentialsForDaemon needs.
func decodeX25519(b64 string) (*ecdh.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	return crypto.PublicKeyFromRawBytes(raw)
}

// handleDaemonJoinToken serves POST /api/v1/daemon/join-token.
//
// This one IS behind AuthMiddleware — an org admin mints a join token for their
// own org, then hands it to the daemon operator out of band (Terraform output
// for BYOC, internal provisioning for managed). The plaintext is returned once
// and never persisted.
func (s *Server) handleDaemonJoinToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// fleet_id is optional: omit it to mint a token for the org's unassigned
	// pool. A body is not required, so an empty/absent one is fine.
	var req struct {
		FleetID string `json:"fleet_id"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// A token may only target a fleet the caller's org owns — otherwise a daemon
	// could be routed into a fleet the org cannot see.
	if req.FleetID != "" {
		fleets, err := s.storage.ListFleets(r.Context(), claims.OrgID)
		if err != nil {
			log.Printf("[daemon] list fleets for org %s: %v", claims.OrgID, err)
			http.Error(w, "could not create join token", http.StatusInternalServerError)
			return
		}
		owned := false
		for _, f := range fleets {
			if f.ID == req.FleetID {
				owned = true
				break
			}
		}
		if !owned {
			http.Error(w, "unknown fleet", http.StatusBadRequest)
			return
		}
	}

	token, err := s.storage.CreateDaemonJoinToken(r.Context(), claims.OrgID, req.FleetID, joinTokenTTL)
	if err != nil {
		log.Printf("[daemon] mint join token for org %s: %v", claims.OrgID, err)
		http.Error(w, "could not create join token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"join_token": token,
		"expires_in": int(joinTokenTTL.Seconds()),
		"fleet_id":   req.FleetID,
	})
}

// specFromQueuedTask converts a stored spec map into the WorkerSpec the daemon
// executes. The queue stores specs as JSON, so this round-trips rather than
// hand-mapping fields — the planner and the daemon then agree by construction.
func specFromQueuedTask(task *store.QueuedTask) (agent.WorkerSpec, error) {
	var spec agent.WorkerSpec
	b, err := json.Marshal(task.Spec)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		return spec, err
	}
	// The queue row is authoritative for identity: a spec that disagreed with
	// its own task id would break lease completion.
	spec.ID = task.ID
	if spec.Task == "" {
		return spec, errors.New("spec has no task")
	}
	return spec, nil
}

// specModel reads the model a worker spec will run, which decides whether a
// Kiwi-owned key is in scope for this lease.
func specArchitectModel(spec agent.WorkerSpec) string {
	return spec.ArchitectModel
}

func specModel(spec agent.WorkerSpec) string {
	return spec.Model
}

// DaemonResponse represents a single daemon in the /api/v1/daemons list response.
// Identity material (keys) is omitted because the UI does not need it.
type DaemonResponse struct {
	ID         string     `json:"id"`
	FleetID    string     `json:"fleet_id"`
	Online     bool       `json:"online"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// handleDaemonsList serves GET /api/v1/daemons.
// Returns the org's registered daemons and computes their liveness.
func (s *Server) handleDaemonsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	daemons, err := s.storage.ListDaemons(r.Context(), claims.OrgID)
	if err != nil {
		log.Printf("[daemon] list daemons for org %s: %v", claims.OrgID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var resp []DaemonResponse
	now := time.Now()
	for _, d := range daemons {
		online := false
		if d.LastSeenAt != nil && now.Sub(*d.LastSeenAt) < 30*time.Second {
			online = true
		}
		resp = append(resp, DaemonResponse{
			ID:         d.ID,
			FleetID:    d.FleetID,
			Online:     online,
			LastSeenAt: d.LastSeenAt,
			CreatedAt:  d.CreatedAt,
		})
	}
	if resp == nil {
		resp = []DaemonResponse{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDaemonProgress serves POST /api/v1/daemon/progress — partial telemetry
// for a task that is still running.
//
// The daemon is the only observer of the Actor–Critic loop, and until this
// existed it kept everything until the end: a task running for ten minutes
// showed a spinner, and one that was stuck looked exactly like one working.
//
// Everything here is best-effort and returns 204 even when nothing was stored.
// A daemon must never retry, back off, or fail a run because its telemetry did
// not land — the run is the product, this is the commentary.
func (s *Server) handleDaemonProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req daemon.ProgressReq
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
	if req.TaskID == "" || req.LeaseID == "" {
		http.Error(w, "task_id and lease_id are required", http.StatusBadRequest)
		return
	}

	// Resolve the org from the signing identity rather than trusting anything in
	// the body: task_events rows are org-scoped, and an unregistered or spoofed
	// caller must not be able to write into another tenant's telemetry.
	d, err := s.storage.GetDaemonBySignPubKey(r.Context(), req.SignPubKey)
	if err != nil {
		if errors.Is(err, store.ErrDaemonNotFound) {
			http.Error(w, "daemon not registered", http.StatusForbidden)
			return
		}
		log.Printf("[daemon] progress lookup failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Progress is the densest liveness signal there is — it arrives every few
	// seconds for the whole run, where a renewal arrives every couple of minutes
	// and a heartbeat not at all until the task ends. Touched before the fencing
	// check on purpose: a daemon reporting on a lease it has lost is still a
	// daemon that is alive and should not be advertised as offline.
	if err := s.storage.TouchDaemon(r.Context(), d.ID); err != nil {
		log.Printf("[daemon] touch on progress for %s: %v", d.ID, err)
	}

	// The fencing token decides whether this daemon still owns the task. A
	// reassigned lease means these events describe a run that has been
	// abandoned, so storing them would interleave two runs' phases in one
	// timeline. Nothing is written when it does not apply.
	applied, err := s.storage.RecordTaskProgress(r.Context(), req.TaskID, req.LeaseID, req.Phase, summarize(req.OutputTail, 4000))
	if err != nil {
		log.Printf("[daemon] record progress for task %s: %v", req.TaskID, err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !applied {
		// Stale lease or a task that already finished. 204 rather than 409: the
		// daemon has nothing useful to do with the distinction, and the run it is
		// finishing will be rejected by CompleteTask on the same grounds.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Provisional. ReportResult sends the authoritative list at the end, which
	// replaces these — see replaceTaskEvents.
	s.recordTaskEvents(r.Context(), d.OrgID, req.TaskID, req.Events)

	w.WriteHeader(http.StatusNoContent)
}
