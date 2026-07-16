package daemon

import (
	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

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
	// EncryptedCreds carries credentials sealed to the daemon's X25519 public key,
	// decrypted in-memory by the daemon (seal/open path tracked as a follow-up issue).
	EncryptedCreds string `json:"encrypted_creds,omitempty"`
}
