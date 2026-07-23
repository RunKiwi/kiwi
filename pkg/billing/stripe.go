package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defaultStripeBase is the live Stripe API host. Overridable on StripeClient so
// tests can point at an httptest server (no network, no real key needed).
const defaultStripeBase = "https://api.stripe.com"

// signatureTolerance bounds how old a webhook signature's timestamp may be, to
// blunt replay attacks. Matches Stripe's own default guidance.
const signatureTolerance = 5 * time.Minute

// StripeClient is a tiny, dependency-free wrapper over the two Stripe calls the
// billing rail actually needs: creating a Checkout Session (to start a payment)
// and — via VerifyStripeSignature — validating the webhook that reports it.
type StripeClient struct {
	SecretKey string
	BaseURL   string // defaults to defaultStripeBase
	HTTP      *http.Client
}

func (c *StripeClient) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultStripeBase
}

func (c *StripeClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// CreateCheckoutSession opens a Stripe Checkout Session for a subscription to
// priceID and returns the hosted-checkout URL the user is redirected to. orgID
// and plan are stamped into the session metadata AND the resulting
// subscription's metadata, so both `checkout.session.completed` and
// `customer.subscription.*` webhook events carry enough to upgrade the org.
func (c *StripeClient) CreateCheckoutSession(ctx context.Context, priceID, orgID, plan, successURL, cancelURL string) (string, error) {
	if c.SecretKey == "" {
		return "", fmt.Errorf("stripe secret key not configured")
	}
	if priceID == "" {
		return "", fmt.Errorf("stripe price id not configured")
	}

	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("line_items[0][price]", priceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("success_url", successURL)
	form.Set("cancel_url", cancelURL)
	// client_reference_id + metadata both carry the org so the webhook can never
	// be ambiguous about who paid.
	form.Set("client_reference_id", orgID)
	form.Set("metadata[org_id]", orgID)
	form.Set("metadata[plan]", plan)
	form.Set("subscription_data[metadata][org_id]", orgID)
	form.Set("subscription_data[metadata][plan]", plan)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/v1/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.SecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("stripe checkout request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("stripe checkout returned %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode stripe checkout response: %w", err)
	}
	if out.URL == "" {
		return "", fmt.Errorf("stripe checkout response had no url")
	}
	return out.URL, nil
}

// VerifyStripeSignature validates a webhook body against the `Stripe-Signature`
// header using Stripe's scheme: HMAC-SHA256 over "<timestamp>.<payload>", keyed
// by the endpoint's signing secret, compared in constant time, with a timestamp
// tolerance to reject replays. Returns nil iff the signature is authentic.
func VerifyStripeSignature(payload []byte, sigHeader, secret string) error {
	return verifyStripeSignatureAt(payload, sigHeader, secret, time.Now(), signatureTolerance)
}

func verifyStripeSignatureAt(payload []byte, sigHeader, secret string, now time.Time, tolerance time.Duration) error {
	if secret == "" {
		return fmt.Errorf("webhook signing secret not configured")
	}
	var ts string
	var v1s []string
	for _, part := range strings.Split(sigHeader, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			v1s = append(v1s, kv[1])
		}
	}
	if ts == "" || len(v1s) == 0 {
		return fmt.Errorf("malformed Stripe-Signature header")
	}
	if tolerance > 0 {
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid signature timestamp")
		}
		if diff := now.Sub(time.Unix(sec, 0)); diff > tolerance || diff < -tolerance {
			return fmt.Errorf("signature timestamp outside tolerance")
		}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(payload)))
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, v := range v1s {
		if hmac.Equal([]byte(v), []byte(expected)) {
			return nil
		}
	}
	return fmt.Errorf("no matching signature")
}
