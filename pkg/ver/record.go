package ver

// SchemaVersion gates the canonicalization and signing rules. A verifier that
// does not recognise the version must fail closed rather than guess.
const SchemaVersion = "kiwi.ver/v1"

// Attestation states. Both are inside the signing payload, so they can only be
// set before signing — SignRecord owns that transition.
const (
	AttestationSigned   = "signed"
	AttestationUnsigned = "unsigned"
)

// Record is the root Verified Execution Record (VER) per job.
type Record struct {
	Ver            string `json:"ver"` // schema version (e.g., "kiwi.ver/v1")
	RecordID       string `json:"record_id"`
	OrgID          string `json:"org_id"`
	JobID          string `json:"job_id"`
	PrevRecordHash string `json:"prev_record_hash"` // per-org hash chain; "" for the genesis record
	// Attestation is "signed" when a Control Plane signature is present and
	// "unsigned" otherwise, so a reader never has to infer authenticity from a
	// missing field. A record from a daemon too old to sign stays valid.
	Attestation     string        `json:"attestation"`
	Intent          Intent        `json:"intent"`
	GoverningSpec   GoverningSpec `json:"governing_spec"`
	Subject         Subject       `json:"subject"`
	Execution       Execution     `json:"execution"`
	Verification    Verification  `json:"verification"`
	Delivery        Delivery      `json:"delivery"`
	ExecSignature   *Signature    `json:"exec_signature,omitempty"`
	RecordSignature *Signature    `json:"record_signature,omitempty"`
}

// Timestamps are RFC 3339 strings rather than time.Time. A record must
// re-canonicalize to identical bytes years later, and a time.Time round-trip
// can vary in precision and zone offset; a fixed string cannot.
type Intent struct {
	Task           string `json:"task"`
	Source         string `json:"source"`
	SubmittedBy    string `json:"submitted_by"`
	SubmittedAt    string `json:"submitted_at"`
	IdempotencyKey string `json:"idempotency_key"`
}

type GoverningSpec struct {
	PlanManifestHash string   `json:"plan_manifest_hash"`
	PlanSummary      string   `json:"plan_summary"`
	PlannerModel     string   `json:"planner_model"`
	PlannerProvider  string   `json:"planner_provider"`
	ReferenceMode    string   `json:"reference_mode"`
	ReferenceJobIDs  []string `json:"reference_job_ids"` // ensure this serializes as [] rather than null if empty
}

type Subject struct {
	Repo         string   `json:"repo"`
	Ref          string   `json:"ref"`
	BaseCommit   string   `json:"base_commit"`
	HeadCommit   string   `json:"head_commit"`
	FilesTouched []string `json:"files_touched"`
}

type Execution struct {
	FleetID      string              `json:"fleet_id"`
	Mode         string              `json:"mode"`
	DaemonID     string              `json:"daemon_id"`
	DaemonPubKey string              `json:"daemon_pubkey"`
	Sandbox      Sandbox             `json:"sandbox"`
	Workers      []WorkerAttestation `json:"workers"`
}

type Sandbox struct {
	Runtime string `json:"runtime"`
	Network string `json:"network"`
}

type WorkerAttestation struct {
	WorkerID         string       `json:"worker_id"`
	DependsOn        []string     `json:"depends_on"`
	ActorModel       string       `json:"actor_model"`
	CriticModel      string       `json:"critic_model"`
	Provider         string       `json:"provider"`
	Steps            []WorkerStep `json:"steps"`
	CriticRejections int          `json:"critic_rejections"`
	InputTokens      int64        `json:"input_tokens"`
	OutputTokens     int64        `json:"output_tokens"`
	CostUSD          float64      `json:"cost_usd"`
}

type WorkerStep struct {
	Step       int    `json:"step"`
	Phase      string `json:"phase"`
	Outcome    string `json:"outcome"`
	DetailHash string `json:"detail_hash,omitempty"`
	Reasons    string `json:"reasons,omitempty"`
}

type Verification struct {
	TestCmd      string `json:"test_cmd"`
	FinalOutcome string `json:"final_outcome"`
	OutputHash   string `json:"output_hash"`
	DurationMs   int64  `json:"duration_ms"`
}

// Delivery is filled in two stages: PRURL/OpenedAt when the PR opens, and the
// approver fields only once a merge is observed. Because a sealed record is
// never mutated, the approver arrives as a separate chained record; the fields
// here stay empty on the original.
type Delivery struct {
	PRURL       string `json:"pr_url"`
	OpenedAt    string `json:"opened_at"`
	ApprovedBy  string `json:"approved_by"`
	MergedAt    string `json:"merged_at"`
	MergeCommit string `json:"merge_commit"`
}

type Signature struct {
	Alg string `json:"alg"`
	Key string `json:"key"`
	Sig string `json:"sig"` // base64
}
