package store

import (
	"context"
	"crypto/ecdh"
	"time"

	"gorm.io/gorm"
)

// SnapshotRef points to a durable snapshot of the workspace.
type SnapshotRef struct {
	URI  string
	Hash string
}

// PlanSubmission tracks idempotent plan submissions.
type PlanSubmission struct {
	OrgID          string `gorm:"primaryKey"`
	IdempotencyKey string `gorm:"primaryKey"`
	JobID          string
	CreatedAt      time.Time
}

type JobSummary struct {
	JobID     string    `json:"job_id"`
	CreatedAt time.Time `json:"created_at"`
	TaskCount int       `json:"task_count"`
	Status    string    `json:"status"`
	PRURLs    []string  `json:"pr_urls"`
}

// Store defines the data access interface for the control plane.
// It abstracts away the underlying database (e.g. Postgres or SQLite)
// and provides a unified interface for all subsystems.
type Store interface {
	// Tenancy & Limits
	GetOrganization(ctx context.Context, id string) (*Organization, error)
	GetOrgLimits(ctx context.Context, orgID string) (*OrgLimits, error)
	// Jobs (Target V2 Schema)
	CreateJobWithOutbox(ctx context.Context, job *Job, outbox *Outbox) error
	GetJob(ctx context.Context, id string) (*Job, error)
	ListJobs(ctx context.Context, orgID string) ([]JobSummary, error)
	UpdateJobStatus(ctx context.Context, id string, expectedStatus string, newStatus string) (bool, error)
	UpdateJobCost(ctx context.Context, id string, additionalCost float64) error
	CreateManifest(ctx context.Context, m *Manifest) error
	UpdateJobManifest(ctx context.Context, jobID, manifestID string) error

	// Events & Checkpoints
	AppendEvent(ctx context.Context, event *Event) error
	SaveCheckpoint(ctx context.Context, checkpoint *Checkpoint) error

	// Side Effects (Idempotency)
	GetSideEffect(ctx context.Context, id string) (*SideEffect, error)
	RecordSideEffect(ctx context.Context, effect *SideEffect) error

	// Lease-based work queue (BYOC daemon handoff). Tasks are leased, not
	// destructively popped, so a crashed daemon's work returns to the queue.
	EnqueueTask(ctx context.Context, task *QueuedTask) error
	LeaseNextTask(ctx context.Context, orgID, leasedBy, fleetID string, ttl time.Duration) (*QueuedTask, error)
	RenewLease(ctx context.Context, taskID, leaseID string, ttl time.Duration) (bool, error)
	CompleteTask(ctx context.Context, taskID, leaseID, finalStatus, resultURL, detail string) (bool, error)
	RequeueExpiredLeases(ctx context.Context) (int, error)
	ExpireStaleQueuedTasks(ctx context.Context, ttl time.Duration) (int, error)
	GetJobTasks(ctx context.Context, orgID, jobID string) ([]QueuedTask, error)

	// Fleets & models (dashboard).
	CreateFleet(ctx context.Context, orgID, name, ftype string) (*Fleet, error)
	ListFleets(ctx context.Context, orgID string) ([]Fleet, error)
	CreateModel(ctx context.Context, orgID, name, provider string) (*ModelEntry, error)
	ListModels(ctx context.Context, orgID string) ([]ModelEntry, error)
	DeleteModel(ctx context.Context, orgID, id string) error

	// Daemons: Data Plane runner identity. A daemon's Ed25519 key is its
	// identity and resolves a heartbeat to an org; registration is gated by a
	// short-lived, org-bound, single-use join token (no trust-on-first-use).
	CreateDaemonJoinToken(ctx context.Context, orgID, fleetID string, ttl time.Duration) (string, error)
	RegisterDaemon(ctx context.Context, joinToken, signPubKey, encPubKey string) (*Daemon, error)
	GetDaemonBySignPubKey(ctx context.Context, signPubKey string) (*Daemon, error)
	TouchDaemon(ctx context.Context, id string) error
	ListDaemons(ctx context.Context, orgID string) ([]Daemon, error)

	// Credentials: org-scoped secrets, AES-256-GCM encrypted at rest and
	// re-sealed to a daemon's X25519 public key for delivery.
	SaveCredential(ctx context.Context, orgID, name, kind, plaintext string) error
	ListCredentials(ctx context.Context, orgID string) ([]Credential, error)
	GetCredentialPlaintext(ctx context.Context, orgID, name string) (string, error)
	SealCredentialsForDaemon(ctx context.Context, orgID string, daemonPubKey *ecdh.PublicKey) (string, error)

	// Legacy orchestrator tasks mapping (temp for V1-V2 transition)
	UpdateTaskLogs(ctx context.Context, id string, logs string) error

	// DB Accessor for gradual migration of legacy endpoints
	DB() *gorm.DB
}
