package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type providerItem struct {
	ID            string `json:"id"`
	Display       string `json:"display"`
	Kind          string `json:"kind"`
	Connected     bool   `json:"connected"`
	KiwiAvailable bool   `json:"kiwi_available"`
}

// A provider with no platform key configured must report kiwi_available=false.
// That single flag is what makes the UI say "Coming soon" instead of offering
// a model that cannot run.
func TestProvidersEndpointReportsKiwiAvailability(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-test")
	t.Setenv("KIWI_PLATFORM_ANTHROPIC_API_KEY", "")

	s := newDashTestServer(t)
	req := authed(http.MethodGet, "/api/v1/providers", "", "o1")
	rec := httptest.NewRecorder()
	s.handleProviders(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Providers []providerItem `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	byID := map[string]providerItem{}
	for _, p := range body.Providers {
		byID[p.ID] = p
	}
	if !byID["openrouter"].KiwiAvailable {
		t.Error("openrouter has a platform key but reports kiwi_available=false")
	}
	if byID["anthropic"].KiwiAvailable {
		t.Error("anthropic has no platform key but reports kiwi_available=true")
	}
	if byID["openrouter"].Display != "OpenRouter" {
		t.Errorf("openrouter Display = %q", byID["openrouter"].Display)
	}
	if len(body.Providers) != 4 {
		t.Errorf("got %d providers, want 4", len(body.Providers))
	}
}

// The endpoint must never leak a key, and must never leak whether *another*
// org connected one.
func TestProvidersEndpointNeverReturnsKeyMaterial(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-secret-value")

	s := newDashTestServer(t)
	req := authed(http.MethodGet, "/api/v1/providers", "", "o1")
	rec := httptest.NewRecorder()
	s.handleProviders(rec, req)

	if body := rec.Body.String(); contains(body, "sk-or-secret-value") {
		t.Fatal("the providers endpoint returned platform key material")
	}
}

func TestProvidersEndpointRequiresAuth(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	s.handleProviders(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})()
}
