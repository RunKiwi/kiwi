package daemon

import (
	"os"
	"path/filepath"
)

// Package caches that survive a container.
//
// The install phase and the verification phase are separate `docker run`
// invocations. Only the worktree is bind-mounted, so anything a toolchain
// writes elsewhere in its container filesystem is destroyed when that container
// exits.
//
// npm was misleading here: node_modules lives inside the project directory, so
// it survives for free, and an npm-only test suggested the whole design worked.
// Most ecosystems do not behave that way — Go downloads to /go/pkg/mod, cargo
// to ~/.cargo, bundler to the gem home — so `go mod download` succeeded in
// phase A and `go build` in phase B still reported "network is unreachable".
//
// Each of these is redirected into a directory mounted from the daemon's cache,
// which is itself a host path shared with the sandbox (see the provisioner's
// launch mount). Both phases get the same mount, so what phase A downloads is
// what phase B compiles against.
//
// The cache deliberately lives OUTSIDE the worktree. Delivery runs `git add -A`,
// so a cache inside it would be committed into the user's pull request.

// containerCacheRoot is where caches appear inside a sandbox container.
const containerCacheRoot = "/kiwideps"

// depCache is the mount and environment that redirect one ecosystem's package
// downloads somewhere durable.
type depCache struct {
	Mounts []string // "host:container" specs
	Env    []string // toolchain variables pointing at the mount
}

// depCacheFor returns the cache wiring for an ecosystem, or nil when the
// toolchain already installs into the project directory (node_modules, vendor/)
// and therefore needs nothing.
//
// baseDir is the daemon's cache directory, which must be a path the sandbox's
// docker daemon can resolve — the whole point of the provisioner mounting it at
// an identical path on both sides.
func depCacheFor(eco ecosystem, baseDir string) *depCache {
	if baseDir == "" {
		return nil
	}

	// name is the per-ecosystem directory; container is where it is mounted and
	// what the toolchain variable points at.
	wire := func(name string, env ...string) *depCache {
		host := filepath.Join(baseDir, "deps", name)
		if err := os.MkdirAll(host, 0o777); err != nil {
			// Without the directory the mount would still work (docker creates
			// it) but with root ownership; either way a failure here must not
			// fail the task, so fall back to no cache and let the install run
			// per-task as it did before.
			return nil
		}
		return &depCache{
			Mounts: []string{host + ":" + containerCacheRoot + "/" + name},
			Env:    env,
		}
	}

	dir := containerCacheRoot + "/"
	switch eco {
	case ecoGo:
		return wire("go", "GOMODCACHE="+dir+"go")
	case ecoRust:
		return wire("cargo", "CARGO_HOME="+dir+"cargo")
	case ecoRuby:
		return wire("bundle", "BUNDLE_PATH="+dir+"bundle")
	case ecoGradle:
		return wire("gradle", "GRADLE_USER_HOME="+dir+"gradle")
	case ecoMaven:
		// MAVEN_ARGS is honoured by Maven 3.9+, which the pinned image ships.
		return wire("m2", "MAVEN_ARGS=-Dmaven.repo.local="+dir+"m2")
	case ecoPython:
		// pip installs into site-packages, which is not durable, so packages go
		// to an explicit target and PYTHONPATH makes them importable. This is
		// why the inferred pytest command is `python -m pytest` rather than the
		// `pytest` console script, which would not be on PATH.
		return wire("py", "PIP_TARGET="+dir+"py", "PYTHONPATH="+dir+"py")
	}

	// node/pnpm/yarn (node_modules) and composer (vendor/) install into the
	// project directory, which is already mounted.
	return nil
}
