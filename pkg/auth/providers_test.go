package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestAuthProvidersEndpoint verifies /auth/providers reports exactly the OAuth
// providers whose client IDs are configured, so the login page only renders
// buttons that actually work.
func TestAuthProvidersEndpoint(t *testing.T) {
	db := setupTestDB(t)

	cases := []struct {
		name     string
		githubID string
		googleID string
		want     []string
	}{
		{"github only", "gh-id", "", []string{"github"}},
		{"google only", "", "gg-id", []string{"google"}},
		{"both", "gh-id", "gg-id", []string{"github", "google"}},
		{"none", "", "", []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KIWI_GITHUB_OAUTH_CLIENT_ID", tc.githubID)
			t.Setenv("KIWI_GOOGLE_OAUTH_CLIENT_ID", tc.googleID)

			router := http.NewServeMux()
			OAuthRouter(db, router)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/auth/providers", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status: got %d, want 200", w.Code)
			}

			var resp struct {
				Providers []string `json:"providers"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !reflect.DeepEqual(resp.Providers, tc.want) {
				t.Fatalf("providers: got %v, want %v", resp.Providers, tc.want)
			}
		})
	}
}
