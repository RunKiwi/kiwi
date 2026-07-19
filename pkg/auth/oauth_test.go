package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestOAuthFlow_Github(t *testing.T) {
	db := setupTestDB(t)

	// Stub github endpoints
	githubMux := http.NewServeMux()

	// token endpoint
	githubMux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token": "mock-token", "token_type": "bearer"}`))
	})

	// user endpoint
	githubMux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"login": "testuser", "name": "Test User", "id": 12345}`))
	})

	// emails endpoint
	githubMux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"email": "test@github.local", "primary": true, "verified": true}]`))
	})

	mockGithub := httptest.NewServer(githubMux)
	defer mockGithub.Close()

	// override variables for test
	oldEndpoint := githubEndpoint
	oldAPI := githubAPIURL

	githubEndpoint = oauth2.Endpoint{
		AuthURL:  mockGithub.URL + "/login/oauth/authorize",
		TokenURL: mockGithub.URL + "/login/oauth/access_token",
	}
	githubAPIURL = mockGithub.URL

	t.Setenv("KIWI_GITHUB_OAUTH_CLIENT_ID", "client_id")
	t.Setenv("KIWI_GITHUB_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("KIWI_OAUTH_REDIRECT_BASE", mockGithub.URL)

	defer func() {
		githubEndpoint = oldEndpoint
		githubAPIURL = oldAPI
	}()

	// Test 1: Start
	reqStart := httptest.NewRequest("GET", "/auth/github/start", nil)
	wStart := httptest.NewRecorder()

	router := http.NewServeMux()
	OAuthRouter(db, router)
	router.ServeHTTP(wStart, reqStart)

	if wStart.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect, got %v", wStart.Code)
	}

	var stateCookie *http.Cookie
	for _, c := range wStart.Result().Cookies() {
		if c.Name == "oauth_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("expected oauth_state cookie")
	}

	// Test 2: Callback
	reqCallback := httptest.NewRequest("GET", "/auth/github/callback?state="+stateCookie.Value+"&code=mock_code", nil)
	reqCallback.AddCookie(stateCookie)
	wCallback := httptest.NewRecorder()

	router.ServeHTTP(wCallback, reqCallback)

	if wCallback.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect to SPA callback, got %v", wCallback.Code)
	}

	// The callback hands the browser back to the SPA on the frontend origin,
	// carrying the freshly-minted API token in the URL fragment.
	loc := wCallback.Header().Get("Location")
	if !strings.Contains(loc, "/auth/callback#token=") {
		t.Fatalf("expected SPA callback redirect with token fragment, got %v", loc)
	}

	var sessionCookie *http.Cookie
	for _, c := range wCallback.Result().Cookies() {
		if c.Name == SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}

	// Verify session
	sess, err := VerifySession(sessionCookie.Value)
	if err != nil {
		t.Fatalf("failed to verify session: %v", err)
	}

	// Verify user exists in DB
	var user User
	if err := db.First(&user, "id = ?", sess.UserID).Error; err != nil {
		t.Fatalf("expected user to be created: %v", err)
	}
	if user.Email != "test@github.local" || user.Name != "Test User" || *user.OAuthProvider != "github" || *user.OAuthSubject != "12345" {
		t.Errorf("user fields mismatch: %+v", user)
	}

	// The token handed back in the fragment must be persisted in api_keys and
	// map to this user — otherwise the SPA's bearer auth (validate) 401s.
	frag := loc[strings.Index(loc, "#token=")+len("#token="):]
	tokenPlain, err := url.QueryUnescape(frag)
	if err != nil {
		t.Fatalf("failed to unescape token fragment: %v", err)
	}
	var apiKey APIKey
	if err := db.First(&apiKey, "key_hash = ?", hashToken(tokenPlain)).Error; err != nil {
		t.Fatalf("expected minted API key to be persisted: %v", err)
	}
	if apiKey.UserID != user.ID {
		t.Errorf("API key user mismatch: got %s want %s", apiKey.UserID, user.ID)
	}

	// test@github.local is a personal domain: resolveOrgForUser creates exactly
	// one personal org and there is NO join request (personal users need no
	// approval). A repeat sign-in must not create duplicates.
	var orgs []Organization
	db.Where("type = ?", "personal").Find(&orgs)
	if len(orgs) != 1 {
		t.Fatalf("expected 1 personal organization, got %d", len(orgs))
	}
	var joinReqs []OrgJoinRequest
	db.Find(&joinReqs)
	if len(joinReqs) != 0 {
		t.Fatalf("expected 0 join requests for a personal-domain user, got %d", len(joinReqs))
	}

	// Duplicate sign-in: the user already exists, so no new org is created.
	reqCallback2 := httptest.NewRequest("GET", "/auth/github/callback?state="+stateCookie.Value+"&code=mock_code", nil)
	reqCallback2.AddCookie(stateCookie)
	wCallback2 := httptest.NewRecorder()
	router.ServeHTTP(wCallback2, reqCallback2)
	if wCallback2.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect on duplicate login, got %v", wCallback2.Code)
	}
	db.Where("type = ?", "personal").Find(&orgs)
	if len(orgs) != 1 {
		t.Fatalf("expected 1 personal organization after duplicate login, got %d", len(orgs))
	}
}

