// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// newSeamTestServer builds a Server backed by an in-memory sqlite store and
// exposes exactly the daemon-facing routes (which bypass AuthMiddleware in
// production) via httptest. It returns the store so tests can seed state.
func newSeamTestServer(t *testing.T) (*httptest.Server, store.Store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&store.Organization{}, &store.OrgLimits{}, &store.QueuedTask{},
		&store.Credential{}, &store.Daemon{}, &store.DaemonJoinToken{}, &store.Job{},
		&store.PostMergeTelemetryPoll{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	s := &Server{db: db, storage: st}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/daemon/register", s.handleDaemonRegister)
	mux.HandleFunc("/api/v1/daemon/heartbeat", s.handleDaemonHeartbeat)
	mux.HandleFunc("/api/v1/daemon/progress", s.handleDaemonProgress)
	mux.HandleFunc("/api/v1/daemon/result", s.handleDaemonResult)
	mux.HandleFunc("/api/v1/daemon/telemetry/due", s.handleDaemonTelemetryDue)
	mux.HandleFunc("/api/v1/daemon/telemetry/report", s.handleDaemonTelemetryReport)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, st
}

// daemonKeys is a test daemon's two keypairs plus a preconfigured client.
type daemonKeys struct {
	encPub   *ecdh.PublicKey
	encPriv  *ecdh.PrivateKey
	signPub  ed25519.PublicKey
	signPriv ed25519.PrivateKey
	client   *daemon.Client
}

func newDaemonKeys(t *testing.T, baseURL string) *daemonKeys {
	t.Helper()
	encPub, encPriv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("gen enc key: %v", err)
	}
	signPub, signPriv, err := crypto.GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("gen sign key: %v", err)
	}
	c := daemon.NewClient(baseURL)
	c.SetSigner(signPriv)
	return &daemonKeys{encPub, encPriv, signPub, signPriv, c}
}

func (d *daemonKeys) encPubB64() string  { return base64.StdEncoding.EncodeToString(d.encPub.Bytes()) }
func (d *daemonKeys) signPubB64() string { return base64.StdEncoding.EncodeToString(d.signPub) }

