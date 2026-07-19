package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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

	// Verify idempotency
	var orgs []Organization
	db.Where("type = ?", "personal").Find(&orgs)
	if len(orgs) != 1 {
		t.Fatalf("expected 1 personal organization, got %d", len(orgs))
	}
	var joinReqs []OrgJoinRequest
	db.Find(&joinReqs)
	if len(joinReqs) != 1 {
		t.Fatalf("expected 1 join request, got %d", len(joinReqs))
	}

	// Test 3: Duplicate callback
	reqCallback2 := httptest.NewRequest("GET", "/auth/github/callback?state="+stateCookie.Value+"&code=mock_code", nil)
	reqCallback2.AddCookie(stateCookie)
	wCallback2 := httptest.NewRecorder()
	router.ServeHTTP(wCallback2, reqCallback2)

	if wCallback2.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect to dashboard for duplicate login, got %v", wCallback2.Code)
	}

	db.Where("type = ?", "personal").Find(&orgs)
	if len(orgs) != 1 {
		t.Fatalf("expected exactly 1 personal organization after duplicate login, got %d", len(orgs))
	}
	db.Find(&joinReqs)
	if len(joinReqs) != 1 {
		t.Fatalf("expected exactly 1 join request after duplicate login, got %d", len(joinReqs))
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
