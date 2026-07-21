package store

import "time"

// QueuedTask status values for the lease-based work queue.
const (
	TaskQueued    = "QUEUED"
	TaskLeased    = "LEASED"
	TaskSucceeded = "SUCCEEDED"
	TaskFailed    = "FAILED"
)

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
	JobID string `gorm:"index" json:"job_id"`
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
	Attempts     int       `gorm:"not null;default:0" json:"attempts"`
	ResultURL    *string   `json:"result_url"`
	ResultDetail *string   `json:"result_detail"`
	CreatedAt    time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt    time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (QueuedTask) TableName() string { return "queued_tasks" }
