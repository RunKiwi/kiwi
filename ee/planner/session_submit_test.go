// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// Session mode makes no LLM call at submit and reads no credential plaintext.
// The second is not a nicety: planning on the Control Plane with the org's
// decrypted key is a containment gap in BYOC, and this closes it.
func TestSessionSubmitCallsNoModelAndDecryptsNothing(t *testing.T) {
	st := newTestStore(t)
	s := NewService(st, nil, nil)
	t.Setenv("KIWI_PLANNER", "llm")
	seedCredential(t, st, "org1", "ANTHROPIC_API_KEY")
	seedRepoAccess(t, st, "org1")

	var completerBuilt bool
	s.newCompleter = func(model string) Completer {
		completerBuilt = true
		return nil
	}

	res, err := s.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "add retries", RepoURL: "https://github.com/a/b", Ref: "main",
		Model: "claude-sonnet-5", ArchitectModel: "claude-opus-4-8",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if completerBuilt {
		t.Error("session mode must not build a planning completer")
	}
	if len(res.TaskIDs) != 1 {
		t.Errorf("expected one task, got %v", res.TaskIDs)
	}
}

// The spec is what the daemon reads, so the architect model has to be on it —
// not only in the manifest. There is no mode key: the single-file loop is gone,
// so a spec that named a loop would be describing a choice that no longer
// exists. The manifest still records one, for records made either side of the
// change to stay comparable.
func TestSessionSpecCarriesArchitectModel(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	seedCredential(t, s.store.(*store.PostgresStore), "org1", "ANTHROPIC_API_KEY")

	res, err := s.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "add retries", Model: "claude-sonnet-5",
		ArchitectModel: "claude-opus-4-8",
	})
	if err != nil {
		t.Fatal(err)
	}

	var task store.QueuedTask
	if err := s.store.DB().First(&task, "id = ?", res.TaskIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := task.Spec["mode"]; ok {
		t.Errorf("spec should carry no mode key, got %v", task.Spec["mode"])
	}
	if task.Spec["architect_model"] != "claude-opus-4-8" {
		t.Errorf("spec architect_model = %v", task.Spec["architect_model"])
	}
}

// Submitting used to fail immediately for an org with no provider key, because
// the Control Plane had to read that key to plan. It no longer reads one — so
// without a presence check the org would get a 202 and a failure minutes later
// inside the daemon.
func TestSessionSubmitStillFailsFastWithoutAProviderKey(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)

	_, err := s.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "add retries", Model: "claude-sonnet-5",
	})
	if err == nil {
		t.Fatal("expected the submit to be refused")
	}
	// Phrased so the dashboard's error mapper still offers the Integrations link.
	if !strings.Contains(err.Error(), "add one in Integrations") {
		t.Errorf("error should point at Integrations, got %q", err)
	}
}

// The presence check must not decrypt: it reads the metadata row only.
func TestProviderKeyCheckIsPresenceOnly(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	seedCredential(t, s.store.(*store.PostgresStore), "org1", "GEMINI_API_KEY")

	if err := s.requireProviderKey(context.Background(), "org1", "gemini"); err != nil {
		t.Errorf("a present key must satisfy the check: %v", err)
	}
	if err := s.requireProviderKey(context.Background(), "org1", "anthropic"); err == nil {
		t.Error("a missing key must fail the check")
	}
}

// An operator needs to be able to turn the mode off across a fleet without a
// deploy rollback.
func TestSessionModeCanBeDisabledByTheOperator(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	t.Setenv("KIWI_SESSION_MODE", "off")
	seedCredential(t, s.store.(*store.PostgresStore), "org1", "ANTHROPIC_API_KEY")

	_, err := s.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "x", Model: "claude-sonnet-5",
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected the kill-switch to refuse the submit, got %v", err)
	}
}

func seedCredential(t *testing.T, s *store.PostgresStore, org, name string) {
	t.Helper()
	if err := s.SaveCredential(context.Background(), org, name, store.CredentialLLM, "sk-test"); err != nil {
		t.Fatal(err)
	}
}

// seedRepoAccess gives an org a way to reach the repository it is submitting.
//
// requireRepoAuth refuses a submit whose repository nothing can clone, which is
// the right behaviour and not what any of these tests are about — without this
// they fail on the repo URL before reaching the thing under test. A GIT_TOKEN is
// the simpler of the two accepted paths; a GitHub App installation is the other,
// and repo_auth_test.go covers both properly.
//
// It is typed CredentialGit rather than reusing seedCredential: the type is not
// what requireRepoAuth looks up, but a git token stored as an LLM credential is
// a lie that the next person to read these tests would have to un-learn.
func seedRepoAccess(t *testing.T, s *store.PostgresStore, org string) {
	t.Helper()
	if err := s.SaveCredential(context.Background(), org, "GIT_TOKEN", store.CredentialGit, "ghp-test"); err != nil {
		t.Fatal(err)
	}
}

// seedAdmissibleOrg gives an org everything admission demands before it will
// look at what a test is actually asserting: a way to clone the repository, and
// a key for the provider serving the model it will run.
//
// The second became universal when the single-file loop was retired. Every
// submit now resolves an Architect, and a BYOK org must hold a key for that
// model's provider — where before, the heuristic path checked nothing at submit
// and let the daemon discover the problem minutes later.
func seedAdmissibleOrg(t *testing.T, s *store.PostgresStore, org string) {
	t.Helper()
	seedRepoAccess(t, s, org)
	seedCredential(t, s, org, "ANTHROPIC_API_KEY")
}
