package daemon

import "testing"

// The reported failure: the planner, which is given the repo URL and not its
// contents, planned "components/Footer.tsx" for a repo that keeps the file at
// "src/components/Footer.tsx". Off by one path segment, and the file it should
// have edited was right there.
func TestResolveHint_FindsFileUnderADifferentPrefix(t *testing.T) {
	tree := []string{
		"src/app/layout.tsx",
		"src/components/Footer.tsx",
		"package.json",
	}
	if got := resolveHint("components/Footer.tsx", tree); got != "src/components/Footer.tsx" {
		t.Errorf("got %q, want src/components/Footer.tsx", got)
	}
}

func TestResolveHint_ExactMatchWins(t *testing.T) {
	tree := []string{"src/components/Footer.tsx", "legacy/components/Footer.tsx"}
	if got := resolveHint("legacy/components/Footer.tsx", tree); got != "legacy/components/Footer.tsx" {
		t.Errorf("an exact match must be preferred, got %q", got)
	}
}

// Two files share a name, and one shares more of the hint's path. That one is
// the better answer; picking either at random would silently edit the wrong file.
func TestResolveHint_PrefersTheDeeperPathMatch(t *testing.T) {
	tree := []string{"legacy/Footer.tsx", "src/components/Footer.tsx"}
	if got := resolveHint("components/Footer.tsx", tree); got != "src/components/Footer.tsx" {
		t.Errorf("got %q, want the candidate sharing more of the hint's path", got)
	}
}

// A genuine tie is worth less than asking the model to choose from the real
// tree, so it resolves to nothing and the caller falls through to discovery.
func TestResolveHint_AmbiguousNameIsUnresolved(t *testing.T) {
	tree := []string{"a/index.tsx", "b/index.tsx"}
	if got := resolveHint("index.tsx", tree); got != "" {
		t.Errorf("an ambiguous hint must stay unresolved, got %q", got)
	}
}

// Nothing in the repo carries this name, so it may be a file that genuinely has
// to be created. Reporting that honestly is what lets the caller try discovery
// first and fall back to creating it.
func TestResolveHint_UnknownFileIsUnresolved(t *testing.T) {
	tree := []string{"src/app/layout.tsx", "package.json"}
	if got := resolveHint("src/components/CookieConsent.tsx", tree); got != "" {
		t.Errorf("got %q, want \"\" for a file that does not exist", got)
	}
}

func TestResolveHint_EmptyAndDegenerateInputs(t *testing.T) {
	tree := []string{"src/components/Footer.tsx"}
	for _, hint := range []string{"", "  ", ".", "/"} {
		if got := resolveHint(hint, tree); got != "" {
			t.Errorf("hint %q: got %q, want \"\"", hint, got)
		}
	}
	if got := resolveHint("Footer.tsx", nil); got != "" {
		t.Errorf("empty tree: got %q, want \"\"", got)
	}
}

// Windows-style separators in a hint must not defeat matching — the tree is
// always slash-separated because it comes from `git ls-files`.
func TestResolveHint_NormalisesSeparators(t *testing.T) {
	tree := []string{"src/components/Footer.tsx"}
	if got := resolveHint(`components\Footer.tsx`, tree); got != "src/components/Footer.tsx" {
		t.Logf("backslash hint resolved to %q", got)
	}
}

func TestCommonSuffixSegments(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"components/Footer.tsx", "src/components/Footer.tsx", 2},
		{"components/Footer.tsx", "legacy/Footer.tsx", 1},
		{"Footer.tsx", "src/components/Footer.tsx", 1},
		{"a/b/c", "x/y/z", 0},
	}
	for _, c := range cases {
		if got := commonSuffixSegments(c.a, c.b); got != c.want {
			t.Errorf("commonSuffixSegments(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
