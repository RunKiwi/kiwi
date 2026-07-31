package daemon

import (
	"strings"
	"testing"
)

// "update the sirupsen/logrus dependency to latest" ran six Actor steps and
// spent the whole budget. It could never have worked: the install phase fetches
// what go.mod asks for *before* the Actor runs, so a version the Actor adds
// afterwards was never downloaded, and verification has no network to fetch it.
// go.sum makes it starker — its hashes cannot be computed without the module.
func TestDependencyChange_ManifestTargetIsRefusedWithAReason(t *testing.T) {
	why := dependencyChangeBlocked([]string{"go.mod"})
	if why == "" {
		t.Fatal("a task targeting go.mod cannot be verified offline and must say so")
	}
	if !strings.Contains(why, "go.mod") {
		t.Errorf("the reason should name the file, got %q", why)
	}
	if !strings.Contains(why, "network") {
		t.Errorf("the reason should explain the constraint, got %q", why)
	}
	// Actionable, not just accurate.
	if !strings.Contains(why, "go get") && !strings.Contains(why, "locally") {
		t.Errorf("the reason should tell the user what to do instead, got %q", why)
	}
}

func TestDependencyChange_CoversEveryEcosystemsManifest(t *testing.T) {
	for _, f := range []string{
		"go.mod", "go.sum",
		"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"requirements.txt", "pyproject.toml", "poetry.lock", "Pipfile.lock",
		"Cargo.toml", "Cargo.lock",
		"Gemfile", "Gemfile.lock",
		"composer.json", "composer.lock",
		"pom.xml", "build.gradle", "build.gradle.kts",
	} {
		if dependencyChangeBlocked([]string{f}) == "" {
			t.Errorf("%s is a dependency manifest but was not caught", f)
		}
	}
}

// The manifest is matched by base name, so a nested module is caught too.
func TestDependencyChange_MatchesNestedManifests(t *testing.T) {
	if dependencyChangeBlocked([]string{"services/api/go.mod"}) == "" {
		t.Error("a nested go.mod is still a dependency manifest")
	}
}

// The costly error is the other direction: refusing ordinary code changes.
func TestDependencyChange_OrdinaryFilesAreUntouched(t *testing.T) {
	for _, files := range [][]string{
		{"src/components/Footer.tsx"},
		{"main.go", "handler.go"},
		{"README.md"},
		{"internal/gomodutil/parse.go"}, // contains "gomod" but is not go.mod
		{},
	} {
		if why := dependencyChangeBlocked(files); why != "" {
			t.Errorf("%v was wrongly refused: %s", files, why)
		}
	}
}

// One manifest among several targets still blocks: the build breaks for every
// worker in that job either way.
func TestDependencyChange_CaughtAmongOtherFiles(t *testing.T) {
	if dependencyChangeBlocked([]string{"main.go", "go.mod"}) == "" {
		t.Error("a manifest anywhere in the target set must be caught")
	}
}
