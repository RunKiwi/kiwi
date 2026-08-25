package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
func inferInstallStep(dir string) *installStep { return installStepFor(dir, true) }

// installStepFor works out how to fetch a repository's dependencies, or returns
// nil when it declares none.
//
// `locked` selects between the two situations in which this runs:
//
//	locked=true   before the loop — install exactly what the lockfile pins
//	locked=false  after the Actor edited a manifest — resolve and re-lock
//
// The distinction is not cosmetic. `npm ci` deletes node_modules and installs
// strictly from package-lock.json, and it *fails outright* when the lock and
// package.json disagree — which is precisely the state the Actor creates by
// adding a dependency. The same is true of every `--frozen-lockfile` flag. So a
// re-install after an edit has to be the resolving form, which also updates the
// lockfile so the change is committed complete.
func installStepFor(dir string, locked bool) *installStep {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	// pick returns the frozen command before the loop, the resolving one after.
	pick := func(frozen, resolving string) string {
		if locked {
			return frozen
		}
		return resolving
	}

	switch {
	// JavaScript: the lockfile names the package manager.
	case has("pnpm-lock.yaml"):
		return &installStep{pick("pnpm install --frozen-lockfile", "pnpm install"), "pnpm-lock.yaml"}
	case has("yarn.lock"):
		return &installStep{pick("yarn install --frozen-lockfile", "yarn install"), "yarn.lock"}
	case has("package-lock.json"):
		// --legacy-peer-deps on the frozen (pre-loop) install only: npm 7+
		// makes a peer-dependency conflict already baked into the committed
		// lockfile a hard ERESOLVE error instead of the npm 6 warning. Real
		// repositories hit this constantly (a lockfile pinned before a peer's
		// major bump), and it fails Phase A outright with no way for the
		// Implementer to route around it — the flag restores the
		// warn-and-continue behaviour npm's own error message recommends.
		// The resolving form (after the Actor edits package.json) stays
		// plain: it writes the lockfile the PR commits, and that file must
		// resolve under an ordinary `npm ci` on the user's own machine.
		return &installStep{pick("npm ci --legacy-peer-deps", "npm install"), "package-lock.json"}
	case has("package.json"):
		// No lockfile to be reproducible against, so a plain install is the
		// only option; `npm ci` would refuse outright. Same split as above:
		// the flag only unblocks the initial install, not the one that
		// writes the lockfile the PR ships.
		return &installStep{pick("npm install --legacy-peer-deps", "npm install"), "package.json"}

	case has("go.mod"):
		// `go mod download` fetches what go.mod already names but does not add a
		// requirement the code now imports, nor write the go.sum hashes for it.
		// `go mod tidy` does both, which is what an edited go.mod needs.
		return &installStep{pick("go mod download", "go mod tidy"), "go.mod"}

	// Python: most specific tool first, since a project using poetry or pipenv
	// also has a pyproject.toml that pip alone would handle differently.
	case has("poetry.lock"):
		return &installStep{pick("poetry install --no-interaction", "poetry lock --no-update && poetry install --no-interaction"), "poetry.lock"}
	case has("Pipfile.lock"):
		return &installStep{pick("pipenv install --dev --deploy", "pipenv install --dev"), "Pipfile.lock"}
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

// manifestFingerprint summarises every dependency manifest present in the
// worktree root, so an edit the Actor makes mid-loop can be detected by
// comparing it against the fingerprint taken after the last install.
//
// Content-hashed rather than mtime-based: the Actor rewrites whole files, so a
// no-op rewrite would bump the mtime and trigger a pointless five-minute
// re-install. Missing files are simply absent from the hash, so deleting a
// manifest registers as a change too.
func manifestFingerprint(dir string) string {
	names := make([]string, 0, len(manifestFiles))
	for name := range manifestFiles {
		names = append(names, name)
	}
	sort.Strings(names) // map order must not change the fingerprint

	h := sha256.New()
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s:%d:", name, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}
