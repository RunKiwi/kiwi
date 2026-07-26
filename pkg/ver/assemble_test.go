package ver

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func baseInput() AssembleInput {
	return AssembleInput{
		OrgID:          "org_1",
		JobID:          "job_1",
		Task:           "fix divide by zero",
		SubmittedAtRFC: "2026-07-26T09:12:03Z",
		TestCmd:        "go test ./...",
		Mode:           "byoc",
		SandboxRT:      "runsc",
		SandboxNet:     "default-deny",
		FinalOutcome:   "pass",
	}
}

func TestAssembleRequiresIdentity(t *testing.T) {
	in := baseInput()
	in.JobID = ""
	if _, err := AssembleRecord(in); err == nil {
		t.Fatal("expected error when job_id is missing")
	}
}

// A record must never carry raw detail: test output routinely contains secrets,
// so only a hash may cross into the record.
func TestAssembleHashesDetailAndNeverQuotesTestOutput(t *testing.T) {
	secret := "AWS_SECRET_ACCESS_KEY=hunter2 leaked in test output"
	in := baseInput()
	in.Workers = []WorkerInput{{
		WorkerID: "impl",
		Events: []TaskEvent{
			{Step: 1, Phase: "test", Outcome: "fail", Detail: secret},
		},
	}}

	rec, err := AssembleRecord(in)
	if err != nil {
		t.Fatal(err)
	}
	step := rec.Execution.Workers[0].Steps[0]
	if !strings.HasPrefix(step.DetailHash, "sha256:") {
		t.Errorf("expected a sha256 detail hash, got %q", step.DetailHash)
	}
	if step.Reasons != "" {
		t.Errorf("test-phase detail must not be quoted, got %q", step.Reasons)
	}

	b, err := Canonicalize(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hunter2") {
		t.Fatal("raw test output leaked into the canonicalized record")
	}
}

// The Critic's own prose is the one thing quoted, and only briefly.
func TestAssembleQuotesCriticReasonsBounded(t *testing.T) {
	long := strings.Repeat("é", 400) // multi-byte, to catch byte-slicing
	in := baseInput()
	in.Workers = []WorkerInput{{
		WorkerID: "impl",
		Events: []TaskEvent{
			{Step: 1, Phase: "critic", Outcome: "rejected", Detail: long},
			{Step: 2, Phase: "critic", Outcome: "approved", Detail: "looks scoped"},
		},
	}}

	rec, err := AssembleRecord(in)
	if err != nil {
		t.Fatal(err)
	}
	w := rec.Execution.Workers[0]
	if w.CriticRejections != 1 {
		t.Errorf("critic_rejections = %d, want 1", w.CriticRejections)
	}
	got := w.Steps[0].Reasons
	if len(got) > reasonsCap {
		t.Errorf("reasons length %d exceeds cap %d", len(got), reasonsCap)
	}
	if !utf8.ValidString(got) {
		t.Error("truncated reasons is not valid UTF-8 — a rune was split")
	}
}

// Unobservable facts must read as "unknown", never as a plausible default: a
// signed record's fields are read as attested truth.
func TestAssembleMarksUnobservedFactsUnknown(t *testing.T) {
	in := baseInput()
	in.Mode = ""
	in.SandboxRT = ""
	in.Repo = ""

	rec, err := AssembleRecord(in)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Execution.Mode != unknown {
		t.Errorf("Mode = %q, want %q", rec.Execution.Mode, unknown)
	}
	if rec.Execution.Sandbox.Runtime != unknown {
		t.Errorf("Runtime = %q, want %q", rec.Execution.Sandbox.Runtime, unknown)
	}
	if rec.Subject.Repo != unknown {
		t.Errorf("Repo = %q, want %q", rec.Subject.Repo, unknown)
	}
}

// Assembly is pure: identical input must yield an identical hash, or a record
// cannot be re-derived and re-verified later.
func TestAssembleIsDeterministic(t *testing.T) {
	in := baseInput()
	in.Workers = []WorkerInput{
		{WorkerID: "b", Events: []TaskEvent{{Step: 1, Phase: "actor", Outcome: "proposed"}}},
		{WorkerID: "a", Events: []TaskEvent{{Step: 1, Phase: "test", Outcome: "pass"}}},
	}

	r1, err := AssembleRecord(in)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := AssembleRecord(in)
	if err != nil {
		t.Fatal(err)
	}
	h1, err := RecordHash(r1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := RecordHash(r2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("assembly is not deterministic: %s != %s", h1, h2)
	}
	// Workers are sorted, so input order cannot change the record.
	if r1.Execution.Workers[0].WorkerID != "a" {
		t.Errorf("workers not sorted: got %q first", r1.Execution.Workers[0].WorkerID)
	}
}
