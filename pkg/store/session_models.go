package store

import "time"

// AgentSession statuses.
const (
	SessionRunning   = "RUNNING"
	SessionSucceeded = "SUCCEEDED"
	SessionFailed    = "FAILED"
)

// AgentSession is the durable position of one agentic session (pkg/session).
//
// It is deliberately not a transcript. The work itself lives in git — the job
// branch, committed round by round — and the Architect's context is rebuilt
// from AgentSessionEvent rows on every call. What is stored here is only what a
// different daemon would need in order to pick the task up: where the session
// got to, what it was last asked to do, and what it has spent.
//
// That split is what makes a task-long conversation survivable on a lease
// queue built for disposable work. A crashed round is discarded rather than
// resumed: the Implementer starts each round fresh anyway, so re-running one
// from HeadSHA and Spec costs at most a round and needs no half-written
// provider state to be persisted or replayed.
type AgentSession struct {
	ID    string `gorm:"primaryKey" json:"id"`
	OrgID string `gorm:"index;not null" json:"org_id"`
	JobID string `gorm:"index" json:"job_id"`
	// TaskID is the queued_tasks row this session belongs to. Unique, so
	// resuming is a lookup by the thing the queue actually hands out.
	TaskID  string `gorm:"uniqueIndex;not null" json:"task_id"`
	RepoURL string `json:"repo_url"`
	Branch  string `json:"branch"`
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha"`
	Phase   string `json:"phase"`
	Round   int    `gorm:"not null;default:0" json:"round"`
	// RoundAttempts counts starts of the CURRENT round. A round that takes its
	// daemon down twice is a poison pill and fails the session, rather than
	// spending all five of MaxLeaseAttempts discovering the same thing.
	RoundAttempts  int    `gorm:"not null;default:0" json:"round_attempts"`
	MaxRounds      int    `gorm:"not null;default:4" json:"max_rounds"`
	Rejections     int    `gorm:"not null;default:0" json:"rejections"`
	ArchitectModel string `json:"architect_model"`
	WorkerModel    string `json:"worker_model"`
	// State is pkg/session's own checkpoint payload. Nothing queries inside it:
	// giving each field a column would freeze that package's internals into the
	// schema, and the queue needs none of them to make a scheduling decision.
	State     map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"state"`
	CostUSD   float64                `gorm:"not null;default:0" json:"cost_usd"`
	TokensIn  int64                  `gorm:"not null;default:0" json:"tokens_in"`
	TokensOut int64                  `gorm:"not null;default:0" json:"tokens_out"`
	Status    string                 `gorm:"not null;default:RUNNING" json:"status"`
	CreatedAt time.Time              `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time              `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (AgentSession) TableName() string { return "agent_sessions" }

// AgentSessionEvent is one thing that happened, in order.
//
// Append-only, and numbered by the daemon within (session, round) so a retried
// checkpoint cannot duplicate a round's history. Detail is a bounded tail
// rather than full output: tool output can carry secrets, and this table is
// read by the dashboard and the execution record.
type AgentSessionEvent struct {
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// (SessionID, Round, Seq) is unique so a retried checkpoint cannot duplicate
	// a round's history. The daemon numbers the events; the database enforces
	// that numbering, because "the daemon does not retry" is not a property
	// anything can guarantee across a crash.
	SessionID  string    `gorm:"index;not null;uniqueIndex:idx_ase_seq,priority:1" json:"session_id"`
	OrgID      string    `gorm:"index;not null" json:"org_id"`
	Round      int       `gorm:"not null;default:0;uniqueIndex:idx_ase_seq,priority:2" json:"round"`
	Seq        int       `gorm:"not null;uniqueIndex:idx_ase_seq,priority:3" json:"seq"`
	Kind       string    `gorm:"not null" json:"kind"`
	Outcome    string    `json:"outcome"`
	Tool       string    `json:"tool"`
	Detail     string    `json:"detail"`
	DurationMs int64     `gorm:"not null;default:0" json:"duration_ms"`
	TokensIn   int64     `gorm:"not null;default:0" json:"tokens_in"`
	TokensOut  int64     `gorm:"not null;default:0" json:"tokens_out"`
	CostUSD    float64   `gorm:"not null;default:0" json:"cost_usd"`
	CreatedAt  time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (AgentSessionEvent) TableName() string { return "agent_session_events" }
