package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// signature helper mirroring Stripe's scheme, so tests can forge a valid header.
func sign(payload []byte, secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.%s", ts, payload)))
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func TestVerifyStripeSignature(t *testing.T) {
	secret := "whsec_test"
	payload := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Unix(1_700_000_000, 0)
	header := sign(payload, secret, now.Unix())

	if err := verifyStripeSignatureAt(payload, header, secret, now, signatureTolerance); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	// Tampered body must fail.
	if err := verifyStripeSignatureAt([]byte(`{"type":"evil"}`), header, secret, now, signatureTolerance); err == nil {
		t.Error("tampered payload accepted")
	}
	// Wrong secret must fail.
	if err := verifyStripeSignatureAt(payload, header, "whsec_wrong", now, signatureTolerance); err == nil {
		t.Error("wrong secret accepted")
	}
	// Malformed header must fail.
	if err := verifyStripeSignatureAt(payload, "not-a-header", secret, now, signatureTolerance); err == nil {
		t.Error("malformed header accepted")
	}
	// Empty secret must fail (billing disabled).
	if err := verifyStripeSignatureAt(payload, header, "", now, signatureTolerance); err == nil {
		t.Error("empty secret accepted")
	}
	// Replay: a timestamp outside tolerance must fail.
	old := sign(payload, secret, now.Add(-10*time.Minute).Unix())
	if err := verifyStripeSignatureAt(payload, old, secret, now, signatureTolerance); err == nil {
		t.Error("stale (replayed) signature accepted")
	}
}

func TestCreateCheckoutSession(t *testing.T) {
	var gotForm url.Values
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkout/sessions" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1"}`))
	}))
	defer srv.Close()

	c := &StripeClient{SecretKey: "sk_test_x", BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.CreateCheckoutSession(context.Background(), "price_123", "org_1", "pro",
		"https://app/success", "https://app/cancel")
	if err != nil {
		t.Fatalf("CreateCheckoutSession: %v", err)
	}
	if got != "https://checkout.stripe.com/c/pay/cs_test_1" {
		t.Errorf("url = %q", got)
	}
	if gotAuth != "Bearer sk_test_x" {
		t.Errorf("auth header = %q", gotAuth)
	}
	// The org and plan must ride in the metadata so the webhook can act on them.
	for k, want := range map[string]string{
		"mode":                                "subscription",
		"line_items[0][price]":                "price_123",
		"metadata[org_id]":                    "org_1",
		"metadata[plan]":                      "pro",
		"subscription_data[metadata][org_id]": "org_1",
		"client_reference_id":                 "org_1",
	} {
		if gotForm.Get(k) != want {
			t.Errorf("form[%q] = %q, want %q", k, gotForm.Get(k), want)
		}
	}
}

func TestCreateCheckoutSessionRequiresConfig(t *testing.T) {
	if _, err := (&StripeClient{}).CreateCheckoutSession(context.Background(), "price_1", "o", "pro", "s", "c"); err == nil {
		t.Error("expected error with no secret key")
	}
	if _, err := (&StripeClient{SecretKey: "sk"}).CreateCheckoutSession(context.Background(), "", "o", "pro", "s", "c"); err == nil {
		t.Error("expected error with no price id")
	}
}

func TestCreateCheckoutSessionSurfacesStripeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad price"}}`))
	}))
	defer srv.Close()
	c := &StripeClient{SecretKey: "sk_test_x", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.CreateCheckoutSession(context.Background(), "price_bad", "o", "pro", "s", "c"); err == nil {
		t.Error("expected error on non-2xx from Stripe")
	}
}
