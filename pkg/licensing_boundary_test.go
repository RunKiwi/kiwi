package pkg_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The repository is split across two licences: everything outside ee/ is
// Apache-2.0, and ee/ is Business Source License 1.1.
//
// That split only means something if the dependency arrow points one way.
// Apache-2.0 code importing ee/ would drag BSL terms into packages we tell
// people are Apache-2.0 — and the people most affected are BYOC customers,
// whose legal review reads exactly this boundary before letting the daemon into
// their cloud. A single import added in good faith is enough to break it, and
// nothing about the build would complain.
//
// So the boundary is a test. It is deliberately not a lint or a comment.

// ossBinaries are the commands we distribute as Apache-2.0. cmd/kiwidaemon is
// the one that matters most: it is the binary a customer runs on their own
// hardware.
var ossBinaries = []string{
	"./cmd/kiwidaemon",
	"./cmd/kiwi",
	"./cmd/kiwi-agent",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func TestApacheLicensedBinariesDoNotDependOnEE(t *testing.T) {
	for _, bin := range ossBinaries {
		t.Run(strings.TrimPrefix(bin, "./cmd/"), func(t *testing.T) {
			cmd := exec.Command("go", "list", "-deps", bin)
			// This test runs with the package directory as its working
			// directory; the binaries are named relative to the repo root.
			cmd.Dir = repoRoot(t)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("go list -deps %s: %v", bin, err)
			}
			for dep := range strings.SplitSeq(string(out), "\n") {
				if strings.Contains(dep, "ibreakthecloud/kiwi/ee/") {
					t.Errorf("%s depends on %s\n\n"+
						"That package is BSL-licensed (see ee/LICENSE), so this binary can no "+
						"longer be distributed as Apache-2.0. Either move the code you need out "+
						"of ee/, or introduce an interface in the Apache-2.0 package and let ee/ "+
						"supply the implementation.", bin, dep)
				}
			}
		})
	}
}

// The same guarantee at the package level, so a violation is caught in the
// library that introduced it rather than only in whichever binary links it.
func TestApacheLicensedPackagesDoNotImportEE(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "ee", ".git", "node_modules", "vendor", ".next":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// This file names ee/ packages in order to look for them.
		if filepath.Base(path) == "licensing_boundary_test.go" {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(src), `ibreakthecloud/kiwi/ee/`) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s is outside ee/ but imports an ee/ package.\n\n"+
				"Apache-2.0 code cannot depend on BSL code without making it BSL too.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// Every file under ee/ must say so. A file without the marker is one a reader —
// or an automated licence scanner — will take for Apache-2.0.
func TestEveryEEFileCarriesItsLicenceHeader(t *testing.T) {
	eeRoot, err := filepath.Abs("../ee")
	if err != nil {
		t.Fatalf("resolve ee root: %v", err)
	}

	err = filepath.WalkDir(eeRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if !strings.Contains(string(src), "SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1") {
			rel, _ := filepath.Rel(eeRoot, path)
			t.Errorf("ee/%s has no SPDX licence header; it will be read as Apache-2.0", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
