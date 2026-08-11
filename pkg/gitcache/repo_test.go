package gitcache

import "testing"

func TestParseRepo(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		want   Repo
		ok     bool
		github bool
	}{
		{
			name:   "https",
			remote: "https://github.com/RunKiwi/kiwi",
			want:   Repo{Host: "github.com", Owner: "RunKiwi", Name: "kiwi"},
			ok:     true, github: true,
		},
		{
			name:   "https with .git",
			remote: "https://github.com/RunKiwi/kiwi.git",
			want:   Repo{Host: "github.com", Owner: "RunKiwi", Name: "kiwi"},
			ok:     true, github: true,
		},
		{
			name:   "https with trailing slash",
			remote: "https://github.com/RunKiwi/kiwi/",
			want:   Repo{Host: "github.com", Owner: "RunKiwi", Name: "kiwi"},
			ok:     true, github: true,
		},
		{
			// A token embedded in the URL must not become part of the host.
			name:   "https with credentials",
			remote: "https://x-access-token:ghs_secret@github.com/RunKiwi/kiwi.git",
			want:   Repo{Host: "github.com", Owner: "RunKiwi", Name: "kiwi"},
			ok:     true, github: true,
		},
		{
			name:   "host case is folded",
			remote: "https://GitHub.com/RunKiwi/kiwi",
			want:   Repo{Host: "github.com", Owner: "RunKiwi", Name: "kiwi"},
			ok:     true, github: true,
		},
		{
			name:   "ssh scp form",
			remote: "git@github.com:RunKiwi/kiwi.git",
			want:   Repo{Host: "github.com", Owner: "RunKiwi", Name: "kiwi"},
			ok:     true, github: true,
		},
		{
			name:   "ssh url form",
			remote: "ssh://git@github.com/RunKiwi/kiwi.git",
			want:   Repo{Host: "github.com", Owner: "RunKiwi", Name: "kiwi"},
			ok:     true, github: true,
		},
		{
			name:   "gitlab is parsed but not github",
			remote: "https://gitlab.com/acme/widgets.git",
			want:   Repo{Host: "gitlab.com", Owner: "acme", Name: "widgets"},
			ok:     true, github: false,
		},
		{
			// GHES needs its own App on a different API base; matching it as
			// github.com would resolve to somebody else's installation.
			name:   "enterprise server is not github.com",
			remote: "https://github.acme-corp.com/acme/widgets.git",
			want:   Repo{Host: "github.acme-corp.com", Owner: "acme", Name: "widgets"},
			ok:     true, github: false,
		},
		{
			name:   "port is stripped from host",
			remote: "https://git.internal:8443/acme/widgets.git",
			want:   Repo{Host: "git.internal", Owner: "acme", Name: "widgets"},
			ok:     true, github: false,
		},
		{name: "empty", remote: "", ok: false},
		{name: "whitespace", remote: "   ", ok: false},
		{name: "local path", remote: "/srv/repos/kiwi", ok: false},
		{name: "relative path", remote: "../kiwi", ok: false},
		{name: "no owner segment", remote: "https://github.com/kiwi", ok: false},
		{name: "bare host", remote: "https://github.com", ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseRepo(tc.remote)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tc.ok, got)
			}
			if !tc.ok {
				return
			}
			if got != tc.want {
				t.Errorf("repo = %+v, want %+v", got, tc.want)
			}
			if got.IsGitHub() != tc.github {
				t.Errorf("IsGitHub() = %v, want %v", got.IsGitHub(), tc.github)
			}
		})
	}
}
