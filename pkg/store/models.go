package store

import (
	"time"
)

// Organization represents a tenant in the system.
type Organization struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	CreatedAt time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}

// OrgLimits represents the resource limits and quotas for an organization.
type OrgLimits struct {
	OrgID              string  `gorm:"primaryKey" json:"org_id"`
	MaxConcurrentJobs  int     `gorm:"not null;default:10" json:"max_concurrent_jobs"`
	MaxBudgetPerJob    float64 `gorm:"not null;default:5.00" json:"max_budget_per_job"`
	MaxBudgetPerMonth  float64 `gorm:"not null;default:500.00" json:"max_budget_per_month"`
	MaxWorkersPerJob   int     `gorm:"not null;default:8" json:"max_workers_per_job"`
	TaskTimeoutSeconds int     `gorm:"not null;default:1800" json:"task_timeout_seconds"`
	MaxSandboxDiskMB   int     `gorm:"not null;default:2048" json:"max_sandbox_disk_mb"`
}

// User represents an authenticated user belonging to an organization.
type User struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Email     string    `json:"email" gorm:"uniqueIndex;not null"`
	Name      string    `json:"name"`
	OrgID     string    `json:"org_id" gorm:"index;not null"`
	Role      string    `json:"role" gorm:"not null;default:member"` // "admin" or "member"
	CreatedAt time.Time `json:"created_at"`
}

