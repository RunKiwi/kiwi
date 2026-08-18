package daemon

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// ErrLeaseLost reports that the Control Plane has taken a task away from this
// daemon — the user cancelled it, or the lease expired and was reassigned. It is
// returned only for an explicit 409, never for a transport failure, because the
// daemon abandons in-flight work on it: treating a network blip the same way
// would throw away a run that was going fine.
var ErrLeaseLost = errors.New("lease lost")

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
	Abuse      bool   `json:"abuse,omitempty"`
	// Events is the ordered Actor–Critic telemetry for this task. The daemon is
	// the only component that observes the loop, so without this the Control
	// Plane has no evidence of what was proposed, reviewed or rejected.
	Events []ver.TaskEvent `json:"events,omitempty"`
	// ExecSignature is the daemon's Ed25519 attestation over the execution and
	// verification subtrees. Optional: an older daemon omits it and the record
	// is persisted as unsigned rather than rejected.
	ExecSignature *ver.Signature `json:"exec_signature,omitempty"`
	// SandboxRuntime records which isolator actually ran the test command
	// ("docker" | "runsc" | "firecracker"), so the record states what was
	// observed instead of assuming a default.
	SandboxRuntime string `json:"sandbox_runtime,omitempty"`
}

// RenewReq extends a task's lease while it is still running.
type RenewReq struct {
	TaskID     string `json:"task_id"`
	LeaseID    string `json:"lease_id"`
	SignPubKey string `json:"sign_pub_key"`
}

// ProgressReq carries what has happened so far in a still-running task, so the
// dashboard can show a run as it happens rather than only once it is over.
//
// Events is a DELTA — only the phases not yet acknowledged — so a long run does
// not re-send its whole history every few seconds. The Control Plane treats
// these as provisional: the authoritative list arrives with ResultReq and
// replaces them.
type ProgressReq struct {
	TaskID  string `json:"task_id"`
	LeaseID string `json:"lease_id"`
	// SignPubKey identifies the reporting daemon (verified against X-Kiwi-Signature).
	SignPubKey string `json:"sign_pub_key"`
	// Events not yet accepted by the Control Plane, in execution order.
	Events []ver.TaskEvent `json:"events,omitempty"`
	// Phase names what is running right now — "install", "test", "actor" — for
	// the gap between two events, which on a slow command is most of the run.
	Phase string `json:"phase,omitempty"`
	// OutputTail is the end of the running command's output. The end is the part
	// that says what it is doing; the start is usually a banner.
	OutputTail string `json:"output_tail,omitempty"`
	// PhaseSince is when the current Phase started, so the dashboard can show
	// how long this step has actually been running — not just that the feed
	// is still alive (which ProgressAt on the receiving end already answers).
	PhaseSince time.Time `json:"phase_since,omitzero"`
}

// SessionCheckpointReq carries a session's durable position and the events that
// belong to it. Both travel together deliberately: a checkpoint that advanced
// the round without its events would leave a resumed session with a hole in its
// history exactly where the crash was.
type SessionCheckpointReq struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	LeaseID   string `json:"lease_id"`
	// SignPubKey identifies the reporting daemon (verified against X-Kiwi-Signature).
	SignPubKey string `json:"sign_pub_key"`
	JobID      string `json:"job_id,omitempty"`
	RepoURL    string `json:"repo_url,omitempty"`
	Branch     string `json:"branch,omitempty"`
	BaseSHA    string `json:"base_sha,omitempty"`
	HeadSHA    string `json:"head_sha,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Round      int    `json:"round"`
	Attempts   int    `json:"attempts"`
	MaxRounds  int    `json:"max_rounds,omitempty"`
	Rejections int    `json:"rejections,omitempty"`
	// State is pkg/session's opaque checkpoint. The Control Plane stores it and
	// hands it back; it never interprets it, so the session package can change
	// what it remembers without a migration.
	State          json.RawMessage `json:"state,omitempty"`
	ArchitectModel string          `json:"architect_model,omitempty"`
	WorkerModel    string          `json:"worker_model,omitempty"`
	CostUSD        float64         `json:"cost_usd,omitempty"`
	TokensIn       int64           `json:"tokens_in,omitempty"`
	TokensOut      int64           `json:"tokens_out,omitempty"`
	// Status is empty while the session runs, or SUCCEEDED/FAILED when it ends.
	Status string `json:"status,omitempty"`
	// Events are the phases since the last checkpoint, in order.
	Events []SessionEvent `json:"events,omitempty"`
}

// SessionEvent is one phase of a session on the wire.
type SessionEvent struct {
	Round      int     `json:"round"`
	Seq        int     `json:"seq"`
	Kind       string  `json:"kind"`
	Outcome    string  `json:"outcome,omitempty"`
	Tool       string  `json:"tool,omitempty"`
	Detail     string  `json:"detail,omitempty"`
	DurationMs int64   `json:"duration_ms,omitempty"`
	TokensIn   int64   `json:"tokens_in,omitempty"`
	TokensOut  int64   `json:"tokens_out,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
}

