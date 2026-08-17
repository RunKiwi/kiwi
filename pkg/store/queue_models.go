package store

import "time"

// QueuedTask status values for the lease-based work queue.
const (
	TaskQueued    = "QUEUED"
	TaskLeased    = "LEASED"
	TaskSucceeded = "SUCCEEDED"
	TaskFailed    = "FAILED"
	// TaskCancelled is a user-requested stop. It is terminal and deliberately
	// distinct from FAILED: a job you called off did not fail, and folding the
	// two together would make the failure rate unreadable. Nothing retries a
	// cancelled task automatically — RetryJob is an explicit act.
	TaskCancelled = "CANCELLED"
)

// IsTerminal reports whether a task status is final. Terminal tasks are never
// leased, swept, or diagnosed.
func IsTerminal(status string) bool {
	return status == TaskSucceeded || status == TaskFailed || status == TaskCancelled
}

// MaxLeaseAttempts bounds how many times a task may be leased before it is
// treated as a poison pill. A task whose lease expires after this many attempts
// is dead-lettered (marked FAILED) rather than requeued forever — so a spec
// that reliably crashes its daemon cannot loop indefinitely.
const MaxLeaseAttempts = 5

// QueuedTask is a unit of work (a worker-spec) waiting for a daemon to lease and
// execute it. It implements a lease-based queue rather than a destructive pop:
// a task is NOT removed when handed out — it is LEASED to one daemon for a
// bounded window. If that daemon dies without renewing, the lease expires and
// the task returns to QUEUED so another daemon can pick it up (crash recovery).
//
// LeaseID is a fencing token: every renew/complete must present it, so a stale
// daemon whose lease has since been reassigned cannot mutate the task.
//
//	QUEUED ──lease──▶ LEASED ──complete──▶ SUCCEEDED | FAILED
//	   ▲                 │
//	   └──lease expiry───┘
type QueuedTask struct {
	ID    string `gorm:"primaryKey" json:"id"`
	OrgID string `gorm:"index;not null" json:"org_id"`
	// JobID links the task back to the job/manifest that produced it.
	//
	// Since continuations, a job id no longer identifies exactly one task: a
	// continuation deliberately reuses its parent's, because the job id is what
	// names the branch (jobBranchName is "kiwi/"+JobID) and therefore what puts
	// the next round on the same pull request. Anything reading by JobID alone
	// now gets a thread.
	JobID string `gorm:"index" json:"job_id"`
	// ParentTaskID is the task this one continues or forks from. NULL on a task
	// submitted directly, which is every task that existed before lineage.
	ParentTaskID *string `gorm:"index" json:"parent_task_id"`
	// RootTaskID is the thread this task belongs to, equal to its own id on a
	// root. Denormalised on purpose: the parent chain alone would need a
	// recursive query, and "give me this whole thread" is asked on every task
	// view in the dashboard.
	RootTaskID string `gorm:"index" json:"root_task_id"`
	// Origin records how this task came to exist: submit | pr_comment | fork | postmerge_remediation.
	// The dashboard labels a node from it without having to infer, and a
	// trigger stays auditable after the fact.
	Origin string `gorm:"not null;default:submit" json:"origin"`
	// TriggerCommentID is the GitHub comment that caused a pr_comment task.
	// Unique, because GitHub redelivers webhooks and a redelivery must not buy
	// the customer a second round.
	TriggerCommentID *int64 `gorm:"uniqueIndex" json:"trigger_comment_id"`
	// FleetID optionally scopes the task to a fleet (empty = any fleet).
	FleetID string `gorm:"index" json:"fleet_id"`
	// Status ∈ QUEUED|LEASED|SUCCEEDED|FAILED.
	Status string `gorm:"index;not null" json:"status"`
	// Spec is the worker-spec.json payload the daemon executes.
	Spec map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"spec"`
	// LeasedBy identifies the daemon currently holding the lease (nil when QUEUED).
	LeasedBy *string `json:"leased_by"`
	// LeaseID is the fencing token proving current ownership (nil when QUEUED).
	LeaseID *string `json:"lease_id"`
	// LeaseExpiresAt is when the current lease lapses (nil when QUEUED).
	LeaseExpiresAt *time.Time `gorm:"index" json:"lease_expires_at"`
	// StartedAt is when the task was leased (execution start). Unlike UpdatedAt,
	// it is set once at lease and never touched by RenewLease, so it is the
	// correct basis for agent-minutes metering: time.Since(StartedAt) is the full
	// task duration, whereas UpdatedAt resets on every renewal.
	StartedAt *time.Time `json:"started_at"`
	// Attempts counts how many times this task has been leased.
	Attempts int `gorm:"not null;default:0" json:"attempts"`
	// ProgressPhase and ProgressOutput are the daemon's live report of what is
	// happening right now — the command running and the tail of its output.
	// Overwritten on every update rather than appended: this is a "what is it
	// doing" indicator, not a log. The ordered history lives in task_events.
	ProgressPhase  *string    `json:"progress_phase"`
	ProgressOutput *string    `json:"progress_output"`
	ProgressAt     *time.Time `json:"progress_at"`
	// ProgressPhaseSince is when ProgressPhase started, distinct from
	// ProgressAt (when the daemon last reported anything at all). Set once per
	// phase change and left alone on every subsequent report of the same
	// phase — see progressReporter.setActivity in pkg/daemon.
	ProgressPhaseSince *time.Time `json:"progress_phase_since"`
	ResultURL          *string    `json:"result_url"`
	ResultDetail       *string    `json:"result_detail"`
	CostUSD            float64    `gorm:"not null;default:0" json:"cost_usd"`
	Funding            string     `gorm:"not null;default:'byok'" json:"funding"`
	TokensIn           int64      `gorm:"not null;default:0" json:"tokens_in"`
	TokensOut          int64      `gorm:"not null;default:0" json:"tokens_out"`
	MeteredAt          *time.Time `json:"metered_at"`
	CreatedAt          time.Time  `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (QueuedTask) TableName() string { return "queued_tasks" }
