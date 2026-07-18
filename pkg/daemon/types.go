package daemon

import (
	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

// RegisterReq is the one-time join handshake. The daemon presents a join token
// (delivered out of band) plus both public keys; the body is signed with the
// Ed25519 private key, proving the daemon holds the identity it claims. The
// Control Plane binds this identity to the token's org.
type RegisterReq struct {
	// JoinToken is the short-lived, single-use, org-bound registration secret.
	JoinToken string `json:"join_token"`
	// PubKey is the base64-encoded X25519 public key credentials are sealed to.
	PubKey string `json:"pub_key"`
	// SignPubKey is the base64-encoded Ed25519 identity public key.
	SignPubKey string `json:"sign_pub_key"`
}

// HeartbeatReq is the payload sent by the daemon to the Control Plane
// to poll for new tasks. The request is authenticated by an Ed25519 signature
// over the marshaled body, sent in the X-Kiwi-Signature header and verifiable
// against SignPubKey.
type HeartbeatReq struct {
	// PubKey is the base64-encoded X25519 public key used to seal credentials to this daemon.
	PubKey string `json:"pub_key"`
	// SignPubKey is the base64-encoded Ed25519 identity public key used to verify X-Kiwi-Signature.
	SignPubKey string `json:"sign_pub_key,omitempty"`
	// Timestamp is the unix time (seconds) the request was created; it is signed to bound replay windows.
	Timestamp int64 `json:"timestamp,omitempty"`
}

// HeartbeatRes is the payload received from the Control Plane if tasks are available.
type HeartbeatRes struct {
	// Spec defines the worker tasks to execute (equivalent to worker-spec.json).
	Specs []agent.WorkerSpec `json:"specs"`
	// LeaseID is the fencing token for the leased task. The daemon must present
	// it when reporting the result, so a stale daemon whose lease has since been
	// reassigned cannot complete a task it no longer owns. The Control Plane
	// leases one task per heartbeat, so a single token covers Specs.
	LeaseID string `json:"lease_id,omitempty"`
	// EncryptedCreds carries the org's credentials sealed to the daemon's X25519
	// public key, opened in-memory by the daemon via crypto.OpenSealed.
	EncryptedCreds string `json:"encrypted_creds,omitempty"`
}

// ResultReq reports a task's terminal outcome back to the Control Plane, closing
// the lease. It is signed like every other daemon request.
type ResultReq struct {
	TaskID string `json:"task_id"`
	// LeaseID is the fencing token from the heartbeat that handed out this task.
	LeaseID string `json:"lease_id"`
	// Status is the terminal status: "SUCCEEDED" or "FAILED".
	Status string `json:"status"`
	// SignPubKey identifies the reporting daemon (verified against X-Kiwi-Signature).
	SignPubKey string `json:"sign_pub_key"`
	ResultURL  string `json:"result_url,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// RenewReq extends a task's lease while it is still running.
type RenewReq struct {
	TaskID     string `json:"task_id"`
	LeaseID    string `json:"lease_id"`
	SignPubKey string `json:"sign_pub_key"`
}
