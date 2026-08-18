package daemon

import (
	"context"
	"crypto/ecdh"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/crypto"
)

func TestDaemon_pollCP(t *testing.T) {
	// Setup mock control plane
	mockSpec := agent.WorkerSpec{
		ID:      "task-test-poll",
		Model:   "sonnet",
		Task:    "echo 'test'",
		RepoURL: "", // Use fallback to avoid full git clone in fast unit tests
		Ref:     "",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := HeartbeatRes{
			Specs: []agent.WorkerSpec{mockSpec},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	cfg := Config{
		APIURL:   server.URL,
		KeyPath:  "", // ephemeral key
		CacheDir: cacheDir,
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start daemon failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	success := d.pollCP(ctx)
	if !success {
		t.Fatalf("pollCP returned false")
	}

	// Verify fallback sandbox directory was created
	fallbackPath := filepath.Join(os.TempDir(), "kiwi-sandbox", mockSpec.ID)
	if stat, err := os.Stat(fallbackPath); err != nil || !stat.IsDir() {
		t.Errorf("fallback sandbox dir was not created")
	}

	// Clean up fallback dir
	os.RemoveAll(fallbackPath)
}

// sealCredsForTest seals a credential map to pub the same way
// store.PostgresStore.SealCredentialsForDaemon does (json.Marshal then
// crypto.SealToPublicKey), so tests can produce a TelemetryDueRes.EncryptedCreds
// value that d.openCredentials can actually open with the matching private key.
func sealCredsForTest(t *testing.T, pub *ecdh.PublicKey, creds map[string]string) string {
	t.Helper()
	payload, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("marshal creds: %v", err)
	}
	sealed, err := crypto.SealToPublicKey(pub, payload)
	if err != nil {
		t.Fatalf("SealToPublicKey: %v", err)
	}
	return sealed
}

// fakeTelemetryClient is a test double for the telemetryClient interface
// pollTelemetry calls through. onDue/onReport are invoked (if non-nil) so a
// test can assert whether the client was called at all, independent of what
// it returns.
type fakeTelemetryClient struct {
	onDue     func()
	onReport  func()
	dueRes    *TelemetryDueRes
	dueErr    error
	reportErr error

	// gotReport captures the last TelemetryReportReq this fake was called
	// with, so tests can inspect what pollTelemetry actually reported.
	gotReport *TelemetryReportReq
}

func (f *fakeTelemetryClient) TelemetryDue(ctx context.Context, req TelemetryDueReq) (*TelemetryDueRes, error) {
	if f.onDue != nil {
		f.onDue()
	}
	return f.dueRes, f.dueErr
}

func (f *fakeTelemetryClient) TelemetryReport(ctx context.Context, req TelemetryReportReq) error {
	if f.onReport != nil {
		f.onReport()
	}
	f.gotReport = &req
	return f.reportErr
}

func TestPollTelemetryCallsDueThenReportWhenCredentialsCached(t *testing.T) {
	// pollTelemetry must ask what's due and, when nothing is due, must not
	// call TelemetryReport at all — an empty batch report is pure noise on
	// the Control Plane side. EncryptedCreds is a real sealed blob (as the
	// live TelemetryDue response now always carries when polls are due) so
	// this exercises the same decrypt path a non-empty Due list would use.
	dueCalls, reportCalls := 0, 0
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	sealed := sealCredsForTest(t, pub, map[string]string{"DATADOG_API_KEY": "secret"})
	fakeClient := &fakeTelemetryClient{
		onDue:    func() { dueCalls++ },
		onReport: func() { reportCalls++ },
		dueRes:   &TelemetryDueRes{EncryptedCreds: sealed},
	}
	d := &Daemon{telemetryClient: fakeClient, priKey: priv}

	d.pollTelemetry(context.Background())

	if dueCalls != 1 {
		t.Errorf("TelemetryDue called %d times, want 1", dueCalls)
	}
	if reportCalls != 0 {
		t.Errorf("TelemetryReport called %d times with nothing due, want 0", reportCalls)
	}
}

func TestPollTelemetryReportsProviderErrorForOneDueSpec(t *testing.T) {
	// Exercises the per-spec loop: a due poll naming a provider whose
	// required credential is missing from the bundle sealed on the
	// TelemetryDue response must still produce a batch report — one result
	// with Error set and both DTO pointers nil, matching TelemetryPollResult's
	// documented contract (nil, not zero-valued, when that half of the query
	// failed).
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	// Sealed bundle has no PROMETHEUS_BASE_URL/PROMETHEUS_BEARER_TOKEN, so
	// telemetry.ProviderFor must fail to construct a connector for p1.
	sealed := sealCredsForTest(t, pub, map[string]string{})
	fakeClient := &fakeTelemetryClient{
		dueRes: &TelemetryDueRes{
			Due: []TelemetryPollSpec{
				{PollID: "p1", Provider: "prometheus", Query: "up"},
			},
			EncryptedCreds: sealed,
		},
	}
	d := &Daemon{telemetryClient: fakeClient, priKey: priv}

	d.pollTelemetry(context.Background())

	if fakeClient.gotReport == nil {
		t.Fatal("TelemetryReport was not called")
	}
	if len(fakeClient.gotReport.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(fakeClient.gotReport.Results))
	}
	got := fakeClient.gotReport.Results[0]
	if got.PollID != "p1" {
		t.Errorf("PollID = %q, want %q", got.PollID, "p1")
	}
	if got.Error == "" {
		t.Error("Error is empty, want a message naming the missing credential")
	}
	if got.Baseline != nil || got.Current != nil {
		t.Errorf("Baseline/Current = %+v/%+v, want both nil on a provider-resolution error", got.Baseline, got.Current)
	}
}

func TestPollTelemetrySkipsQueryingWhenNothingDue(t *testing.T) {
	// TelemetryDue returning an empty Due list (204-equivalent) must not call
	// openCredentials or TelemetryReport — there is nothing to authenticate or
	// report.
	calls := 0
	fakeClient := &fakeTelemetryClient{
		dueRes:   &TelemetryDueRes{},
		onReport: func() { calls++ },
	}
	d := &Daemon{telemetryClient: fakeClient}

	d.pollTelemetry(context.Background())

	if calls != 0 {
		t.Errorf("TelemetryReport called %d times with nothing due, want 0", calls)
	}
}
