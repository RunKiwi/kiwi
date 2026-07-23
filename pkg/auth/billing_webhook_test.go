package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newBillingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// ActivateOrg (called on checkout) also enqueues a ProvisioningRequest.
	if err := db.AutoMigrate(&Organization{}, &OrgLimits{}, &ProvisioningRequest{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func stripeSigned(t *testing.T, secret, body string) *http.Request {
	t.Helper()
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.%s", ts, body)))
	sig := fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/billing", strings.NewReader(body))
	req.Header.Set("Stripe-Signature", sig)
	return req
}

func TestBillingWebhookUpgradesOrgOnValidSignature(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_test")
	db := newBillingTestDB(t)
	db.Create(&Organization{ID: "org1", Name: "Acme", Plan: "free"})
	db.Create(&OrgLimits{OrgID: "org1", MaxConcurrentJobs: 1})

	body := `{"type":"checkout.session.completed","data":{"object":{"metadata":{"org_id":"org1","plan":"pro"}}}}`
	rec := httptest.NewRecorder()
	BillingWebhookHandler(db)(rec, stripeSigned(t, "whsec_test", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var org Organization
	db.First(&org, "id = ?", "org1")
	if org.Plan != "pro" {
		t.Errorf("plan = %q, want pro", org.Plan)
	}
	var limits OrgLimits
	db.First(&limits, "org_id = ?", "org1")
	if limits.MaxConcurrentJobs != 20 {
		t.Errorf("max_concurrent_jobs = %d, want 20 (pro)", limits.MaxConcurrentJobs)
	}
}

func TestBillingWebhookRejectsBadSignature(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_test")
	db := newBillingTestDB(t)
	db.Create(&Organization{ID: "org1", Plan: "free"})

	body := `{"type":"checkout.session.completed","data":{"object":{"metadata":{"org_id":"org1","plan":"pro"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/billing", strings.NewReader(body))
	req.Header.Set("Stripe-Signature", "t=1700000000,v1=deadbeef")
	rec := httptest.NewRecorder()
	BillingWebhookHandler(db)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var org Organization
	db.First(&org, "id = ?", "org1")
	if org.Plan != "free" {
		t.Errorf("plan changed to %q on a forged webhook", org.Plan)
	}
}

func TestBillingWebhookDisabledWithoutSecret(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")
	db := newBillingTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/billing", strings.NewReader("{}"))
	BillingWebhookHandler(db)(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
