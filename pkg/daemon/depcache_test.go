package daemon

import (
	"strings"
	"testing"
)

// npm was the misleading case: node_modules lives inside the project directory,
// so it survives the install container for free, and an npm-only test made the
// whole two-phase design look correct. Go does not behave that way — modules
// land in /go/pkg/mod, which dies with the container — so `go mod download`
// succeeded and `go build` still reported "network is unreachable".
func TestDepCache_GoModulesAreRedirectedSomewhereDurable(t *testing.T) {
	dc := depCacheFor(ecoGo, t.TempDir())
	if dc == nil {
		t.Fatal("Go needs a durable cache; its modules do not live in the project directory")
	}
	if len(dc.Mounts) != 1 {
		t.Fatalf("expected one mount, got %v", dc.Mounts)
	}
	host, container, _ := strings.Cut(dc.Mounts[0], ":")
	if host == "" || !strings.HasPrefix(container, containerCacheRoot) {
		t.Errorf("mount %q is not host:container under %s", dc.Mounts[0], containerCacheRoot)
	}
	var got string
	for _, e := range dc.Env {
		if strings.HasPrefix(e, "GOMODCACHE=") {
			got = strings.TrimPrefix(e, "GOMODCACHE=")
		}
	}
	if got != container {
		t.Errorf("GOMODCACHE is %q but the mount lands at %q — the toolchain would still write to the container", got, container)
	}
}

// Every ecosystem whose packages live outside the project directory needs the
// same treatment, and each has its own variable.
func TestDepCache_EachToolchainGetsItsOwnVariable(t *testing.T) {
	cases := map[ecosystem]string{
		ecoGo:     "GOMODCACHE=",
		ecoRust:   "CARGO_HOME=",
		ecoRuby:   "BUNDLE_PATH=",
		ecoGradle: "GRADLE_USER_HOME=",
		ecoMaven:  "MAVEN_ARGS=",
		ecoPython: "PIP_TARGET=",
	}
	base := t.TempDir()
	for eco, prefix := range cases {
		t.Run(string(eco), func(t *testing.T) {
			dc := depCacheFor(eco, base)
			if dc == nil {
				t.Fatalf("%s has no durable cache", eco)
			}
			found := false
			for _, e := range dc.Env {
				if strings.HasPrefix(e, prefix) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s is missing %s (env: %v)", eco, prefix, dc.Env)
			}
		})
	}
}

// Python needs both: PIP_TARGET to install somewhere durable, and PYTHONPATH so
// what was installed there is importable. Either alone is useless.
func TestDepCache_PythonNeedsTargetAndPath(t *testing.T) {
	dc := depCacheFor(ecoPython, t.TempDir())
	joined := strings.Join(dc.Env, " ")
	if !strings.Contains(joined, "PIP_TARGET=") || !strings.Contains(joined, "PYTHONPATH=") {
		t.Errorf("python needs PIP_TARGET and PYTHONPATH, got %v", dc.Env)
	}
}

// node_modules and vendor/ are inside the project directory, which is already
// mounted, so wiring a cache for them would be pure overhead.
func TestDepCache_ProjectDirectoryEcosystemsNeedNothing(t *testing.T) {
	for _, eco := range []ecosystem{ecoNode, ecoPHP} {
		if dc := depCacheFor(eco, t.TempDir()); dc != nil {
			t.Errorf("%s installs into the project directory and needs no cache, got %+v", eco, dc)
		}
	}
}

// The cache must never land inside the worktree: delivery runs `git add -A`, so
// it would be committed straight into the user's pull request.
func TestDepCache_LivesOutsideTheWorktree(t *testing.T) {
	base := t.TempDir()
	dc := depCacheFor(ecoGo, base)
	host, _, _ := strings.Cut(dc.Mounts[0], ":")
	if strings.Contains(host, "worktrees") {
		t.Errorf("cache path %q is inside the worktree; it would be committed by `git add -A`", host)
	}
	if !strings.HasPrefix(host, base) {
		t.Errorf("cache path %q is not under the daemon cache dir %q", host, base)
	}
}

func TestDepCache_NoBaseDirIsSafe(t *testing.T) {
	if dc := depCacheFor(ecoGo, ""); dc != nil {
		t.Errorf("without a cache dir there is nothing to mount, got %+v", dc)
	}
}
