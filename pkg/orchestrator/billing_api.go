package orchestrator

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/billing"
)

// handleBillingCheckout starts a Stripe Checkout Session for the caller's org to
// upgrade to Pro and returns the hosted-checkout URL. The org is taken from the
// authenticated claims, never the body, and stamped into the session so the
// webhook (POST /api/v1/webhooks/billing) can upgrade the right tenant on
// payment. Returns 503 when Stripe isn't configured, so the free path is
// unaffected in environments without billing.
func (s *Server) handleBillingCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	secret := os.Getenv("STRIPE_SECRET_KEY")
	priceID := os.Getenv("STRIPE_PRICE_ID")
	if secret == "" || priceID == "" {
		http.Error(w, "billing not configured", http.StatusServiceUnavailable)
		return
	}

	appURL := os.Getenv("KIWI_APP_URL")
	if appURL == "" {
		appURL = "https://app.runkiwi.dev"
	}

	client := &billing.StripeClient{SecretKey: secret}
	url, err := client.CreateCheckoutSession(r.Context(), priceID, claims.OrgID, "pro",
		appURL+"/settings?checkout=success", appURL+"/settings?checkout=cancelled")
	if err != nil {
		http.Error(w, "failed to start checkout", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": url})
}
