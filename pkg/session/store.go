package session

import (
	"context"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// Checkpoint is everything a different process would need to pick this session
// up. It is the whole of the runner's position that is not already in git.
//
// What is deliberately absent is the Implementer's conversation. A round is
// re-run from its spec rather than resumed mid-transcript, which costs at most
// one round and avoids owning a provider-specific, half-written message array
// with a tool call outstanding. The Implementer starts every round fresh in the
// normal case too, so resumption is not a special path — it is the ordinary one
// entered at a different time.
type Checkpoint struct {
	// Round is the round to run next. A checkpoint written after round 2
	// finished carries Round 3.
	Round int `json:"round"`
	// Attempts counts starts of Round. Two means the round has already taken a
	// daemon down once.
	Attempts int    `json:"attempts"`
	Spec     Spec   `json:"spec"`
	BaseSHA  string `json:"base_sha"`
	HeadSHA  string `json:"head_sha"`
	// Architect and Implementer spend are kept apart so a resumed session
	// reports the same split an uninterrupted one does.
	Architect    provider.ToolUsage `json:"architect_usage"`
	Implementer  provider.ToolUsage `json:"implementer_usage"`
	Rejections   int                `json:"rejections"`
	History      []string           `json:"history"`
	LastVerify   string             `json:"last_verify"`
	VerifyPassed bool               `json:"verify_passed"`
	Progress     map[string]int     `json:"progress"`
	SpecSeen     map[string]int     `json:"spec_seen"`
}

// Store persists a session so it survives the process running it.
//
// The daemon has no database — it reaches the Control Plane over HTTP — so this
// is an interface rather than a store dependency, exactly as Workspace and
// VerifyFunc are. A nil Store runs the session entirely in memory, which is
// what tests and single-shot runs want.
type Store interface {
	// Load returns the checkpoint for a session, or nil when there is none.
	Load(ctx context.Context, sessionID string) (*Checkpoint, error)
	// Save writes the checkpoint and appends events atomically. The two must
	// land together: a checkpoint that advanced without its events leaves a
	// history with a hole where the interesting part was.
	Save(ctx context.Context, sessionID string, cp Checkpoint, events []Event) error
	// Finish records the session's terminal status so a re-leased task starts a
	// new session rather than resuming a concluded one.
	Finish(ctx context.Context, sessionID string, success bool) error
}

// maxRoundAttempts bounds how many times one round may be started.
//
// A round that kills its daemon twice is a poison pill, and the lease queue's
// own guard (MaxLeaseAttempts, five) would spend five leases and five
// cold-starts learning that. Two is enough to distinguish bad luck — a host
// restart, a lost network — from a round that reliably takes the process down.
const maxRoundAttempts = 2

// checkpoint captures the runner's current position.
func (st *state) checkpoint(baseSHA string, nextRound, attempts int) Checkpoint {
	return Checkpoint{
		Round:        nextRound,
		Attempts:     attempts,
		Spec:         st.spec,
		BaseSHA:      baseSHA,
		HeadSHA:      st.headSHA,
		Architect:    st.architect,
		Implementer:  st.implementer,
		Rejections:   st.rejections,
		History:      st.history,
		LastVerify:   st.lastVerify,
		VerifyPassed: st.verifyPassed,
		Progress:     st.progress,
		SpecSeen:     st.specSeen,
	}
}

// restore loads a checkpoint back into the runner's position.
func (st *state) restore(cp *Checkpoint) {
	st.spec = cp.Spec
	st.headSHA = cp.HeadSHA
	st.architect = cp.Architect
	st.implementer = cp.Implementer
	st.rejections = cp.Rejections
	st.history = cp.History
	st.lastVerify = cp.LastVerify
	st.verifyPassed = cp.VerifyPassed
	if cp.Progress != nil {
		st.progress = cp.Progress
	}
	if cp.SpecSeen != nil {
		st.specSeen = cp.SpecSeen
	}
}
