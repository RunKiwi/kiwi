package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
	"gorm.io/gorm"
)

var (
	githubEndpoint = github.Endpoint
	googleEndpoint = google.Endpoint
	githubAPIURL   = "https://api.github.com"
	googleAPIURL   = "https://www.googleapis.com"
)

const SessionCookieName = "kiwi_session"

type SessionData struct {
	UserID    string `json:"user_id"`
	ExpiresAt int64  `json:"expires_at"`
}

func CreateSessionCookieValue(userID string) string {
	sess := SessionData{
		UserID:    userID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	b, _ := json.Marshal(sess)
	data := base64.RawURLEncoding.EncodeToString(b)

	secret := os.Getenv("KIWI_SESSION_SECRET")
	if secret == "" {
		secret = "default-insecure-secret"
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	sig := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return data + "." + sig
}

func VerifySession(cookieValue string) (*SessionData, error) {
	parts := strings.Split(cookieValue, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid session cookie format")
	}
	data, sig := parts[0], parts[1]

	secret := os.Getenv("KIWI_SESSION_SECRET")
	if secret == "" {
		secret = "default-insecure-secret"
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	expectedSig := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid signature")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}

	var session SessionData
	if err := json.Unmarshal(decoded, &session); err != nil {
		return nil, err
	}

	if time.Now().Unix() > session.ExpiresAt {
		return nil, fmt.Errorf("session expired")
	}

	return &session, nil
}

func OAuthRouter(db *gorm.DB, mux *http.ServeMux) {
	mux.HandleFunc("/auth/providers", func(w http.ResponseWriter, r *http.Request) {
		providers := []string{}
		if os.Getenv("KIWI_GITHUB_OAUTH_CLIENT_ID") != "" {
			providers = append(providers, "github")
		}
		if os.Getenv("KIWI_GOOGLE_OAUTH_CLIENT_ID") != "" {
			providers = append(providers, "google")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{"providers": providers})
	})

	if os.Getenv("KIWI_GITHUB_OAUTH_CLIENT_ID") != "" {
		mux.HandleFunc("/auth/github/start", func(w http.ResponseWriter, r *http.Request) { handleOAuthStart(w, r, "github") })
		mux.HandleFunc("/auth/github/callback", func(w http.ResponseWriter, r *http.Request) { handleOAuthCallback(db, w, r, "github") })
	}
	if os.Getenv("KIWI_GOOGLE_OAUTH_CLIENT_ID") != "" {
		mux.HandleFunc("/auth/google/start", func(w http.ResponseWriter, r *http.Request) { handleOAuthStart(w, r, "google") })
		mux.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) { handleOAuthCallback(db, w, r, "google") })
	}
}

func getConfig(provider string) *oauth2.Config {
	base := os.Getenv("KIWI_OAUTH_REDIRECT_BASE")
	if base == "" {
		base = "http://localhost:8080"
	}
	if provider == "github" {
		return &oauth2.Config{
			ClientID:     os.Getenv("KIWI_GITHUB_OAUTH_CLIENT_ID"),
			ClientSecret: os.Getenv("KIWI_GITHUB_OAUTH_CLIENT_SECRET"),
			Endpoint:     githubEndpoint,
			RedirectURL:  base + "/auth/github/callback",
			Scopes:       []string{"read:user", "user:email", "repo"},
		}
	} else if provider == "google" {
		return &oauth2.Config{
			ClientID:     os.Getenv("KIWI_GOOGLE_OAUTH_CLIENT_ID"),
			ClientSecret: os.Getenv("KIWI_GOOGLE_OAUTH_CLIENT_SECRET"),
			Endpoint:     googleEndpoint,
			RedirectURL:  base + "/auth/google/callback",
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
		}
	}
	return nil
}

func handleOAuthStart(w http.ResponseWriter, r *http.Request, provider string) {
	cfg := getConfig(provider)
	if cfg == nil {
		http.NotFound(w, r)
		return
	}

	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)

	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		Expires:  time.Now().Add(10 * time.Minute),
		HttpOnly: true,
		Secure:   strings.HasPrefix(cfg.RedirectURL, "https"),
		SameSite: http.SameSiteLaxMode,
	})

	url := cfg.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleOAuthCallback(db *gorm.DB, w http.ResponseWriter, r *http.Request, provider string) {
	cfg := getConfig(provider)
	if cfg == nil {
		http.NotFound(w, r)
		return
	}

	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || r.FormValue("state") != stateCookie.Value {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	token, err := cfg.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "Failed to exchange code", http.StatusInternalServerError)
		return
	}

	client := cfg.Client(r.Context(), token)
	var email, name, subject string

	if provider == "github" {
		req, _ := http.NewRequest("GET", githubAPIURL+"/user", nil)
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			http.Error(w, "Failed to get github user", http.StatusInternalServerError)
			return
		}
		var userResp struct {
			Login string `json:"login"`
			Name  string `json:"name"`
			ID    int64  `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&userResp)
		resp.Body.Close()
		name = userResp.Name
		if name == "" {
			name = userResp.Login
		}
		subject = fmt.Sprintf("%d", userResp.ID)

		reqEmail, _ := http.NewRequest("GET", githubAPIURL+"/user/emails", nil)
		respEmail, err := client.Do(reqEmail)
		if err != nil || respEmail.StatusCode != http.StatusOK {
			http.Error(w, "Failed to get github emails", http.StatusInternalServerError)
			return
		}
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		json.NewDecoder(respEmail.Body).Decode(&emails)
		respEmail.Body.Close()

		for _, e := range emails {
			if e.Primary && e.Verified {
				email = e.Email
				break
			}
		}
		if email == "" {
			http.Error(w, "No verified primary email found on GitHub", http.StatusForbidden)
			return
		}

	} else if provider == "google" {
		req, _ := http.NewRequest("GET", googleAPIURL+"/oauth2/v2/userinfo", nil)
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			http.Error(w, "Failed to get google user", http.StatusInternalServerError)
			return
		}
		var userResp struct {
			ID            string `json:"id"`
			Email         string `json:"email"`
			VerifiedEmail bool   `json:"verified_email"`
			Name          string `json:"name"`
		}
		json.NewDecoder(resp.Body).Decode(&userResp)
		resp.Body.Close()

		if !userResp.VerifiedEmail || userResp.Email == "" {
			http.Error(w, "Google email not verified", http.StatusForbidden)
			return
		}
		email = userResp.Email
		name = userResp.Name
		subject = userResp.ID
	}

	// Resolve or create user
	var user User
	err = db.Where("oauth_provider = ? AND oauth_subject = ?", provider, subject).First(&user).Error

	if err != nil {
		// Try finding by email
		err = db.Where("email = ?", email).First(&user).Error
		if err != nil {
			org, isNewOrg, needsApproval := resolveOrgForUser(r.Context(), db, email)

			assignedOrgID := org.ID
			role := "member"

			if isNewOrg {
				role = "admin"
			}

			if needsApproval {
				// Create a personal org while they wait for approval. Use
				// Where(stable key).Attrs(create-only) so a repeat sign-in
				// resolves the existing row instead of inserting a duplicate.
				personalOrgID := "org_" + hex.EncodeToString([]byte(email))[:8]
				var personalOrg Organization
				if err := db.Where(Organization{ID: personalOrgID}).First(&personalOrg).Error; err != nil {
					db.Transaction(func(tx *gorm.DB) error {
						personalOrg = Organization{
							ID:              personalOrgID,
							Name:            email + "'s Workspace",
							Type:            "personal",
							ActivationState: "inactive",
							Plan:            "free",
							CreatedAt:       time.Now(),
						}
						if err := tx.Create(&personalOrg).Error; err != nil {
							return err
						}
						if err := tx.Create(FreeLimits(personalOrg.ID)).Error; err != nil {
							return err
						}
						return CreateDefaultFleet(tx, personalOrg.ID)
					})
				}
				assignedOrgID = personalOrg.ID
				role = "admin" // admin of their personal org

				// Only the (org, user, pending) tuple identifies the request;
				// the random ID and timestamp are create-only via Attrs, so they
				// can't leak into the lookup and force a duplicate insert.
				reqIDBytes := make([]byte, 8)
				rand.Read(reqIDBytes)
				var joinReq OrgJoinRequest
				db.Where(OrgJoinRequest{OrgID: org.ID, UserEmail: email, Status: "pending"}).Attrs(OrgJoinRequest{
					ID:        "req_" + hex.EncodeToString(reqIDBytes),
					CreatedAt: time.Now(),
				}).FirstOrCreate(&joinReq)
			}

			idBytes := make([]byte, 8)
			rand.Read(idBytes)
			userID := "usr_" + hex.EncodeToString(idBytes)

			user = User{
				ID:            userID,
				Email:         email,
				Name:          name,
				OrgID:         assignedOrgID,
				Role:          role,
				OAuthProvider: &provider,
				OAuthSubject:  &subject,
				CreatedAt:     time.Now(),
			}
			db.Create(&user)
		} else {
			// Update existing user with oauth connection
			user.OAuthProvider = &provider
			user.OAuthSubject = &subject
			db.Save(&user)
		}
	}

	// Issue session cookie (used by the server-rendered surfaces; the SPA
	// authenticates with the bearer API key handed back below).
	sessionVal := CreateSessionCookieValue(user.ID)
	secure := strings.HasPrefix(cfg.RedirectURL, "https")
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionVal,
		Path:     "/",
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	// Mint a fresh API key so the SPA (which authenticates with a bearer token
	// held in localStorage) has a credential. An existing key's plaintext can't
	// be recovered, so each OAuth sign-in issues a new "Web Session" key.
	apiKey, apiKeyRecord, keyErr := GenerateAPIKey(user.ID, "Web Session", nil)
	if keyErr != nil {
		http.Error(w, "Failed to create session key", http.StatusInternalServerError)
		return
	}
	if err := db.Create(apiKeyRecord).Error; err != nil {
		http.Error(w, "Failed to persist session key", http.StatusInternalServerError)
		return
	}

	// Hand the browser back to the SPA on the frontend origin. The token rides
	// in the URL fragment, which browsers never send to servers — so it stays
	// out of access logs and Referer headers. KIWI_FRONTEND_URL must be the
	// app origin (e.g. https://app.runkiwi.dev); default is the local dev SPA.
	frontendURL := os.Getenv("KIWI_FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	redirectURL := strings.TrimRight(frontendURL, "/") + "/auth/callback#token=" + url.QueryEscape(apiKey)
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}
