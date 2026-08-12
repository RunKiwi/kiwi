package daemon

import (
	"strings"
	"testing"
)

func envHas(env []string, name string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			return true
		}
	}
	return false
}

// The pre-existing guarantee: model keys never enter the sandbox, because the
// sandbox runs model-generated code.
func TestSandboxWithCredentialsOptInNeverGetsModelKeys(t *testing.T) {
	creds := map[string]string{
		"GIT_TOKEN":      "ghp_secret",
		"DATABASE_URL":   "postgres://x",
		anthropicKeyName: "sk-ant",
		geminiKeyName:    "gem",
		openaiKeyName:    "sk-oai",
	}

	env := taskTestEnv("do the thing", creds, false)

	if !envHas(env, "GIT_TOKEN") || !envHas(env, "DATABASE_URL") {
		t.Errorf("the credentials opt-in should pass non-model credentials through: %v", env)
	}
	for _, k := range []string{anthropicKeyName, geminiKeyName, openaiKeyName} {
		if envHas(env, k) {
			t.Errorf("%s must never reach the sandbox", k)
		}
	}
}

// The new guarantee. In session mode the model chooses the commands and their
// output travels back into the event log, so a credential in the environment
// has a read-and-echo path out that needs no network at all.
func TestSessionSandboxGetsNoCredentialsAtAll(t *testing.T) {
	creds := map[string]string{
		"GIT_TOKEN":      "ghp_secret",
		"DATABASE_URL":   "postgres://x",
		anthropicKeyName: "sk-ant",
	}

	env := taskTestEnv("do the thing", creds, true)

	for _, k := range []string{"GIT_TOKEN", "DATABASE_URL", anthropicKeyName} {
		if envHas(env, k) {
			t.Errorf("%s must not reach a session sandbox: %v", k, env)
		}
	}
	if !envHas(env, "TASK") {
		t.Error("the task description should still be present")
	}
}

// Repositories whose suites genuinely need a secret can opt back in, and that
// has to be an explicit act rather than a default.
func TestSessionCredentialsCanBeOptedBackIn(t *testing.T) {
	t.Setenv("KIWI_SESSION_ALLOW_TEST_CREDS", "true")

	creds := map[string]string{"DATABASE_URL": "postgres://x", anthropicKeyName: "sk-ant"}
	env := taskTestEnv("do the thing", creds, true)

	if !envHas(env, "DATABASE_URL") {
		t.Errorf("opting in should restore test credentials: %v", env)
	}
	// The model-key exclusion is not part of the opt-in and never has been.
	if envHas(env, anthropicKeyName) {
		t.Error("opting in must not expose model keys")
	}
}

func TestSessionCredentialOptInDefaultsOff(t *testing.T) {
	t.Setenv("KIWI_SESSION_ALLOW_TEST_CREDS", "")
	if sessionAllowsTestCredentials() {
		t.Error("session credentials must default to off")
	}
	t.Setenv("KIWI_SESSION_ALLOW_TEST_CREDS", "nonsense")
	if sessionAllowsTestCredentials() {
		t.Error("an unparseable value must not be read as opt-in")
	}
}
