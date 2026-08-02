package gitcache

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// Option configures a single cache operation.
//
// Variadic rather than a field on Cache because the token belongs to the task
// being run, not to the cache: the cache outlives any one task and is shared
// across them, so storing a credential on it would leave one task's secret
// reachable by the next.
type Option func(*options)

type options struct {
	token string
}

// WithToken authenticates the fetch side of a cache operation.
//
// Without it a private repository cannot be cloned at all. GIT_TOKEN was
// applied only when pushing (pkg/daemon/delivery.go), so the daemon could open
// a pull request against a repo it was unable to read — every private repo
// failed at provisioning with:
//
//	fatal: could not read Username for 'https://github.com': No such device or address
//
// which names no credential and reads like a terminal misconfiguration.
func WithToken(token string) Option {
	return func(o *options) { o.token = token }
}

func apply(opts []Option) options {
	var o options
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	return o
}

// authArgs builds the `git -c` prefix that authenticates a remote operation.
//
// The token goes in an http.extraHeader rather than in the remote URL, which is
// what the push path does (delivery.go embeds x-access-token in the URL). That
// is safe for a one-shot push but wrong here: `clone` writes its argument to
// the bare repo's config as remote.origin.url, so a URL-embedded token would be
// persisted to disk in the per-org cache and re-read by every later fetch. A -c
// value lives only for the one process.
//
// The header is scoped to the remote's scheme://host/ prefix. An unscoped
// http.extraHeader is attached to every request git makes, including a redirect
// to another host — which is how credentials leak to third parties.
func authArgs(repoURL, token string) []string {
	if token == "" {
		return nil
	}
	scope, ok := authScope(repoURL)
	if !ok {
		// Not an HTTP(S) remote — SSH and local paths carry their own auth and
		// an extraHeader would be silently ignored at best.
		return nil
	}
	// Git speaks HTTP Basic here; GitHub accepts any username alongside a token
	// and x-access-token is the documented one.
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{"-c", "http." + scope + ".extraHeader=Authorization: Basic " + basic}
}

// authScope is the scheme://host/ prefix git matches an http.<url>.* setting
// against. Trailing slash included: git treats it as a path prefix.
func authScope(repoURL string) (string, bool) {
	u, err := url.Parse(repoURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", false
	}
	return u.Scheme + "://" + u.Host + "/", true
}

// scrub removes a token and its encoded form from text bound for a log or an
// error. git echoes the failing command on error, and that command carries the
// -c value, so an unscrubbed failure would print the credential.
func scrub(s, token string) string {
	if token == "" {
		return s
	}
	s = strings.ReplaceAll(s, token, "***")
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return strings.ReplaceAll(s, basic, "***")
}
