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

// Telemetry credentials (Datadog/Prometheus) must be withheld from the sandbox
// test-command environment for the same reason LLM keys are: the sandbox runs
// model-generated code the org did not write. This must hold both in the
// credentials-opt-in path (sessionMode=false) and in the session opt-in path
// (sessionMode=true with KIWI_SESSION_ALLOW_TEST_CREDS set) — a telemetry key
// must never reach the sandbox regardless of which opt-in let other
// credentials through.
func TestTaskTestEnvExcludesTelemetryCredentials(t *testing.T) {
	creds := map[string]string{
		anthropicKeyName:          "llm-secret",
		"GIT_TOKEN":               "git-secret",
		"DATADOG_API_KEY":         "dd-secret",
		"DATADOG_APP_KEY":         "dd-app-secret",
		"PROMETHEUS_BASE_URL":     "https://prom.internal",
		"PROMETHEUS_BEARER_TOKEN": "prom-secret",
		"SOME_APP_CONFIG":         "not-a-secret-should-pass-through",
	}

	excluded := []string{
		anthropicKeyName, "DATADOG_API_KEY", "DATADOG_APP_KEY",
		"PROMETHEUS_BASE_URL", "PROMETHEUS_BEARER_TOKEN",
	}

	t.Run("credentials opt-in (sessionMode=false)", func(t *testing.T) {
		env := taskTestEnv("do the thing", creds, false)
		for _, k := range excluded {
			if envHas(env, k) {
				t.Errorf("taskTestEnv leaked %s into the sandbox test environment", k)
			}
		}
		if !envHas(env, "SOME_APP_CONFIG") {
			t.Error("taskTestEnv dropped a non-credential config value it should have passed through")
		}
	})

	t.Run("session opt-in (sessionMode=true, KIWI_SESSION_ALLOW_TEST_CREDS=true)", func(t *testing.T) {
		t.Setenv("KIWI_SESSION_ALLOW_TEST_CREDS", "true")
		env := taskTestEnv("do the thing", creds, true)
		for _, k := range excluded {
			if envHas(env, k) {
				t.Errorf("taskTestEnv leaked %s into a session sandbox test environment", k)
			}
		}
		if !envHas(env, "SOME_APP_CONFIG") {
			t.Error("taskTestEnv dropped a non-credential config value it should have passed through")
		}
	})
}

// The Slack webhook URL (Task 12's notifySlackVerdict credential) is a
// Control-Plane-only notification target, never read by the daemon or the
// sandbox. But SealCredentialsForDaemon bundles every org credential
// regardless of Kind, so without an explicit exclusion here it would ride
// along in the daemon's decrypted credential map and leak into the sandbox
// test-command environment exactly like an unexcluded telemetry key would.
func TestTaskTestEnvExcludesSlackWebhookCredential(t *testing.T) {
	creds := map[string]string{
		anthropicKeyName:    "llm-secret",
		"GIT_TOKEN":         "git-secret",
		"SLACK_WEBHOOK_URL": "https://hooks.slack.example/secret",
		"SOME_APP_CONFIG":   "not-a-secret-should-pass-through",
	}

	t.Run("credentials opt-in (sessionMode=false)", func(t *testing.T) {
		env := taskTestEnv("do the thing", creds, false)
		if envHas(env, "SLACK_WEBHOOK_URL") {
			t.Error("taskTestEnv leaked SLACK_WEBHOOK_URL into the sandbox test environment")
		}
		if !envHas(env, "SOME_APP_CONFIG") {
			t.Error("taskTestEnv dropped a non-credential config value it should have passed through")
		}
	})

	t.Run("session opt-in (sessionMode=true, KIWI_SESSION_ALLOW_TEST_CREDS=true)", func(t *testing.T) {
		t.Setenv("KIWI_SESSION_ALLOW_TEST_CREDS", "true")
		env := taskTestEnv("do the thing", creds, true)
		if envHas(env, "SLACK_WEBHOOK_URL") {
			t.Error("taskTestEnv leaked SLACK_WEBHOOK_URL into a session sandbox test environment")
		}
		if !envHas(env, "SOME_APP_CONFIG") {
			t.Error("taskTestEnv dropped a non-credential config value it should have passed through")
		}
	})
}
