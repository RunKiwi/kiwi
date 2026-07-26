package ver

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// TaskEvent is one persisted phase of a worker's Actor–Critic loop, as reported
// by the daemon that ran it. It mirrors the orchestrator's telemetry row; the
// type is restated here so this package stays a leaf (stdlib + pkg/crypto) and
// can be imported from the daemon and an offline verifier alike.
// The json tags are load-bearing: this type crosses the daemon→Control Plane
// wire AND forms part of the daemon's signed attestation payload, so both sides
// must serialize it identically.
type TaskEvent struct {
	Step         int     `json:"step"`
	Phase        string  `json:"phase"`
	Outcome      string  `json:"outcome"`
	Detail       string  `json:"detail"`
	DurationMs   int64   `json:"duration_ms"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// WorkerInput describes one planned worker and everything observed about its
// execution. The caller maps its own storage types onto this.
type WorkerInput struct {
	WorkerID    string
	DependsOn   []string
	ActorModel  string
	CriticModel string
	Provider    string
	Events      []TaskEvent
}

// AssembleInput is the complete set of facts a record is built from. Every
// field is supplied by the caller; this package invents nothing.
//
// Fields the caller cannot observe must be left empty. They serialize as
// "unknown" markers rather than plausible-looking defaults — a record that
// guesses is worse than one that admits a gap, because the signature makes
// every field read as attested fact.
type AssembleInput struct {
	OrgID string
	JobID string

	Task           string
	Source         string
	SubmittedBy    string
	SubmittedAtRFC string
	IdempotencyKey string

	PlanManifestHash string
	PlanSummary      string
	PlannerModel     string
	PlannerProvider  string
	ReferenceMode    string
	ReferenceJobIDs  []string

	Repo         string
	Ref          string
	BaseCommit   string
	HeadCommit   string
	FilesTouched []string

	FleetID      string
	Mode         string // "managed" | "byoc" — observed, never assumed
	DaemonID     string
	DaemonPubKey string
	SandboxRT    string // "docker" | "runsc" | "firecracker"
	SandboxNet   string

	Workers []WorkerInput

	TestCmd          string
	FinalOutcome     string // "pass" | "fail"
	TestOutputHash   string
	VerifyDurationMs int64

	Delivery Delivery

	PrevRecordHash string
}

// reasonsCap bounds how much Critic text a record carries verbatim.
const reasonsCap = 200

// unknown is the explicit marker for a fact the assembler was not given. It
// exists so a reader can tell "not observed" from "observed as empty".
const unknown = "unknown"

func orUnknown(s string) string {
	if s == "" {
		return unknown
	}
	return s
}

// truncRunes cuts s to at most n bytes without splitting a UTF-8 sequence, so
// the result is always valid UTF-8 and canonicalizes deterministically.
func truncRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.RuneStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	// Drop a trailing partial rune whose continuation bytes were cut away.
	for len(cut) > 0 {
		if r, size := utf8.DecodeLastRuneInString(cut); r == utf8.RuneError && size <= 1 {
			cut = cut[:len(cut)-1]
			continue
		}
		break
	}
	return cut
}

// AssembleRecord builds a Verified Execution Record from the given facts. It is
// pure: same input, same record, no clock and no I/O, so it is directly
// testable and a record can be re-derived and re-hashed at any time.
func AssembleRecord(in AssembleInput) (*Record, error) {
	if in.JobID == "" || in.OrgID == "" {
		return nil, fmt.Errorf("ver: org_id and job_id are required")
	}

	refs := in.ReferenceJobIDs
	if refs == nil {
		refs = []string{}
	}
	files := in.FilesTouched
	if files == nil {
		files = []string{}
	}

	workers := make([]WorkerAttestation, 0, len(in.Workers))
	for _, w := range in.Workers {
		deps := w.DependsOn
		if deps == nil {
			deps = []string{}
		}
		att := WorkerAttestation{
			WorkerID:    w.WorkerID,
			DependsOn:   deps,
			ActorModel:  orUnknown(w.ActorModel),
			CriticModel: orUnknown(w.CriticModel),
			Provider:    orUnknown(w.Provider),
			Steps:       make([]WorkerStep, 0, len(w.Events)),
		}
		for _, ev := range w.Events {
			step := WorkerStep{
				Step:    ev.Step,
				Phase:   ev.Phase,
				Outcome: ev.Outcome,
			}
			if ev.Detail != "" {
				// Always a hash. Raw detail (test output especially) can carry
				// secrets and must never be exported in the record itself.
				step.DetailHash = HashString(ev.Detail)
				// Only the Critic's own prose is quoted, and only briefly: it is
				// model-authored review text, not captured program output.
				if ev.Phase == "critic" {
					step.Reasons = truncRunes(ev.Detail, reasonsCap)
				}
			}
			if ev.Phase == "critic" && ev.Outcome == "rejected" {
				att.CriticRejections++
			}
			att.InputTokens += ev.InputTokens
			att.OutputTokens += ev.OutputTokens
			att.CostUSD += ev.CostUSD
			att.Steps = append(att.Steps, step)
		}
		workers = append(workers, att)
	}
	// Stable order so the canonical form does not depend on map iteration.
	sort.Slice(workers, func(i, j int) bool { return workers[i].WorkerID < workers[j].WorkerID })

	attestation := AttestationUnsigned

	return &Record{
		Ver:            SchemaVersion,
		RecordID:       "ver_" + in.JobID,
		OrgID:          in.OrgID,
		JobID:          in.JobID,
		PrevRecordHash: in.PrevRecordHash,
		Attestation:    attestation,
		Intent: Intent{
			Task:           in.Task,
			Source:         orUnknown(in.Source),
			SubmittedBy:    in.SubmittedBy,
			SubmittedAt:    in.SubmittedAtRFC,
			IdempotencyKey: in.IdempotencyKey,
		},
		GoverningSpec: GoverningSpec{
			PlanManifestHash: orUnknown(in.PlanManifestHash),
			PlanSummary:      in.PlanSummary,
			PlannerModel:     orUnknown(in.PlannerModel),
			PlannerProvider:  orUnknown(in.PlannerProvider),
			ReferenceMode:    orUnknown(in.ReferenceMode),
			ReferenceJobIDs:  refs,
		},
		Subject: Subject{
			Repo:         orUnknown(in.Repo),
			Ref:          orUnknown(in.Ref),
			BaseCommit:   orUnknown(in.BaseCommit),
			HeadCommit:   orUnknown(in.HeadCommit),
			FilesTouched: files,
		},
		Execution: Execution{
			FleetID:      in.FleetID,
			Mode:         orUnknown(in.Mode),
			DaemonID:     in.DaemonID,
			DaemonPubKey: in.DaemonPubKey,
			Sandbox: Sandbox{
				Runtime: orUnknown(in.SandboxRT),
				Network: orUnknown(in.SandboxNet),
			},
			Workers: workers,
		},
		Verification: Verification{
			TestCmd:      in.TestCmd,
			FinalOutcome: orUnknown(in.FinalOutcome),
			OutputHash:   in.TestOutputHash,
			DurationMs:   in.VerifyDurationMs,
		},
		Delivery: in.Delivery,
	}, nil
}