// SessionStateRes is the Control Plane's answer when a daemon asks whether a
// task already has a session to resume.
type SessionStateRes struct {
	// Found is false on a task's first lease, which is the normal case.
	Found     bool            `json:"found"`
	SessionID string          `json:"session_id,omitempty"`
	Round     int             `json:"round,omitempty"`
	Attempts  int             `json:"attempts,omitempty"`
	Status    string          `json:"status,omitempty"`
	State     json.RawMessage `json:"state,omitempty"`
}

// GitTokenReq asks the Control Plane for a git credential for a running task.
//
// The repository is deliberately absent. It is read from the task's own spec on
// the Control Plane, so a daemon cannot name a repository of its own choosing
// and buy a token for it: the only repository it can reach is the one belonging
// to a task it currently holds the lease on.
type GitTokenReq struct {
	TaskID     string `json:"task_id"`
	LeaseID    string `json:"lease_id"`
	SignPubKey string `json:"sign_pub_key"`
}

// GitTokenResp carries a short-lived GitHub App installation token.
//
// ExpiresAt is returned rather than assumed because the daemon decides when to
// ask again from it. A token is fetched immediately before each git operation
// rather than held for the length of a task, so no long-lived git credential
// exists on the data plane at all for App-backed orgs.
type GitTokenResp struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// TelemetryDueReq asks the Control Plane what telemetry polls are due for
// this daemon's org right now. Lease-free by design — see Task 8's handler
// doc comment for why this follows heartbeat's auth pattern, not GitToken's.
type TelemetryDueReq struct {
	SignPubKey string `json:"sign_pub_key"`
	Timestamp  int64  `json:"timestamp,omitempty"`
}

// TelemetryPollSpec is exactly what to query — provider, query string, and
// both time ranges — computed Control-Plane-side. The daemon does no
// scheduling or range-computation logic of its own; it only executes.
type TelemetryPollSpec struct {
	PollID        string    `json:"poll_id"`
	Provider      string    `json:"provider"`
	Query         string    `json:"query"`
	BaselineStart time.Time `json:"baseline_start"`
	BaselineEnd   time.Time `json:"baseline_end"`
	CurrentStart  time.Time `json:"current_start"`
	CurrentEnd    time.Time `json:"current_end"`
}

// TelemetryDueRes is the Control Plane's answer to TelemetryDueReq. When Due
// is non-empty, EncryptedCreds carries the org's credential bundle sealed to
// this daemon's X25519 public key (same mechanism as HeartbeatRes.EncryptedCreds,
// opened via the daemon's existing openCredentials helper) — delivered here,
// not on the heartbeat, because a heartbeat that leased no task never reaches
// the code path that seals credentials, and an idle daemon between polls is
// the routine state for telemetry, not an edge case. Empty when Due is empty:
// there is nothing to authenticate a provider connector with if no poll is due.
type TelemetryDueRes struct {
	Due            []TelemetryPollSpec `json:"due,omitempty"`
	EncryptedCreds string              `json:"encrypted_creds,omitempty"`
}

// TelemetryResultDTO mirrors telemetry.Result — a separate type in this
// package (not an import of pkg/telemetry into the wire-protocol layer)
// because types.go's job is JSON shape, not query execution; keeping them
// separate means pkg/telemetry's internal Result shape can change without
// touching the wire protocol.
type TelemetryResultDTO struct {
	SampleCount int     `json:"sample_count"`
	Mean        float64 `json:"mean"`
}

// TelemetryPollResult reports one poll's outcome. Baseline/Current are nil
// (not zero-valued) when that half of the query failed — Error carries why,
// distinguishing "queried and got zero samples" (a real, informative Result)
// from "the query itself errored" (nil + Error).
type TelemetryPollResult struct {
	PollID   string              `json:"poll_id"`
	Baseline *TelemetryResultDTO `json:"baseline,omitempty"`
	Current  *TelemetryResultDTO `json:"current,omitempty"`
	Error    string              `json:"error,omitempty"`
}

// TelemetryReportReq reports the results of the polls this daemon executed
// back to the Control Plane. Like TelemetryDueReq, it carries no LeaseID —
// telemetry polling is not tied to the task lease queue.
type TelemetryReportReq struct {
	SignPubKey string                `json:"sign_pub_key"`
	Timestamp  int64                 `json:"timestamp,omitempty"`
	Results    []TelemetryPollResult `json:"results"`
}

// TelemetryReportRes is the Control Plane's acknowledgement of a telemetry
// report. It carries no fields today; naming it keeps the wire types
// symmetric and leaves room to add one later.
type TelemetryReportRes struct{}
