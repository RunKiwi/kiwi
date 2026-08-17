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
	// Task is the overall goal that produced this job (from the task spec), shown
	// on the job card. Repo is the "owner/name" the job targets.
	Task string `json:"task"`
	Repo string `json:"repo"`
	// FleetID is the fleet the job's tasks target (empty = any fleet). DaemonID
	// is the daemon that leased the work, when known. Used by the topology view
	// to hang a job off its actual executor rather than the Control Plane.
	FleetID  string `json:"fleet_id"`
	DaemonID string `json:"daemon_id"`
	// ContinuationCount is how many of this job's tasks came from a review
	// comment, so a row can say "3 runs" only when there is really a thread —
	// TaskCount cannot, because a plan with three workers also counts three.
	ContinuationCount int `json:"continuation_count"`
	// LatestOrigin is how the newest task in the job came to exist. Without it a
	// thread continued an hour ago still reads as "submitted", and the row is a
	// receipt for something that has since moved on.
	LatestOrigin string `json:"latest_origin"`
}

// TaskCompletion wraps the arguments for ending a task's lease.
type TaskCompletion struct {
	TaskID, LeaseID, FinalStatus, ResultURL, Detail string
	CostUSD                                         float64
	TokensIn, TokensOut                             int64
}

// Store defines the data access interface for the control plane.
// It abstracts away the underlying database (e.g. Postgres or SQLite)
// and provides a unified interface for all subsystems.
type Store interface {
	// Agentic sessions (pkg/session). A session is a task-long conversation, so
	// unlike a queued task it needs a durable position of its own: the queue can
	// only say a task is LEASED, not which round of it is in flight.
	GetAgentSessionByTask(ctx context.Context, orgID, taskID string) (*AgentSession, error)
	SaveAgentSession(ctx context.Context, sess *AgentSession, events []AgentSessionEvent) error
	ListAgentSessionEvents(ctx context.Context, orgID, sessionID string) ([]AgentSessionEvent, error)
	FinishAgentSession(ctx context.Context, orgID, sessionID, status string) error
	// ReattachSession moves a session onto the task about to continue it, and
	// reopens it. A session belongs to one task at a time because the load path
	// resolves it by task id, so continuing a thread means moving it.
	ReattachSession(ctx context.Context, orgID, sessionID, newTaskID string) error

	// Task lineage. A review comment on a pull request starts another task that
	// continues the same session, so a task's history is a thread of them.
	ThreadTasks(ctx context.Context, orgID, rootTaskID string) ([]QueuedTask, error)
	ActiveTaskInThread(ctx context.Context, orgID, rootTaskID string) (*QueuedTask, error)
	PRCommentMode(ctx context.Context, orgID string) (string, error)
	SetPRCommentMode(ctx context.Context, orgID, mode string) error

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
	CompleteTask(ctx context.Context, c TaskCompletion) (bool, error)
	// RecordTaskProgress stores a running task's current activity. Fenced by the
	// lease id so a daemon that lost the task cannot write to it.
	RecordTaskProgress(ctx context.Context, taskID, leaseID, phase, output string, phaseSince time.Time) (bool, error)
	RequeueExpiredLeases(ctx context.Context) (int, error)
	ExpireStaleQueuedTasks(ctx context.Context, ttl time.Duration) (int, error)
	GetJobTasks(ctx context.Context, orgID, jobID string) ([]QueuedTask, error)
	// DiagnoseQueuedTasks explains why each QUEUED task among the given tasks has
	// not started (no runner, cold-start in flight, at a cap, blocked on a
	// dependency, ...). Read-only; it never affects scheduling.
	DiagnoseQueuedTasks(ctx context.Context, orgID string, tasks []QueuedTask) (map[string]TaskDiagnosis, error)

	// Job lifecycle, driven by the user rather than by execution.
	CancelJob(ctx context.Context, orgID, jobID, reason string) (int, error)
	RetryJob(ctx context.Context, orgID, jobID string) (int, error)
	DeleteJob(ctx context.Context, orgID, jobID string) (int, error)
	HasActiveTasks(ctx context.Context, orgID string) (bool, error)

	// Execution records (provenance). AppendExecutionRecord builds and inserts
	// under one transaction so the chain head cannot race; see pkg/store/ver.go.
	AppendExecutionRecord(ctx context.Context, orgID, jobID, ver string, build func(prevHash string) (*ExecutionRecord, error)) (*ExecutionRecord, error)
	GetExecutionRecordChainHead(ctx context.Context, orgID string) (string, error)
	GetExecutionRecord(ctx context.Context, orgID, jobID string) (*ExecutionRecord, error)
	GetExecutionRecordByVer(ctx context.Context, orgID, jobID, ver string) (*ExecutionRecord, error)
	GetJobExecutionRecords(ctx context.Context, orgID, jobID string) ([]ExecutionRecord, error)
	GetQueuedTask(ctx context.Context, taskID string) (*QueuedTask, error)
	GetManifest(ctx context.Context, id string) (*Manifest, error)

	// Fleets & models (dashboard).
	CreateFleet(ctx context.Context, orgID, name, ftype string) (*Fleet, error)
	ListFleets(ctx context.Context, orgID string) ([]Fleet, error)
	CreateModel(ctx context.Context, orgID, name, provider string) (*ModelEntry, error)
	ListModels(ctx context.Context, orgID string) ([]ModelEntry, error)
	DeleteModel(ctx context.Context, orgID, id string) error

	UpsertCatalogModel(ctx context.Context, m *CatalogModel) error
	ListCatalogModels(ctx context.Context, orgID string) ([]CatalogModel, error)
	GetCatalogModel(ctx context.Context, orgID, modelID string) (*CatalogModel, error)
	ResolveModel(ctx context.Context, orgID, modelID string) (Resolution, error)
	MarkCatalogMissing(ctx context.Context, orgID, providerID string, seen []string, at time.Time) error

	// Daemons: Data Plane runner identity. A daemon's Ed25519 key is its
	// identity and resolves a heartbeat to an org; registration is gated by a
	// short-lived, org-bound, single-use join token (no trust-on-first-use).
	CreateDaemonJoinToken(ctx context.Context, orgID, fleetID string, ttl time.Duration) (string, error)
	RegisterDaemon(ctx context.Context, joinToken, signPubKey, encPubKey string) (*Daemon, error)
	GetDaemonBySignPubKey(ctx context.Context, signPubKey string) (*Daemon, error)
	TouchDaemon(ctx context.Context, id string) error
	ListDaemons(ctx context.Context, orgID string) ([]Daemon, error)
	// DeleteDaemonsByOrgAndFleet removes an org's daemon registrations for a fleet
	// (used by idle-reclaim to deregister a stopped free daemon so it does not
	// orphan a row). Returns the number deleted.
	DeleteDaemonsByOrgAndFleet(ctx context.Context, orgID, fleetID string) (int64, error)

	// Credentials: org-scoped secrets, AES-256-GCM encrypted at rest and
	// re-sealed to a daemon's X25519 public key for delivery.
	SaveCredential(ctx context.Context, orgID, name, kind, plaintext string) error
	ListCredentials(ctx context.Context, orgID string) ([]Credential, error)
	GetCredentialPlaintext(ctx context.Context, orgID, name string) (string, error)
	SealCredentialsForDaemon(ctx context.Context, orgID string, daemonPubKey *ecdh.PublicKey, extra map[string]string) (string, error)

	IsKiwiOperatedFleet(ctx context.Context, orgID, fleetID string) (bool, error)

	EnsureGrant(ctx context.Context, orgID, tier, period string, granted int64) (*OrgTokenGrant, error)
	ConsumeTokens(ctx context.Context, orgID, tier, period string, n int64) error
	ListGrants(ctx context.Context, orgID, period string) ([]OrgTokenGrant, error)

	GetOrgPlan(ctx context.Context, orgID string) (string, error)

	// Legacy orchestrator tasks mapping (temp for V1-V2 transition)
	UpdateTaskLogs(ctx context.Context, id string, logs string) error

	// DB Accessor for gradual migration of legacy endpoints
	DB() *gorm.DB

	// Learnings — cross-task shared context. UpsertJobLearning records a job's
	// learning row; CompleteTask (not a standalone method) owns the terminal
	// outcome/pr_url update so it stays atomic with task completion.
	UpsertJobLearning(ctx context.Context, learning *JobLearning) error
	GetJobLearnings(ctx context.Context, orgID string, jobIDs []string) ([]JobLearning, error)
	SearchJobLearnings(ctx context.Context, orgID string, taskEmbedding []float32, limit int, excludeJobID string) ([]JobLearning, error)

	// FindLeasedTask returns a task only to the holder of its current lease. It
	// is an authorisation check: holding the lease is the basis on which a
	// daemon is allowed to buy a git credential for that task's repository.
	FindLeasedTask(ctx context.Context, taskID, leaseID string) (*QueuedTask, error)

	// GitHub App installations. These replace the personal access token as the
	// way Kiwi reaches a customer's repositories; GIT_TOKEN remains the
	// fallback for non-GitHub remotes and for orgs that have not installed.
	UpsertGitHubInstallation(ctx context.Context, inst *GitHubInstallation) error
	FindGitHubInstallation(ctx context.Context, orgID, accountLogin string) (*GitHubInstallation, error)
	ListGitHubInstallations(ctx context.Context, orgID string) ([]GitHubInstallation, error)
	DeleteGitHubInstallation(ctx context.Context, installationID int64) error
	GetGitHubInstallationByID(ctx context.Context, installationID int64) (*GitHubInstallation, error)

	// Post-Merge Verification (Phase 1a). CreateMonitor opens a monitor at
	// merge time; GetMonitorByMergeCommit resolves a webhook event back to
	// its still-open monitor; FinalizeMonitor is the single-fire atomic
	// transition out of MONITORING (see pkg/store/postmerge_monitor.go for
	// why the bool return matters); SetMonitorRemediationTaskID records the
	// continuation task a REGRESSION verdict spawned; ListMonitorsPastWindow
	// feeds the periodic sweep. AutoRemediate reports an org's opt-in to
	// auto-spawning that continuation.
	CreateMonitor(ctx context.Context, m *PostMergeMonitor) error
	GetMonitorByMergeCommit(ctx context.Context, orgID, sha string) (*PostMergeMonitor, error)
	FinalizeMonitor(ctx context.Context, id, newStatus, evidence string) (bool, error)
	SetMonitorRemediationTaskID(ctx context.Context, id, taskID string) error
	ListMonitorsPastWindow(ctx context.Context, now time.Time) ([]PostMergeMonitor, error)
	AutoRemediate(ctx context.Context, orgID string) (bool, error)

	// TelemetryMetric is org-level, operator-configured metric config a
	// PostMergeMonitor's telemetry poll (Task 11) picks from via the
	// originating task's Intent. See pkg/store/telemetry_metric.go.
	CreateTelemetryMetric(ctx context.Context, m *TelemetryMetric) error
	ListTelemetryMetrics(ctx context.Context, orgID, repo string) ([]TelemetryMetric, error)
}
