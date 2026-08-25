package sandbox

import (
	"strings"
	"testing"
)

// The outage this file exists for.
//
// Environment used to reach the container through a temp `--env-file`, written
// one KEY=VALUE per line. That format cannot represent a multi-line value, and
// TASK carries the planner's description — which is routinely several numbered
// steps. Docker rejected the whole file:
//
//	docker: --env-file: invalid env file: variable '1. Inspect the repo structure.' contains whitespaces
//
// The container never started. RunCommand saw a non-zero exit from docker
// itself and reported it as a failing test, identically every time, so the loop
// stalled after three repeats. No test command could pass — changing it from
// `npm run build` to `echo "passed"` made no difference, because the test was
// never the thing failing.
func TestBuildDockerArgs_MultiLineEnvValueSurvives(t *testing.T) {
	task := "Add a cookie consent popup.\n1. Inspect the repo structure.\n2. Add the dependency."

	args, envFile, err := buildDockerArgs("/tmp/x", "echo hi", []string{"TASK=" + task}, &SandboxConfig{}, "alpine", "kiwi-sbx-test")
	if err != nil {
		t.Fatalf("buildDockerArgs: %v", err)
	}
	if envFile != "" {
		t.Errorf("an env file is no longer used; got %q", envFile)
	}

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--env-file") {
		t.Error("--env-file cannot represent a multi-line value and must not be used")
	}
	if !containsPair(args, "-e", "TASK") {
		t.Errorf("TASK should be passed by name, got %v", args)
	}
}

// The property the env file was originally chosen for, which the fix must keep:
// a credential must never appear in the process table, where `ps` on a shared
// host would show it to another tenant.
func TestBuildDockerArgs_SecretValuesNeverEnterArgv(t *testing.T) {
	secret := "sk-ant-super-secret-value"

	args, _, err := buildDockerArgs("/tmp/x", "echo hi",
		[]string{"ANTHROPIC_API_KEY=" + secret, "GITHUB_TOKEN=ghp_hunter2"},
		&SandboxConfig{}, "alpine", "kiwi-sbx-test")
	if err != nil {
		t.Fatalf("buildDockerArgs: %v", err)
	}

	for _, a := range args {
		if strings.Contains(a, secret) || strings.Contains(a, "ghp_hunter2") {
			t.Fatalf("a credential value reached argv: %q", a)
		}
	}
	// The names are expected — they are what tells docker which variables to
	// inherit from the CLI's own environment.
	if !containsPair(args, "-e", "ANTHROPIC_API_KEY") || !containsPair(args, "-e", "GITHUB_TOKEN") {
		t.Errorf("variables should be passed by name, got %v", args)
	}
}

// A malformed entry must be skipped rather than producing a bare `-e` that
// swallows the next argument and corrupts the whole command line.
func TestBuildDockerArgs_SkipsEntriesWithoutAName(t *testing.T) {
	args, _, err := buildDockerArgs("/tmp/x", "echo hi",
		[]string{"NO_EQUALS_SIGN", "=novalue", "GOOD=1"}, &SandboxConfig{}, "alpine", "kiwi-sbx-test")
	if err != nil {
		t.Fatalf("buildDockerArgs: %v", err)
	}
	if !containsPair(args, "-e", "GOOD") {
		t.Errorf("the well-formed entry should survive, got %v", args)
	}
	for i, a := range args {
		if a == "-e" && i+1 < len(args) {
			switch args[i+1] {
			case "NO_EQUALS_SIGN", "", "=novalue":
				t.Errorf("malformed entry %q was passed to docker", args[i+1])
			}
		}
	}
}

// Values containing an equals sign are common (base64, connection strings) and
// must not be truncated at the first one.
func TestBuildDockerArgs_ValuesWithEqualsAreNotSplit(t *testing.T) {
	args, _, err := buildDockerArgs("/tmp/x", "echo hi",
		[]string{"DSN=host=db user=postgres password=a=b=c"}, &SandboxConfig{}, "alpine", "kiwi-sbx-test")
	if err != nil {
		t.Fatalf("buildDockerArgs: %v", err)
	}
	if !containsPair(args, "-e", "DSN") {
		t.Errorf("expected -e DSN, got %v", args)
	}
	for _, a := range args {
		if strings.Contains(a, "password=") {
			t.Errorf("the value leaked into argv: %q", a)
		}
	}
}

// No environment means no -e flags at all.
func TestBuildDockerArgs_EmptyEnvAddsNothing(t *testing.T) {
	args, _, err := buildDockerArgs("/tmp/x", "echo hi", nil, &SandboxConfig{}, "alpine", "kiwi-sbx-test")
	if err != nil {
		t.Fatalf("buildDockerArgs: %v", err)
	}
	for _, a := range args {
		if a == "-e" {
			t.Errorf("unexpected -e with no environment: %v", args)
		}
	}
}

func containsPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