// TestDaemonSeam_EndToEnd is the test the original gap could not have: it drives
// a real daemon.Client through the real Control Plane handlers, end to end —
// register, heartbeat, lease, credential seal→open, and result→lease-close.
// Before this seam existed, the very first call would have 404'd.
func TestDaemonSeam_EndToEnd(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()

	// Seed an org, a credential, and one queued task for it.
	if err := st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := st.SaveCredential(ctx, "o1", "ANTHROPIC_API_KEY", store.CredentialLLM, "sk-ant-secret"); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := st.EnqueueTask(ctx, &store.QueuedTask{
		ID:     "job1-w0",
		OrgID:  "o1",
		JobID:  "job1",
		Status: store.TaskQueued,
		Spec:   map[string]interface{}{"id": "job1-w0", "task": "fix the thing", "model": "sonnet"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	d := newDaemonKeys(t, ts.URL)

	// 1. Register with a valid join token bound to o1.
	token, err := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	if err != nil {
		t.Fatalf("mint join token: %v", err)
	}
	if err := d.client.Register(ctx, daemon.RegisterReq{
		JoinToken:  token,
		PubKey:     d.encPubB64(),
		SignPubKey: d.signPubB64(),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	// 2. Heartbeat: should lease the task and return sealed credentials.
	res, err := d.client.Heartbeat(ctx, daemon.HeartbeatReq{
		PubKey:     d.encPubB64(),
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if res == nil || len(res.Specs) != 1 {
		t.Fatalf("expected 1 spec, got %+v", res)
	}
	if res.Specs[0].ID != "job1-w0" || res.Specs[0].Task != "fix the thing" {
		t.Errorf("unexpected spec: %+v", res.Specs[0])
	}
	if res.LeaseID == "" {
		t.Fatal("expected a lease id (fencing token) in the heartbeat response")
	}

	// 3. Open the sealed credentials with the daemon's X25519 private key.
	plaintext, err := crypto.OpenSealed(d.encPriv, res.EncryptedCreds)
	if err != nil {
		t.Fatalf("open sealed credentials: %v", err)
	}
	if want := `"sk-ant-secret"`; !strings.Contains(string(plaintext), want) {
		t.Errorf("decrypted creds = %s, want to contain %s", plaintext, want)
	}

	// 4. Report success; the lease closes and the task becomes SUCCEEDED.
	if err := d.client.ReportResult(ctx, daemon.ResultReq{
		TaskID:     res.Specs[0].ID,
		LeaseID:    res.LeaseID,
		Status:     store.TaskSucceeded,
		SignPubKey: d.signPubB64(),
	}); err != nil {
		t.Fatalf("report result: %v", err)
	}

	var task store.QueuedTask
	if err := st.DB().First(&task, "id = ?", "job1-w0").Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.Status != store.TaskSucceeded {
		t.Errorf("task status = %s, want SUCCEEDED", task.Status)
	}

	// 5. Next heartbeat has no work: 204.
	res2, err := d.client.Heartbeat(ctx, daemon.HeartbeatReq{
		PubKey:     d.encPubB64(),
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("second heartbeat: %v", err)
	}
	if res2 != nil {
		t.Errorf("expected no work (nil), got %+v", res2)
	}
}

// A progress report's PhaseSince must reach the queued_tasks row untouched —
// this is the field the dashboard uses to show how long the CURRENT phase has
// been running, distinct from when the daemon last reported at all.
func TestDaemonSeam_ProgressPersistsPhaseSince(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()

	if err := st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := st.EnqueueTask(ctx, &store.QueuedTask{
		ID: "job1-w0", OrgID: "o1", JobID: "job1", Status: store.TaskQueued,
		Spec: map[string]interface{}{"id": "job1-w0", "task": "fix the thing", "model": "sonnet"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	d := newDaemonKeys(t, ts.URL)
	token, err := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	if err != nil {
		t.Fatalf("mint join token: %v", err)
	}
	if err := d.client.Register(ctx, daemon.RegisterReq{
		JoinToken: token, PubKey: d.encPubB64(), SignPubKey: d.signPubB64(),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	res, err := d.client.Heartbeat(ctx, daemon.HeartbeatReq{
		PubKey: d.encPubB64(), SignPubKey: d.signPubB64(), Timestamp: time.Now().Unix(),
	})
	if err != nil || res == nil || len(res.Specs) != 1 {
		t.Fatalf("heartbeat: %v %+v", err, res)
	}

	since := time.Now().Add(-45 * time.Second).UTC().Truncate(time.Second)
	if err := d.client.ReportProgress(ctx, daemon.ProgressReq{
		TaskID: res.Specs[0].ID, LeaseID: res.LeaseID, SignPubKey: d.signPubB64(),
		Phase: "install: npm ci", PhaseSince: since,
	}); err != nil {
		t.Fatalf("report progress: %v", err)
	}

	var task store.QueuedTask
	if err := st.DB().First(&task, "id = ?", "job1-w0").Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.ProgressPhaseSince == nil || !task.ProgressPhaseSince.Equal(since) {
		t.Errorf("ProgressPhaseSince = %v, want %v", task.ProgressPhaseSince, since)
	}
	if task.ProgressPhase == nil || *task.ProgressPhase != "install: npm ci" {
		t.Errorf("ProgressPhase = %v, want %q", task.ProgressPhase, "install: npm ci")
	}
}

// TestDaemonSeam_UnregisteredHeartbeatRejected proves the identity check has
// teeth: a daemon that never registered cannot lease work even though its
// signature over the body is valid.
func TestDaemonSeam_UnregisteredHeartbeatRejected(t *testing.T) {
	ts, _ := newSeamTestServer(t)
	d := newDaemonKeys(t, ts.URL)

	_, err := d.client.Heartbeat(context.Background(), daemon.HeartbeatReq{
		PubKey:     d.encPubB64(),
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
	})
	if err == nil {
		t.Fatal("expected heartbeat from an unregistered daemon to be rejected")
	}
}

// TestDaemonSeam_ForgedSignatureRejected proves the signature is actually
// checked: a body claiming a registered daemon's identity but signed by a
// different key must be rejected.
func TestDaemonSeam_ForgedSignatureRejected(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()
	if err := st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}

	victim := newDaemonKeys(t, ts.URL)
	token, _ := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	if err := victim.client.Register(ctx, daemon.RegisterReq{
		JoinToken:  token,
		PubKey:     victim.encPubB64(),
		SignPubKey: victim.signPubB64(),
	}); err != nil {
		t.Fatalf("victim register: %v", err)
	}

	// Attacker signs with its own key but claims the victim's identity in the body.
	attacker := newDaemonKeys(t, ts.URL)
	_, err := attacker.client.Heartbeat(ctx, daemon.HeartbeatReq{
		PubKey:     victim.encPubB64(),
		SignPubKey: victim.signPubB64(), // claims victim, but client signs with attacker key
		Timestamp:  time.Now().Unix(),
	})
	if err == nil {
		t.Fatal("expected forged-signature heartbeat to be rejected")
	}
}

// TestDaemonSeam_TelemetryDueForgedSignatureRejected mirrors
// TestDaemonSeam_ForgedSignatureRejected for the telemetry/due endpoint: it
// shares readSignedBody -> GetDaemonBySignPubKey with Heartbeat, but nothing
// previously exercised that this specific handler wires the same check
// through rather than skipping it. A body claiming a registered daemon's
// identity but signed by a different key must be rejected.
func TestDaemonSeam_TelemetryDueForgedSignatureRejected(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()
	if err := st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}

	victim := newDaemonKeys(t, ts.URL)
	token, _ := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	if err := victim.client.Register(ctx, daemon.RegisterReq{
		JoinToken:  token,
		PubKey:     victim.encPubB64(),
		SignPubKey: victim.signPubB64(),
	}); err != nil {
		t.Fatalf("victim register: %v", err)
	}

	// Attacker signs with its own key but claims the victim's identity in the body.
	attacker := newDaemonKeys(t, ts.URL)
	_, err := attacker.client.TelemetryDue(ctx, daemon.TelemetryDueReq{
		SignPubKey: victim.signPubB64(), // claims victim, but client signs with attacker key
		Timestamp:  time.Now().Unix(),
	})
	if err == nil {
		t.Fatal("expected forged-signature telemetry/due request to be rejected")
	}
	// Assert the request was rejected at the signature check (401), not
	// merely by some other error further down the handler (e.g. 403/500) —
	// distinguishes "the signature check has teeth" from "something failed".
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 (unauthorized/forged signature), got: %v", err)
	}
}

// TestDaemonSeam_TelemetryDueUnregisteredRejected proves an unregistered
// daemon's own, correctly-signed request to telemetry/due is still rejected —
// same identity check as TestDaemonSeam_UnregisteredHeartbeatRejected, applied
// to this endpoint.
func TestDaemonSeam_TelemetryDueUnregisteredRejected(t *testing.T) {
	ts, _ := newSeamTestServer(t)
	d := newDaemonKeys(t, ts.URL)

	_, err := d.client.TelemetryDue(context.Background(), daemon.TelemetryDueReq{
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
	})
	if err == nil {
		t.Fatal("expected telemetry/due from an unregistered daemon to be rejected")
	}
	// Assert rejection happened at GetDaemonBySignPubKey (403), i.e. the
	// signature verified (it's genuinely this key's own signature) but the
	// identity lookup found nothing — distinct from the forged-signature
	// (401) path exercised above.
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 (daemon not registered), got: %v", err)
	}
}

// TestDaemonSeam_TelemetryReportForgedSignatureRejected mirrors
// TestDaemonSeam_ForgedSignatureRejected for the telemetry/report endpoint.
func TestDaemonSeam_TelemetryReportForgedSignatureRejected(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()
	if err := st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}

	victim := newDaemonKeys(t, ts.URL)
	token, _ := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	if err := victim.client.Register(ctx, daemon.RegisterReq{
		JoinToken:  token,
		PubKey:     victim.encPubB64(),
		SignPubKey: victim.signPubB64(),
	}); err != nil {
		t.Fatalf("victim register: %v", err)
	}

	// Attacker signs with its own key but claims the victim's identity in the body.
	attacker := newDaemonKeys(t, ts.URL)
	err := attacker.client.TelemetryReport(ctx, daemon.TelemetryReportReq{
		SignPubKey: victim.signPubB64(), // claims victim, but client signs with attacker key
		Timestamp:  time.Now().Unix(),
		Results:    []daemon.TelemetryPollResult{{PollID: "poll_1"}},
	})
	if err == nil {
		t.Fatal("expected forged-signature telemetry/report request to be rejected")
	}
	// Assert the request was rejected at the signature check (401), not
	// merely by some other error further down the handler.
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 (unauthorized/forged signature), got: %v", err)
	}
}

// TestDaemonSeam_TelemetryReportUnregisteredRejected proves an unregistered
// daemon's own, correctly-signed request to telemetry/report is still
// rejected — same identity check as
// TestDaemonSeam_UnregisteredHeartbeatRejected, applied to this endpoint.
func TestDaemonSeam_TelemetryReportUnregisteredRejected(t *testing.T) {
	ts, _ := newSeamTestServer(t)
	d := newDaemonKeys(t, ts.URL)

	err := d.client.TelemetryReport(context.Background(), daemon.TelemetryReportReq{
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
		Results:    []daemon.TelemetryPollResult{{PollID: "poll_1"}},
	})
	if err == nil {
		t.Fatal("expected telemetry/report from an unregistered daemon to be rejected")
	}
	// Assert rejection happened at GetDaemonBySignPubKey (403), i.e. the
	// signature verified but the identity lookup found nothing — distinct
	// from the forged-signature (401) path exercised above.
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 (daemon not registered), got: %v", err)
	}
}

// TestHandleDaemonTelemetryDueReturnsClaimedPolls proves the due-check seam
// end to end: a registered daemon asking what telemetry is due gets back
// exactly the polls ClaimDuePolls claimed for its org, translated into the
// wire's TelemetryPollSpec shape — with no LeaseID and no sealed credentials
// anywhere in the response, since this channel is independent of the task
// lease queue.
func TestHandleDaemonTelemetryDueReturnsClaimedPolls(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()

	if err := st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}

	d := newDaemonKeys(t, ts.URL)
	token, err := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	if err != nil {
		t.Fatalf("mint join token: %v", err)
	}
	if err := d.client.Register(ctx, daemon.RegisterReq{
		JoinToken: token, PubKey: d.encPubB64(), SignPubKey: d.signPubB64(),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	poll := &store.PostMergeTelemetryPoll{
		ID: "poll_1", OrgID: "o1", MonitorID: "mon_1", Provider: "datadog", Query: "q",
		BaselineStart: time.Now().Add(-24 * time.Hour), BaselineEnd: time.Now().Add(-23 * time.Hour),
		CurrentStart: time.Now().Add(-15 * time.Minute), CurrentEnd: time.Now(),
		NextPollAt: time.Now().Add(-time.Minute), WindowEndsAt: time.Now().Add(4 * time.Hour),
	}
	if err := st.CreateTelemetryPoll(ctx, poll); err != nil {
		t.Fatalf("create telemetry poll: %v", err)
	}

	res, err := d.client.TelemetryDue(ctx, daemon.TelemetryDueReq{
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("telemetry due: %v", err)
	}
	if len(res.Due) != 1 {
		t.Fatalf("got %+v", res.Due)
	}
	got := res.Due[0]
	want := daemon.TelemetryPollSpec{
		PollID:        poll.ID,
		Provider:      poll.Provider,
		Query:         poll.Query,
		BaselineStart: poll.BaselineStart,
		BaselineEnd:   poll.BaselineEnd,
		CurrentStart:  poll.CurrentStart,
		CurrentEnd:    poll.CurrentEnd,
	}
	if got.PollID != want.PollID || got.Provider != want.Provider || got.Query != want.Query ||
		!got.BaselineStart.Equal(want.BaselineStart) || !got.BaselineEnd.Equal(want.BaselineEnd) ||
		!got.CurrentStart.Equal(want.CurrentStart) || !got.CurrentEnd.Equal(want.CurrentEnd) {
		t.Fatalf("spec mapping mismatch:\n got  %+v\n want %+v", got, want)
	}

	// A second due-check immediately after must see nothing: the poll was
	// claimed (claimed_at set), so it is no longer a candidate for
	// ClaimDuePolls. This also proves the first assertion was not vacuous —
	// the claim actually stuck rather than the poll being returned by chance.
	res2, err := d.client.TelemetryDue(ctx, daemon.TelemetryDueReq{
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("second telemetry due: %v", err)
	}
	if len(res2.Due) != 0 {
		t.Fatalf("expected no due polls on second check, got %+v", res2.Due)
	}
}

// TestHandleDaemonTelemetryReportFinalizesOnRegression is a placeholder.
// This test's exact shape depends on Task 10's significance-check function
// existing — write it once Task 10 lands. Assert: given a poll result whose
// current mean is >20% worse than baseline in the configured direction, the
// monitor's status becomes REGRESSION and RecordPollResult was called with
// reschedule=false.
func TestHandleDaemonTelemetryReportFinalizesOnRegression(t *testing.T) {
	t.Skip("completed alongside Task 10's significance check")
}
