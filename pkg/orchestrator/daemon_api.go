package orchestrator

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/store"
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

	task, err := s.storage.LeaseNextTask(r.Context(), d.OrgID, d.ID, leaseTTL)
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
			if _, cerr := s.storage.CompleteTask(r.Context(), task.ID, *task.LeaseID, store.TaskFailed); cerr != nil {
				log.Printf("[daemon] failing unusable task %s: %v", task.ID, cerr)
			}
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
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
	sealed, err := s.storage.SealCredentialsForDaemon(r.Context(), d.OrgID, encPub)
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
	if _, err := s.storage.GetDaemonBySignPubKey(r.Context(), req.SignPubKey); err != nil {
		if errors.Is(err, store.ErrDaemonNotFound) {
			http.Error(w, "daemon not registered", http.StatusForbidden)
			return
		}
		log.Printf("[daemon] result lookup failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ok, err := s.storage.CompleteTask(r.Context(), req.TaskID, req.LeaseID, req.Status)
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

	log.Printf("[daemon] task %s reported %s", req.TaskID, req.Status)
	w.WriteHeader(http.StatusNoContent)
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

	token, err := s.storage.CreateDaemonJoinToken(r.Context(), claims.OrgID, joinTokenTTL)
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