// newGithubMock returns a stub GitHub OAuth server that authenticates a single
// identity.
func newGithubMock(email, name string, id int) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"mock-token","token_type":"bearer"}`))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"login":"u","name":%q,"id":%d}`, name, id)
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{"email":%q,"primary":true,"verified":true}]`, email)
	})
	return httptest.NewServer(mux)
}

// githubSignIn drives a full start->callback OAuth exchange against router.
func githubSignIn(t *testing.T, router http.Handler) {
	t.Helper()
	wStart := httptest.NewRecorder()
	router.ServeHTTP(wStart, httptest.NewRequest("GET", "/auth/github/start", nil))
	var state *http.Cookie
	for _, c := range wStart.Result().Cookies() {
		if c.Name == "oauth_state" {
			state = c
		}
	}
	if state == nil {
		t.Fatal("no oauth_state cookie from /auth/github/start")
	}
	req := httptest.NewRequest("GET", "/auth/github/callback?state="+state.Value+"&code=mock_code", nil)
	req.AddCookie(state)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("callback: expected redirect, got %d: %s", w.Code, w.Body.String())
	}
}

// TestOAuthFlow_IdempotentCompanyJoin exercises the needsApproval branch: a
// second user of an existing company org (domain-join off) gets a personal org
// plus a pending join request. Re-entering the branch (simulating a sign-in
// where the user row never persisted) must NOT create duplicates.
func TestOAuthFlow_IdempotentCompanyJoin(t *testing.T) {
	db := setupTestDB(t)

	// Pre-seed the company org so an acme.com user needs approval to join it.
	if err := db.Create(&Organization{
		ID: "org_acme", Name: "acme.com", Type: "team", PrimaryDomain: "acme.com",
		DomainJoin: false, ActivationState: "inactive", Plan: "free", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}

	mock := newGithubMock("dev@acme.com", "Dev", 4242)
	defer mock.Close()

	oldEndpoint, oldAPI := githubEndpoint, githubAPIURL
	githubEndpoint = oauth2.Endpoint{AuthURL: mock.URL + "/login/oauth/authorize", TokenURL: mock.URL + "/login/oauth/access_token"}
	githubAPIURL = mock.URL
	defer func() { githubEndpoint, githubAPIURL = oldEndpoint, oldAPI }()

	t.Setenv("KIWI_GITHUB_OAUTH_CLIENT_ID", "client_id")
	t.Setenv("KIWI_GITHUB_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("KIWI_OAUTH_REDIRECT_BASE", mock.URL)

	router := http.NewServeMux()
	OAuthRouter(db, router)

	counts := func() (personalOrgs, pendingReqs int) {
		var orgs []Organization
		db.Where("type = ?", "personal").Find(&orgs)
		var reqs []OrgJoinRequest
		db.Where("status = ?", "pending").Find(&reqs)
		return len(orgs), len(reqs)
	}

	// First sign-in: creates the personal org + a pending join request.
	githubSignIn(t, router)
	if o, r := counts(); o != 1 || r != 1 {
		t.Fatalf("after first sign-in: want 1 personal org / 1 join request, got %d / %d", o, r)
	}

	// Simulate the branch re-entering on a later sign-in (e.g. the user row
	// never persisted): delete the user so the callback recreates org+request.
	if err := db.Where("email = ?", "dev@acme.com").Delete(&User{}).Error; err != nil {
		t.Fatalf("delete user: %v", err)
	}

	// Second sign-in must be idempotent: still exactly one org + one request.
	githubSignIn(t, router)
	if o, r := counts(); o != 1 || r != 1 {
		t.Fatalf("after re-entry: want 1 personal org / 1 join request, got %d / %d", o, r)
	}
}

func TestSessionAndMiddleware(t *testing.T) {
	db := setupTestDB(t)

	// create user
	user := User{ID: "usr_sessiontest", Email: "sess@test.local", Name: "Sess User", OrgID: "org1", Role: "member"}
	db.Create(&user)

	sessionVal := CreateSessionCookieValue(user.ID)

	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		claims := ClaimsFromContext(r.Context())
		if claims == nil || claims.UserID != user.ID {
			t.Errorf("missing or incorrect claims: %+v", claims)
		}
	})

	middleware := AuthMiddleware(db, testHandler)

	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionVal})

	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !handlerCalled {
		t.Fatal("handler not called")
	}
}
