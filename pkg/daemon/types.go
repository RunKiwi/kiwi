package daemon

import (
	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

// HeartbeatReq is the payload sent by the daemon to the Control Plane
// to poll for new tasks.
type HeartbeatReq struct {
	// PubKey is the base64-encoded Ed25519 public key of the daemon.
	PubKey string `json:"pub_key"`
}

// HeartbeatRes is the payload received from the Control Plane if tasks are available.
type HeartbeatRes struct {
	// Spec defines the worker tasks to execute (equivalent to worker-spec.json).
	Specs []agent.WorkerSpec `json:"specs"`
	// EncryptedCreds contains zero-knowledge encrypted credentials (for future issues).
	EncryptedCreds string `json:"encrypted_creds,omitempty"`
}
