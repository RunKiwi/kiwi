// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package provisioner

import (
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
)

// argValue returns the value following flag in a docker argument list.
func argValues(args []string, flag string) []string {
	var out []string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			out = append(out, args[i+1])
		}
	}
	return out
}

// The bug this pins cost every free-tier task ever run.
//
// The daemon writes a worktree inside its own container, then asks the HOST
// docker daemon to bind mount that path into a sandbox. The host resolves the
// path against its own filesystem, so the path has to mean the same thing on
// both sides. It previously did not — /tmp/kiwi-cache was a named volume,
// visible only inside the daemon container — so the host created an empty
// directory at that path and mounted that instead. Every test command ran
// against an empty workspace and reported "reached max steps without passing".
func TestLaunchArgs_CacheMountIsIdenticalOnBothSides(t *testing.T) {
	args := launchArgs("kiwi-free-org-o1", "img", "o1", auth.SharedFreeFleet, "tok", "https://api", 0.50, false)

	var cacheMount string
	for _, v := range argValues(args, "-v") {
		if strings.Contains(v, dockerSocket) {
			continue
		}
		cacheMount = v
	}
	if cacheMount == "" {
		t.Fatal("no cache mount at all")
	}

	host, container, ok := strings.Cut(cacheMount, ":")
	if !ok {
		t.Fatalf("mount %q is not host:container — a named volume cannot be resolved by the host", cacheMount)
	}
	if !strings.HasPrefix(host, "/") {
		t.Errorf("mount source %q is not a host path; a named volume does not exist on the host filesystem", host)
	}
	if host != container {
		t.Errorf("cache mount must be identical on both sides, got host=%q container=%q — "+
			"a sibling sandbox resolves this path against the host", host, container)
	}
}

// The daemon has to be told to use the mounted directory; the default
// (/tmp/kiwi-cache) is inside the container and invisible to sibling sandboxes.
func TestLaunchArgs_DaemonIsPointedAtTheMountedCache(t *testing.T) {
	args := launchArgs("n", "img", "o1", auth.SharedFreeFleet, "tok", "https://api", 0.50, false)

	dirs := argValues(args, "-cache-dir")
	if len(dirs) != 1 {
		t.Fatalf("expected exactly one -cache-dir, got %v", dirs)
	}
	mounts := argValues(args, "-v")
	found := false
	for _, m := range mounts {
		if m == dirs[0]+":"+dirs[0] {
			found = true
		}
	}
	if !found {
		t.Errorf("-cache-dir %q is not the mounted path (mounts: %v)", dirs[0], mounts)
	}
}

// One tenant's checkouts must not land in another's directory.
func TestLaunchArgs_CachePathIsPerOrg(t *testing.T) {
	a := cacheDirFor("org_a")
	b := cacheDirFor("org_b")
	if a == b {
		t.Fatalf("orgs share a cache path: %q", a)
	}
	if !strings.Contains(a, "org_a") {
		t.Errorf("cache path %q does not identify the org", a)
	}
}

func TestLaunchArgs_HostRootIsOverridable(t *testing.T) {
	t.Setenv("KIWI_HOST_CACHE_ROOT", "/data/kiwi")
	if got := cacheDirFor("o1"); got != "/data/kiwi/o1" {
		t.Errorf("got %q, want /data/kiwi/o1", got)
	}
}

// The rest of the launch contract must survive the refactor.
func TestLaunchArgs_KeepsExistingBehaviour(t *testing.T) {
	args := launchArgs("kiwi-free-org-o1", "reg/kiwidaemon:latest", "o1", auth.SharedFreeFleet, "tok", "https://api", 2.00, true)
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"--pull=always",                   // a moving tag must be re-fetched
		"KIWI_SANDBOX_RUNTIME=runsc",      // free work runs under gVisor
		"KIWI_JOIN_TOKEN=tok",             // single-use registration secret
		"KIWI_SESSION_BUDGET_USD=2.00",    // the org's own cap, not the binary's $5 default
		dockerSocket + ":" + dockerSocket, // sandboxes are sibling containers
		"-api-url https://api",            // daemons must reach the public CP
		"reg/kiwidaemon:latest",           // the image itself
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("launch args lost %q\ngot: %s", want, joined)
		}
	}
}

// gVisor is only for the shared free fleet; a dedicated fleet's daemon must not
// silently acquire it.
func TestLaunchArgs_RunscOnlyForTheFreeFleet(t *testing.T) {
	args := launchArgs("n", "img", "o1", "dedicated-fleet", "tok", "https://api", 0.50, false)
	if strings.Contains(strings.Join(args, " "), "runsc") {
		t.Error("runsc was applied to a non-free fleet")
	}
}
