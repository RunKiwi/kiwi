package daemon

import (
	"os"
	"path/filepath"
	"time"
)

// Dependency installation — the networked half of the two-phase sandbox.
//
// Every ecosystem needs a network fetch before its tests can run: npm ci, pip
// install, go mod download, cargo fetch, bundle install. The verification
// sandbox runs with --network none, which is the point of it, so that fetch
// cannot happen there. Getting the image right (see runtime.go) only exposed
// this: `go build` in the correct Go image still fails with "network is
// unreachable" the moment a module is missing.
//
// So the two are separated:
//
//	Phase A — install    network ON,  no credentials, repo's own lockfile
//	Phase B — verify     network OFF, credentials, model-generated code
//
// The security property that matters is preserved and, stated precisely, is
// stronger than "the sandbox has no network": model-generated code never has
// network access, and the phase that does never holds a secret. Phase A runs
// with an empty environment — not the org's git token, not a registry
// credential, nothing — so a malicious postinstall in a dependency can reach
// the network but has nothing to send. It cannot see the LLM keys either, which
// were already withheld from every sandbox.
//
// The trade is explicit: a repository whose dependencies live behind a private
// registry cannot be installed, because supplying that credential to a
// networked container running third-party install hooks is exactly the exposure
// this split exists to avoid. Public dependencies only, for now.

// defaultInstallTimeout bounds phase A. Installing dependencies is slow and
// unbounded in principle — a large npm tree is minutes — but it is not the work
// the user asked for, and on Free the whole task has 600 seconds. Overridable
// with KIWI_INSTALL_TIMEOUT.
const defaultInstallTimeout = 5 * time.Minute

func installTimeout() time.Duration {
	if v := os.Getenv("KIWI_INSTALL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultInstallTimeout
}

// installStep is how a repository's declared dependencies are fetched.
type installStep struct {
	// Command is run in the worktree root, in the same image as the tests.
	Command string
	// Source is the file that decided it, for the task log.
	Source string
}

// inferInstallStep works out how to fetch a repository's dependencies, or
// returns nil when it declares none.
//
// Lockfiles are checked before manifests because a lockfile identifies the
// package manager exactly — yarn.lock and package-lock.json describe the same
// package.json — and because the locked install is the reproducible one.
func inferInstallStep(dir string) *installStep {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}

	switch {
	// JavaScript: the lockfile names the package manager.
	case has("pnpm-lock.yaml"):
		return &installStep{"pnpm install --frozen-lockfile", "pnpm-lock.yaml"}
	case has("yarn.lock"):
		return &installStep{"yarn install --frozen-lockfile", "yarn.lock"}
	case has("package-lock.json"):
		return &installStep{"npm ci", "package-lock.json"}
	case has("package.json"):
		// No lockfile to be reproducible against, so a plain install is the
		// only option; `npm ci` would refuse outright.
		return &installStep{"npm install", "package.json"}

	case has("go.mod"):
		return &installStep{"go mod download", "go.mod"}

	// Python: most specific tool first, since a project using poetry or pipenv
	// also has a pyproject.toml that pip alone would handle differently.
	case has("poetry.lock"):
		return &installStep{"poetry install --no-interaction", "poetry.lock"}
	case has("Pipfile.lock"):
		return &installStep{"pipenv install --dev --deploy", "Pipfile.lock"}
	case has("requirements.txt"):
		return &installStep{"pip install -r requirements.txt", "requirements.txt"}
	case has("pyproject.toml"):
		return &installStep{"pip install -e .", "pyproject.toml"}

	case has("Cargo.toml"):
		return &installStep{"cargo fetch", "Cargo.toml"}
	case has("Gemfile"):
		return &installStep{"bundle install", "Gemfile"}
	case has("composer.json"):
		return &installStep{"composer install --no-interaction", "composer.json"}
	case has("pom.xml"):
		return &installStep{"mvn -q -B dependency:go-offline", "pom.xml"}
	case has("build.gradle"), has("build.gradle.kts"):
		return &installStep{"gradle --no-daemon dependencies", "build.gradle"}
	}

	return nil
}