// APIKey represents a hashed API key associated with a user.
type APIKey struct {
	ID        string     `json:"id" gorm:"primaryKey"`
	KeyHash   string     `json:"-" gorm:"uniqueIndex;not null"` // SHA-256 hash of the plaintext key
	UserID    string     `json:"user_id" gorm:"index;not null"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// IsExpired returns true if the key has passed its expiration date.
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*k.ExpiresAt)
}

// IsRevoked returns true if the key has been revoked.
func (k *APIKey) IsRevoked() bool {
	return k.RevokedAt != nil
}

// OrgProviderConfig holds org-specific configuration for LLM providers.
type OrgProviderConfig struct {
	OrgID           string    `json:"org_id" gorm:"primaryKey"`
	AnthropicAPIKey string    `json:"anthropic_api_key,omitempty"` // plain text temporarily (should encrypt at rest)
	OpenAIAPIKey    string    `json:"openai_api_key,omitempty"`
	GoogleAPIKey    string    `json:"google_api_key,omitempty"`
	DefaultActor    string    `json:"default_actor" gorm:"default:anthropic"`
	DefaultCritic   string    `json:"default_critic" gorm:"default:anthropic"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Workflow represents a versioned workflow/template definition.
type Workflow struct {
	ID        string                 `gorm:"primaryKey" json:"id"`
	OrgID     *string                `gorm:"index;uniqueIndex:idx_wf_org_name_ver,priority:1" json:"org_id"`
	Name      string                 `gorm:"not null;uniqueIndex:idx_wf_org_name_ver,priority:2" json:"name"`
	Version   int                    `gorm:"not null;uniqueIndex:idx_wf_org_name_ver,priority:3" json:"version"`
	Spec      map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"spec"`
	CreatedAt time.Time              `gorm:"not null;default:current_timestamp" json:"created_at"`
}

// Manifest represents the immutable, content-addressed resolved workflow payload.
type Manifest struct {
	ID            string                 `gorm:"primaryKey" json:"id"` // sha256 of content
	OrgID         string                 `gorm:"not null" json:"org_id"`
	WorkflowID    *string                `json:"workflow_id"`
	SchemaVersion string                 `gorm:"not null" json:"schema_version"`
	Content       map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"content"`
	Producer      string                 `gorm:"not null" json:"producer"`
	Signature     *string                `json:"signature"`
	CreatedAt     time.Time              `gorm:"not null;default:current_timestamp" json:"created_at"`
}

// Job represents a submitted job/task. This is the central source of truth for scheduling.
type Job struct {
	ID         string  `gorm:"primaryKey" json:"id"`
	OrgID      string  `gorm:"not null;index;uniqueIndex:idx_org_idempotency,priority:1" json:"org_id"`
	UserID     string  `gorm:"not null" json:"user_id"`
	WorkflowID *string `json:"workflow_id"`
	ManifestID *string `json:"manifest_id"`
	Status     string  `gorm:"index;not null" json:"status"` // PENDING|SCHEDULING|RUNNING|PAUSED|SUCCEEDED|FAILED|CANCELED
	// Dedupe is scoped per-org (org_id, idempotency_key) to match migration 0001;
	// a single-column unique here would leak/collide keys across tenants.
	IdempotencyKey *string                `gorm:"uniqueIndex:idx_org_idempotency,priority:2" json:"idempotency_key"`
	Inputs         map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"inputs"`
	SandboxRef     *string                `json:"sandbox_ref"`
	CostUSD        float64                `gorm:"not null;default:0" json:"cost_usd"`
	Error          *string                `json:"error"`
	CreatedAt      time.Time              `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt      time.Time              `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

// Agent represents an individual agent (master or worker) inside a job.
type Agent struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	JobID     string    `gorm:"index;not null" json:"job_id"`
	Role      string    `gorm:"not null" json:"role"` // 'master' | 'worker'
	Model     string    `gorm:"not null" json:"model"`
	Status    string    `gorm:"not null" json:"status"`
	CreatedAt time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}

// Outbox represents the transactional outbox table for exactly-once queue handoff.
type Outbox struct {
	ID          int64                  `gorm:"primaryKey;autoIncrement" json:"id"`
	JobID       string                 `gorm:"not null" json:"job_id"`
	Topic       string                 `gorm:"not null" json:"topic"`
	Payload     map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"payload"`
	PublishedAt *time.Time             `gorm:"index" json:"published_at"`
	CreatedAt   time.Time              `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (Outbox) TableName() string {
	return "outbox"
}

// Event represents an append-only execution trace event.
type Event struct {
	ID      int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	JobID   string  `gorm:"index;not null;uniqueIndex:idx_job_seq,priority:1" json:"job_id"`
	AgentID *string `json:"agent_id"`
	// seq is monotonic PER JOB — the unique index is composite (job_id, seq),
	// matching migration 0001; a bare unique on seq would be global.
	Seq       int64                  `gorm:"not null;uniqueIndex:idx_job_seq,priority:2" json:"seq"`
	Phase     string                 `gorm:"not null" json:"phase"`
	Payload   map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"payload"`
	TraceID   *string                `json:"trace_id"`
	SpanID    *string                `json:"span_id"`
	CreatedAt time.Time              `gorm:"not null;default:current_timestamp" json:"created_at"`
}

// Checkpoint represents a durable snapshot of the workspace and execution state.
type Checkpoint struct {
	ID           string                 `gorm:"primaryKey" json:"id"`
	JobID        string                 `gorm:"index;not null;uniqueIndex:idx_job_eventseq,priority:1" json:"job_id"`
	AgentID      *string                `json:"agent_id"`
	EventSeq     int64                  `gorm:"not null;uniqueIndex:idx_job_eventseq,priority:2" json:"event_seq"`
	SnapshotURI  *string                `json:"snapshot_uri"`
	SnapshotHash *string                `json:"snapshot_hash"`
	State        map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"state"`
	CreatedAt    time.Time              `gorm:"not null;default:current_timestamp" json:"created_at"`
}

// SideEffect represents a ledger entry for idempotency across side-effecting tool calls.
type SideEffect struct {
	ID          string    `gorm:"primaryKey" json:"id"` // hash(job_id, step, effect_signature)
	JobID       string    `gorm:"not null" json:"job_id"`
	EffectType  string    `gorm:"not null" json:"effect_type"`
	ResultURI   *string   `json:"result_uri"`
	CommittedAt time.Time `gorm:"not null;default:current_timestamp" json:"committed_at"`
}

// AuditLog represents an audit log entry for billing/compliance.
type AuditLog struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID      string    `json:"org_id"`
	UserID     string    `json:"user_id"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id"`
	Details    string    `json:"details"`
	ClientIP   string    `json:"client_ip"`
	CreatedAt  time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}
