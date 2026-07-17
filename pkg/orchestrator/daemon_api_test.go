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
		&store.Credential{}, &store.Daemon{}, &store.DaemonJoinToken{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	s := &Server{db: db, storage: st}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/daemon/register", s.handleDaemonRegister)
	mux.HandleFunc("/api/v1/daemon/heartbeat", s.handleDaemonHeartbeat)
	mux.HandleFunc("/api/v1/daemon/result", s.handleDaemonResult)

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
	token, err := st.CreateDaemonJoinToken(ctx, "o1", time.Hour)
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
	token, _ := st.CreateDaemonJoinToken(ctx, "o1", time.Hour)
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
