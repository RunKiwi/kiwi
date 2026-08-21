// pkg/daemon/investigation_test.go
package daemon

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/session"
)

func TestInvestigationOutcomeMatchesOnNoDiffExpectedSuccess(t *testing.T) {
	res := session.Result{Success: true, NoDiffExpected: true, Summary: "Root cause: nil check missing in auth.go."}
	out, matched := investigationOutcome(res)
	if !matched {
		t.Fatal("expected a match for a successful, no-diff-expected result")
	}
	if !out.ok || out.prURL != "" || out.detail != "Root cause: nil check missing in auth.go." {
		t.Fatalf("got %+v", out)
	}
}

func TestInvestigationOutcomeDoesNotMatchAnOrdinarySuccess(t *testing.T) {
	res := session.Result{Success: true, NoDiffExpected: false, Summary: "Fixed it."}
	if _, matched := investigationOutcome(res); matched {
		t.Fatal("an ordinary (diff-expected) success must fall through to the normal PR-publish path, not be short-circuited here")
	}
}

func TestInvestigationOutcomeDoesNotMatchAFailure(t *testing.T) {
	res := session.Result{Success: false, NoDiffExpected: true}
	if _, matched := investigationOutcome(res); matched {
		t.Fatal("a failed round must never be reported as the investigation-only success path, whatever NoDiffExpected says")
	}
}
