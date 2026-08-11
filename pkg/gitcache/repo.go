package gitcache

import (
	"net/url"
	"strings"
)

// Repo identifies a remote by the parts that decide how it is authenticated.
type Repo struct {
	Host  string // lower-cased, no port: "github.com"
	Owner string // the user or organisation: "RunKiwi"
	Name  string // the repository, without ".git"
}

// IsGitHub reports whether the remote lives on github.com.
//
// GitHub Enterprise Server is deliberately not matched. Its installations live
// on a different API base and would need their own App, so treating a
// self-hosted host as github.com would resolve it to the wrong installation and
// mint a token for somebody else's repository.
func (r Repo) IsGitHub() bool { return r.Host == "github.com" }

// ParseRepo extracts the host, owner and name from a git remote.
//
// Both forms that reach Kiwi are handled: the HTTPS URLs the dashboard and API
// hand over, and the scp-like SSH form (git@github.com:owner/name.git) that
// people paste out of habit. ok is false for anything else, and callers treat
// that as "not a GitHub remote" rather than as an error, because a remote this
// cannot classify is exactly the case the PAT fallback exists to serve.
func ParseRepo(remote string) (Repo, bool) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return Repo{}, false
	}

	var host, path string
	switch {
	case strings.Contains(remote, "://"):
		u, err := url.Parse(remote)
		if err != nil || u.Host == "" {
			return Repo{}, false
		}
		host, path = u.Hostname(), u.Path
	default:
		// scp-like: [user@]host:path. Reject anything with a slash before the
		// colon, which is a local path rather than a remote.
		at := strings.Index(remote, "@")
		colon := strings.Index(remote, ":")
		if colon < 0 || (at >= 0 && at > colon) {
			return Repo{}, false
		}
		hostPart := remote[:colon]
		if at >= 0 {
			hostPart = remote[at+1 : colon]
		}
		if strings.Contains(hostPart, "/") || hostPart == "" {
			return Repo{}, false
		}
		host, path = hostPart, remote[colon+1:]
	}

	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return Repo{}, false
	}

	return Repo{
		Host:  strings.ToLower(host),
		Owner: segments[0],
		Name:  strings.TrimSuffix(segments[1], ".git"),
	}, true
}
