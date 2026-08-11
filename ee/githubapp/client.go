// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package githubapp

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Sentinel errors. Callers map these to what the operator should do, so they
// must survive the boundary rather than collapsing into one "github failed".
var (
	// ErrInstallationGone means the customer uninstalled the App or removed the
	// repository from it. Nothing Kiwi does will fix it and a retry is wasted;
	// the task should fail naming the repository.
	ErrInstallationGone = errors.New("githubapp: installation no longer accessible")

	// ErrAppAuth means the App's own credentials are wrong: bad key, wrong app
	// id, or a clock far enough off that the JWT is rejected. This is a Kiwi
	// misconfiguration and affects every org, not one.
	ErrAppAuth = errors.New("githubapp: app authentication rejected")
)

// expiryMargin is how long before real expiry a cached token stops being
// handed out.
//
// It covers the gap between "the daemon received this token" and "git finished
// using it". A clone of a large repository is the long pole and five minutes
// comfortably exceeds it, while still letting one mint serve most of an hour of
// tasks. Minting is rate-limited per App, so caching is not just an
// optimisation at fleet scale.
const expiryMargin = 5 * time.Minute

// Token is an installation token and the moment it stops being valid.
type Token struct {
	Value     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Client mints installation tokens for one GitHub App.
type Client struct {
	appID   string
	key     *rsa.PrivateKey
	http    *http.Client
	baseURL string
	now     func() time.Time

	mu    sync.Mutex
	cache map[int64]Token
}

// Option configures a Client. Used by tests to point at a stub server and to
// control time; there is no production caller that needs either.
type Option func(*Client)

// WithBaseURL redirects API calls, for tests and GitHub Enterprise Server.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient replaces the HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithClock replaces the time source.
func WithClock(fn func() time.Time) Option {
	return func(c *Client) { c.now = fn }
}

// New builds a Client from an App id and its PEM private key.
func New(appID string, pemKey []byte, opts ...Option) (*Client, error) {
	if strings.TrimSpace(appID) == "" {
		return nil, errors.New("githubapp: empty app id")
	}
	key, err := ParsePrivateKey(pemKey)
	if err != nil {
		return nil, err
	}
	c := &Client{
		appID:   appID,
		key:     key,
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://api.github.com",
		now:     time.Now,
		cache:   map[int64]Token{},
	}
	for _, fn := range opts {
		fn(c)
	}
	return c, nil
}

// InstallationToken returns a token for one installation, minting only when the
// cached one is absent or close enough to expiry to be unsafe to hand out.
func (c *Client) InstallationToken(ctx context.Context, installationID int64) (Token, error) {
	if tok, ok := c.cached(installationID); ok {
		return tok, nil
	}

	tok, err := c.mint(ctx, installationID)
	if err != nil {
		// A dead installation must not leave a usable token behind: the
		// customer revoking access should take effect on the next task, not
		// whenever the cache happens to age out.
		if errors.Is(err, ErrInstallationGone) {
			c.forget(installationID)
		}
		return Token{}, err
	}

	c.mu.Lock()
	c.cache[installationID] = tok
	c.mu.Unlock()
	return tok, nil
}

func (c *Client) cached(id int64) (Token, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tok, ok := c.cache[id]
	if !ok {
		return Token{}, false
	}
	if c.now().Add(expiryMargin).After(tok.ExpiresAt) {
		return Token{}, false
	}
	return tok, true
}

func (c *Client) forget(id int64) {
	c.mu.Lock()
	delete(c.cache, id)
	c.mu.Unlock()
}

func (c *Client) mint(ctx context.Context, installationID int64) (Token, error) {
	jwt, err := appJWT(c.appID, c.key, c.now())
	if err != nil {
		return Token{}, err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.baseURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return Token{}, fmt.Errorf("githubapp: build mint request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("githubapp: mint installation token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
	case http.StatusNotFound, http.StatusGone:
		return Token{}, ErrInstallationGone
	case http.StatusUnauthorized, http.StatusForbidden:
		// 401 on this endpoint is about the App JWT, not the installation:
		// the installation is addressed by path, and a missing one 404s.
		return Token{}, ErrAppAuth
	default:
		return Token{}, fmt.Errorf("githubapp: mint returned %s", resp.Status)
	}

	var out Token
	if err := json.Unmarshal(body, &out); err != nil {
		return Token{}, fmt.Errorf("githubapp: decode mint response: %w", err)
	}
	if out.Value == "" {
		return Token{}, errors.New("githubapp: mint response carried no token")
	}
	return out, nil
}

// Installation is what GitHub knows about one installation.
type Installation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
	} `json:"account"`
	RepositorySelection string `json:"repository_selection"`
}

// GetInstallation looks up an installation by id.
//
// The install callback needs this because GitHub's redirect carries only the
// installation id. The account the App was installed on, which is the key every
// later lookup resolves against, has to be asked for. Taking it from the
// redirect instead would let anyone who guessed an id claim somebody else's
// account.
func (c *Client) GetInstallation(ctx context.Context, installationID int64) (Installation, error) {
	jwt, err := appJWT(c.appID, c.key, c.now())
	if err != nil {
		return Installation{}, err
	}

	url := fmt.Sprintf("%s/app/installations/%d", c.baseURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Installation{}, fmt.Errorf("githubapp: build installation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return Installation{}, fmt.Errorf("githubapp: get installation: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusGone:
		return Installation{}, ErrInstallationGone
	case http.StatusUnauthorized, http.StatusForbidden:
		return Installation{}, ErrAppAuth
	default:
		return Installation{}, fmt.Errorf("githubapp: get installation returned %s", resp.Status)
	}

	var out Installation
	if err := json.Unmarshal(body, &out); err != nil {
		return Installation{}, fmt.Errorf("githubapp: decode installation: %w", err)
	}
	if out.Account.Login == "" {
		return Installation{}, errors.New("githubapp: installation carried no account login")
	}
	return out, nil
}

// Repository is one repository an installation can reach.
type Repository struct {
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

// ListRepositories returns the repositories an installation covers.
//
// This is a better answer than the one a personal access token gives. A PAT
// lists everything its owner can see, most of which Kiwi has no business
// touching and cannot act on anyway; an installation lists exactly what the
// customer ticked. The picker and the permission then agree, so a repository
// that appears in the dropdown is one a task can actually run against.
//
// Paginated because an installation on a large organisation routinely exceeds
// one page, and a silently truncated picker looks like a missing repository.
func (c *Client) ListRepositories(ctx context.Context, installationID int64) ([]Repository, error) {
	tok, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}

	var all []Repository
	for page := 1; page <= 10; page++ {
		url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", c.baseURL, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("githubapp: build repositories request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok.Value)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("githubapp: list repositories: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusNotFound, http.StatusGone:
			return nil, ErrInstallationGone
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, ErrAppAuth
		default:
			return nil, fmt.Errorf("githubapp: list repositories returned %s", resp.Status)
		}

		var out struct {
			Repositories []Repository `json:"repositories"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("githubapp: decode repositories: %w", err)
		}
		all = append(all, out.Repositories...)
		if len(out.Repositories) < 100 {
			break
		}
	}
	return all, nil
}
