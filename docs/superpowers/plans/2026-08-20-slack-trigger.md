# Slack Trigger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an org trigger Kiwi tasks by `@mention`-ing the bot in Slack, with thread-aware context, automatic repo/test-cmd resolution, an editable in-thread status message, thread-reply continuation/fork/new classification, and an investigation-only completion path for requests with no code change to make.

**Architecture:** A new `ee/slackapp` package (BSL, mirrors `ee/githubapp`) handles Slack's wire protocol (signature verification, Events API/interactivity payload parsing, the Slack Web API client). `ee/orchestrator` gets a new webhook receiver and OAuth install flow, plus a trigger pipeline modeled directly on the existing `pr_comment_trigger.go`. `ee/planner` gains one new capability (`SubmitFork`, using the already-reserved-but-unused `store.OriginFork`). `pkg/session`/`pkg/daemon` gain an investigation-only completion path and an Architect-runtime test-cmd fallback for repos with no detectable convention (static convention detection already exists — `pkg/daemon/infer.go` — and needs no new work).

**Tech Stack:** Go 1.25, GORM/Postgres, Next.js/TypeScript frontend, Slack Events API + Web API + OAuth v2.

**Spec:** `docs/superpowers/specs/2026-08-20-slack-trigger-design.md`

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing; `CGO_ENABLED=0 go vet ./...`, `go test ./pkg/...`, `go build ./...` must all pass before every commit (CLAUDE.md §2).
- Everything under `ee/` carries the `LicenseRef-Kiwi-BSL-1.1` header (copy it verbatim from `ee/githubapp/client.go`); `ee/` may import Apache-2.0 packages, never the reverse.
- Every new table/row is `org_id`-scoped; every new orchestrator query filters on it.
- `test_cmd` static inference already exists (`pkg/daemon/infer.go`, called from `pkg/daemon/daemon.go:696-702`) — do not reimplement it. This plan's test-cmd work is only the Architect-runtime fallback for when that inference returns `""`.
- The webhook idiom already established (`github_webhook.go`, `webhook.go`) always answers `200`/`204` except on a signature or parse failure — a non-2xx on a semantic rejection teaches the provider to disable the hook.
- Secrets: store the Slack bot token via the existing `pkg/store` `Credential` mechanism (`SaveCredential`/`GetCredentialPlaintext`, AES-256-GCM at rest) — do not invent new sealing. Add it to the sandbox credential-exclusion list in `pkg/daemon/session_run.go` the same way `SLACK_WEBHOOK_URL` already is.
- README must stay current with any codebase change (CLAUDE.md §3) or the PR needs the `skip-readme-check` label — Task 15 updates it.

---

## Phase A — Data model

### Task 1: Slack store models, migration, credential kind

**Files:**
- Create: `pkg/store/slack_models.go`
- Create: `pkg/store/slack.go`
- Create: `migrations/0043_slack_triggers.up.sql`
- Modify: `pkg/store/store.go` (add method signatures to the `Store` interface)
- Modify: `pkg/store/credentials_models.go` (add `CredentialSlack` kind)

**Interfaces:**
- Produces: `store.SlackInstallation{OrgID, TeamID, TeamName, InstalledByUserID, CreatedAt, UpdatedAt}`, `store.SlackChannelBinding{ID, OrgID, TeamID, ChannelID, RepoURL, DefaultTestCmd, DefaultRef, CreatedBy, CreatedAt}`, `store.SlackTriggeredTask{ID, OrgID, TeamID, ChannelID, ThreadTS, ParentTaskID *string, QueuedTaskID, StatusMessageTS, LastStatus, InvestigationOnly bool, CreatedAt, UpdatedAt}`.
- Produces store methods (added to the `Store` interface and implemented on `*PostgresStore`): `UpsertSlackInstallation`, `GetSlackInstallationByTeamID`, `ListSlackInstallations(orgID)`, `DeleteSlackInstallation(teamID)`, `CreateSlackChannelBinding`, `GetSlackChannelBinding(teamID, channelID)`, `ListSlackChannelBindings(orgID)`, `DeleteSlackChannelBinding(id, orgID)`, `CreateSlackTriggeredTask`, `LatestSlackTriggeredTask(teamID, channelID, threadTS)`, `UpdateSlackTriggeredTaskStatus(id, status, statusMessageTS)`.
- Consumes: nothing (foundation task).

- [ ] **Step 1: Write the failing store tests**

```go
// pkg/store/slack_test.go
package store

import "testing"

func TestUpsertSlackInstallationThenGetByTeamID(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	err := s.UpsertSlackInstallation(ctx, &SlackInstallation{
		TeamID: "T123", OrgID: "org_1", TeamName: "Acme", InstalledByUserID: "user_1",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.GetSlackInstallationByTeamID(ctx, "T123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OrgID != "org_1" || got.TeamName != "Acme" {
		t.Fatalf("got %+v", got)
	}
}

func TestUpsertSlackInstallationRepointsExistingTeam(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	_ = s.UpsertSlackInstallation(ctx, &SlackInstallation{TeamID: "T123", OrgID: "org_1"})
	_ = s.UpsertSlackInstallation(ctx, &SlackInstallation{TeamID: "T123", OrgID: "org_2"})
	got, err := s.GetSlackInstallationByTeamID(ctx, "T123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OrgID != "org_2" {
		t.Fatalf("expected re-pointed org_2, got %s", got.OrgID)
	}
}

func TestChannelBindingRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	b := &SlackChannelBinding{OrgID: "org_1", TeamID: "T123", ChannelID: "C1", RepoURL: "https://github.com/acme/widget"}
	if err := s.CreateSlackChannelBinding(ctx, b); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetSlackChannelBinding(ctx, "T123", "C1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RepoURL != "https://github.com/acme/widget" {
		t.Fatalf("got %+v", got)
	}
}

func TestLatestSlackTriggeredTaskReturnsMostRecent(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	older := &SlackTriggeredTask{OrgID: "org_1", TeamID: "T1", ChannelID: "C1", ThreadTS: "100.1", QueuedTaskID: "task_a"}
	newer := &SlackTriggeredTask{OrgID: "org_1", TeamID: "T1", ChannelID: "C1", ThreadTS: "100.1", QueuedTaskID: "task_b"}
	if err := s.CreateSlackTriggeredTask(ctx, older); err != nil {
		t.Fatalf("create older: %v", err)
	}
	if err := s.CreateSlackTriggeredTask(ctx, newer); err != nil {
		t.Fatalf("create newer: %v", err)
	}
	got, err := s.LatestSlackTriggeredTask(ctx, "T1", "C1", "100.1")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.QueuedTaskID != "task_b" {
		t.Fatalf("expected the newer row (task_b), got %s", got.QueuedTaskID)
	}
}
```

(If the store test suite has no `newTestStore(t)` helper yet, use whatever helper `pkg/store/credentials_test.go` or `pkg/store/github_installations_test.go` already uses — copy that setup exactly rather than inventing a second one.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./pkg/store/... -run 'TestUpsertSlackInstallation|TestChannelBindingRoundTrip|TestLatestSlackTriggeredTask' -v`
Expected: FAIL — undefined `SlackInstallation`, `CreateSlackChannelBinding`, etc.

- [ ] **Step 3: Write the migration**

```sql
-- migrations/0043_slack_triggers.up.sql
CREATE TABLE slack_installations (
    team_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    team_name TEXT NOT NULL DEFAULT '',
    installed_by_user_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_slack_installations_org ON slack_installations (org_id);

CREATE TABLE slack_channel_bindings (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    team_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    default_test_cmd TEXT NOT NULL DEFAULT '',
    default_ref TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_slack_channel_bindings_channel ON slack_channel_bindings (team_id, channel_id);
CREATE INDEX idx_slack_channel_bindings_org ON slack_channel_bindings (org_id);

CREATE TABLE slack_triggered_tasks (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    team_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    thread_ts TEXT NOT NULL,
    parent_task_id TEXT,
    queued_task_id TEXT NOT NULL DEFAULT '',
    status_message_ts TEXT NOT NULL DEFAULT '',
    last_status TEXT NOT NULL DEFAULT '',
    investigation_only BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_slack_triggered_tasks_thread ON slack_triggered_tasks (team_id, channel_id, thread_ts, created_at DESC);
```

- [ ] **Step 4: Write the store models**

```go
// pkg/store/slack_models.go
package store

import "time"

// CredentialSlack marks a Slack bot token, stored the same way an LLM key or
// git token is (SaveCredential/GetCredentialPlaintext, AES-256-GCM at rest).
const CredentialSlack = "slack"

// SlackInstallation links one Kiwi org to one Slack workspace ("team"),
// mirroring GitHubInstallation's shape: the team, not the channel, is the
// unit OAuth grants against.
type SlackInstallation struct {
	TeamID            string    `gorm:"primaryKey" json:"team_id"`
	OrgID             string    `gorm:"index;not null" json:"org_id"`
	TeamName          string    `gorm:"not null;default:''" json:"team_name"`
	InstalledByUserID string    `gorm:"not null;default:''" json:"installed_by_user_id"`
	CreatedAt         time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt         time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (SlackInstallation) TableName() string { return "slack_installations" }

// SlackChannelBinding pins one Slack channel to a repo (and optionally a
// default test command / ref), set once by an admin so a bare "@runkiwi fix
// this bug" in that channel knows what "this" refers to.
type SlackChannelBinding struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	OrgID          string    `gorm:"index;not null" json:"org_id"`
	TeamID         string    `gorm:"not null;uniqueIndex:idx_scb_channel,priority:1" json:"team_id"`
	ChannelID      string    `gorm:"not null;uniqueIndex:idx_scb_channel,priority:2" json:"channel_id"`
	RepoURL        string    `gorm:"not null" json:"repo_url"`
	DefaultTestCmd string    `gorm:"not null;default:''" json:"default_test_cmd"`
	DefaultRef     string    `gorm:"not null;default:''" json:"default_ref"`
	CreatedBy      string    `gorm:"not null;default:''" json:"created_by"`
	CreatedAt      time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (SlackChannelBinding) TableName() string { return "slack_channel_bindings" }

// SlackTriggeredTask maps a Kiwi task back to the Slack thread that started
// it. Not one row per thread — a thread can accumulate several tasks over
// time (fork, new), so callers always want the LATEST row for a ThreadTS.
type SlackTriggeredTask struct {
	ID                 string    `gorm:"primaryKey" json:"id"`
	OrgID              string    `gorm:"index;not null" json:"org_id"`
	TeamID             string    `gorm:"not null" json:"team_id"`
	ChannelID          string    `gorm:"not null" json:"channel_id"`
	ThreadTS           string    `gorm:"not null" json:"thread_ts"`
	ParentTaskID       *string   `json:"parent_task_id,omitempty"`
	QueuedTaskID       string    `gorm:"not null;default:''" json:"queued_task_id"`
	StatusMessageTS    string    `gorm:"not null;default:''" json:"status_message_ts"`
	LastStatus         string    `gorm:"not null;default:''" json:"last_status"`
	InvestigationOnly  bool      `gorm:"not null;default:false" json:"investigation_only"`
	CreatedAt          time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt          time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (SlackTriggeredTask) TableName() string { return "slack_triggered_tasks" }
```

- [ ] **Step 5: Write the store methods**

```go
// pkg/store/slack.go
package store

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *PostgresStore) UpsertSlackInstallation(ctx context.Context, inst *SlackInstallation) error {
	if inst == nil || inst.TeamID == "" {
		return errors.New("slack installation needs a team id")
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "team_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"org_id", "team_name", "installed_by_user_id", "updated_at"}),
	}).Create(inst).Error
}

func (s *PostgresStore) GetSlackInstallationByTeamID(ctx context.Context, teamID string) (*SlackInstallation, error) {
	var inst SlackInstallation
	if err := s.db.WithContext(ctx).Where("team_id = ?", teamID).First(&inst).Error; err != nil {
		return nil, err
	}
	return &inst, nil
}

func (s *PostgresStore) ListSlackInstallations(ctx context.Context, orgID string) ([]SlackInstallation, error) {
	var out []SlackInstallation
	err := s.db.WithContext(ctx).Where("org_id = ?", orgID).Order("created_at desc").Find(&out).Error
	return out, err
}

func (s *PostgresStore) DeleteSlackInstallation(ctx context.Context, teamID string) error {
	return s.db.WithContext(ctx).Delete(&SlackInstallation{}, "team_id = ?", teamID).Error
}

func (s *PostgresStore) CreateSlackChannelBinding(ctx context.Context, b *SlackChannelBinding) error {
	if b.ID == "" {
		b.ID = "scb_" + randHex(8)
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "team_id"}, {Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"repo_url", "default_test_cmd", "default_ref"}),
	}).Create(b).Error
}

func (s *PostgresStore) GetSlackChannelBinding(ctx context.Context, teamID, channelID string) (*SlackChannelBinding, error) {
	var b SlackChannelBinding
	err := s.db.WithContext(ctx).Where("team_id = ? AND channel_id = ?", teamID, channelID).First(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *PostgresStore) ListSlackChannelBindings(ctx context.Context, orgID string) ([]SlackChannelBinding, error) {
	var out []SlackChannelBinding
	err := s.db.WithContext(ctx).Where("org_id = ?", orgID).Order("created_at desc").Find(&out).Error
	return out, err
}

func (s *PostgresStore) DeleteSlackChannelBinding(ctx context.Context, id, orgID string) error {
	return s.db.WithContext(ctx).Delete(&SlackChannelBinding{}, "id = ? AND org_id = ?", id, orgID).Error
}

func (s *PostgresStore) CreateSlackTriggeredTask(ctx context.Context, t *SlackTriggeredTask) error {
	if t.ID == "" {
		t.ID = "stt_" + randHex(8)
	}
	return s.db.WithContext(ctx).Create(t).Error
}

// LatestSlackTriggeredTask is "current context" for a thread: the most
// recent task any prior trigger in this thread produced, whichever of
// continue/fork/new created it.
func (s *PostgresStore) LatestSlackTriggeredTask(ctx context.Context, teamID, channelID, threadTS string) (*SlackTriggeredTask, error) {
	var t SlackTriggeredTask
	err := s.db.WithContext(ctx).
		Where("team_id = ? AND channel_id = ? AND thread_ts = ?", teamID, channelID, threadTS).
		Order("created_at desc").
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *PostgresStore) UpdateSlackTriggeredTaskStatus(ctx context.Context, id, status, statusMessageTS string) error {
	updates := map[string]interface{}{"last_status": status}
	if statusMessageTS != "" {
		updates["status_message_ts"] = statusMessageTS
	}
	return s.db.WithContext(ctx).Model(&SlackTriggeredTask{}).Where("id = ?", id).Updates(updates).Error
}
```

`randHex` already exists in `pkg/store` (used by other id-generating helpers) — if it doesn't, copy the 8-byte hex generator from `ee/planner/service.go`'s `randHex`.

- [ ] **Step 6: Add the new methods to the `Store` interface**

Open `pkg/store/store.go`, find the block of `GitHubInstallation`-related method signatures, and add directly below it:

```go
	// Slack triggers (ee/orchestrator's slack_* files).
	UpsertSlackInstallation(ctx context.Context, inst *SlackInstallation) error
	GetSlackInstallationByTeamID(ctx context.Context, teamID string) (*SlackInstallation, error)
	ListSlackInstallations(ctx context.Context, orgID string) ([]SlackInstallation, error)
	DeleteSlackInstallation(ctx context.Context, teamID string) error
	CreateSlackChannelBinding(ctx context.Context, b *SlackChannelBinding) error
	GetSlackChannelBinding(ctx context.Context, teamID, channelID string) (*SlackChannelBinding, error)
	ListSlackChannelBindings(ctx context.Context, orgID string) ([]SlackChannelBinding, error)
	DeleteSlackChannelBinding(ctx context.Context, id, orgID string) error
	CreateSlackTriggeredTask(ctx context.Context, t *SlackTriggeredTask) error
	LatestSlackTriggeredTask(ctx context.Context, teamID, channelID, threadTS string) (*SlackTriggeredTask, error)
	UpdateSlackTriggeredTaskStatus(ctx context.Context, id, status, statusMessageTS string) error
```

Add `CredentialSlack = "slack"` next to the other `Credential*` constants in `pkg/store/credentials_models.go` if Step 4 didn't already land it there (it's declared in `slack_models.go` above for locality with the rest of this task — either file is fine, just don't declare it twice).

- [ ] **Step 7: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./pkg/store/... -run 'TestUpsertSlackInstallation|TestChannelBindingRoundTrip|TestLatestSlackTriggeredTask' -v`
Expected: PASS

- [ ] **Step 8: Full package build + vet**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...`
Expected: clean (this catches any other `store.Store` implementation — e.g. a test fake — that now needs the new methods too; add them there if the build fails naming one).

- [ ] **Step 9: Commit**

```bash
git add pkg/store/slack_models.go pkg/store/slack.go pkg/store/slack_test.go pkg/store/store.go migrations/0043_slack_triggers.up.sql
git commit -m "feat(store): add Slack installation, channel binding, and triggered-task models"
```

---

## Phase B — Slack wire protocol

### Task 2: Signature verification and event/payload parsing (pure functions)

**Files:**
- Create: `ee/slackapp/signature.go`
- Create: `ee/slackapp/signature_test.go`
- Create: `ee/slackapp/events.go`
- Create: `ee/slackapp/events_test.go`

**Interfaces:**
- Produces: `slackapp.VerifySignature(secret string, timestamp string, body []byte, signature string) bool`; `slackapp.ParseEvent(body []byte) (Event, bool)` where `Event{Type, Challenge, TeamID, EventType, UserID, Text, ChannelID, TS, ThreadTS}`; `slackapp.ParseInteractivity(formBody []byte) (Interactivity, bool)` where `Interactivity{Type, TeamID, ChannelID, MessageTS, UserID, ActionID, ActionValue}`.
- Consumes: nothing.

- [ ] **Step 1: Write the failing signature test**

```go
// ee/slackapp/signature_test.go
package slackapp

import "testing"

func TestVerifySignatureAcceptsAValidV0Signature(t *testing.T) {
	secret := "shhh"
	ts := "1531420618"
	body := []byte(`{"type":"url_verification"}`)
	// Computed offline as HMAC-SHA256("shhh", "v0:1531420618:"+body), hex-encoded.
	sig := computeTestSig(t, secret, ts, body)
	if !VerifySignature(secret, ts, body, "v0="+sig) {
		t.Fatal("expected a correctly computed signature to verify")
	}
}

func TestVerifySignatureRejectsWrongSecret(t *testing.T) {
	ts := "1531420618"
	body := []byte(`{"type":"url_verification"}`)
	sig := computeTestSig(t, "shhh", ts, body)
	if VerifySignature("different-secret", ts, body, "v0="+sig) {
		t.Fatal("expected verification to fail with the wrong secret")
	}
}

func TestVerifySignatureRejectsMalformedHeader(t *testing.T) {
	if VerifySignature("shhh", "1531420618", []byte("{}"), "not-v0-prefixed") {
		t.Fatal("expected a malformed signature header to be rejected")
	}
}

func computeTestSig(t *testing.T, secret, ts string, body []byte) string {
	t.Helper()
	mac := hmacSHA256(secret, "v0:"+ts+":"+string(body))
	return mac
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/slackapp/... -run TestVerifySignature -v`
Expected: FAIL — package `slackapp` and `VerifySignature`/`hmacSHA256` don't exist yet.

- [ ] **Step 3: Implement signature verification**

```go
// ee/slackapp/signature.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package slackapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifySignature checks Slack's request signature: HMAC-SHA256 over
// "v0:{timestamp}:{body}", keyed on the app's signing secret. Slack's own
// spec: https://api.slack.com/authentication/verifying-requests-from-slack.
func VerifySignature(secret, timestamp string, body []byte, signatureHeader string) bool {
	if !strings.HasPrefix(signatureHeader, "v0=") {
		return false
	}
	want := hmacSHA256(secret, "v0:"+timestamp+":"+string(body))
	got := strings.TrimPrefix(signatureHeader, "v0=")
	return hmac.Equal([]byte(want), []byte(got))
}

func hmacSHA256(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
```

- [ ] **Step 4: Run the signature test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/slackapp/... -run TestVerifySignature -v`
Expected: PASS

- [ ] **Step 5: Write the failing event-parsing tests**

```go
// ee/slackapp/events_test.go
package slackapp

import "testing"

func TestParseEventHandlesURLVerification(t *testing.T) {
	body := []byte(`{"type":"url_verification","challenge":"abc123"}`)
	ev, ok := ParseEvent(body)
	if !ok || ev.Type != "url_verification" || ev.Challenge != "abc123" {
		t.Fatalf("got %+v, ok=%v", ev, ok)
	}
}

func TestParseEventExtractsAppMention(t *testing.T) {
	body := []byte(`{
		"type": "event_callback",
		"team_id": "T123",
		"event": {
			"type": "app_mention",
			"user": "U1",
			"text": "<@U0BOT> fix this bug",
			"channel": "C1",
			"ts": "100.001",
			"thread_ts": "99.000"
		}
	}`)
	ev, ok := ParseEvent(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.TeamID != "T123" || ev.EventType != "app_mention" || ev.ChannelID != "C1" || ev.TS != "100.001" || ev.ThreadTS != "99.000" || ev.UserID != "U1" {
		t.Fatalf("got %+v", ev)
	}
}

func TestParseEventRejectsUnrecognizedShape(t *testing.T) {
	if _, ok := ParseEvent([]byte(`{"not":"slack"}`)); ok {
		t.Fatal("expected ok=false for an unrecognized payload")
	}
}

func TestParseInteractivityExtractsBlockActions(t *testing.T) {
	form := []byte(`payload=` + urlEncodedTestPayload)
	in, ok := ParseInteractivity(form)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if in.Type != "block_actions" || in.TeamID != "T123" || in.ChannelID != "C1" || in.ActionID != "fork" {
		t.Fatalf("got %+v", in)
	}
}

// urlEncodedTestPayload is url.QueryEscape of:
// {"type":"block_actions","team":{"id":"T123"},"channel":{"id":"C1"},
//  "message":{"ts":"100.001"},"user":{"id":"U1"},
//  "actions":[{"action_id":"fork","value":"stt_abc"}]}
const urlEncodedTestPayload = `%7B%22type%22%3A%22block_actions%22%2C%22team%22%3A%7B%22id%22%3A%22T123%22%7D%2C%22channel%22%3A%7B%22id%22%3A%22C1%22%7D%2C%22message%22%3A%7B%22ts%22%3A%22100.001%22%7D%2C%22user%22%3A%7B%22id%22%3A%22U1%22%7D%2C%22actions%22%3A%5B%7B%22action_id%22%3A%22fork%22%2C%22value%22%3A%22stt_abc%22%7D%5D%7D`
```

- [ ] **Step 6: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/slackapp/... -run 'TestParseEvent|TestParseInteractivity' -v`
Expected: FAIL — `ParseEvent`, `ParseInteractivity`, `Event`, `Interactivity` undefined.

- [ ] **Step 7: Implement event and interactivity parsing**

```go
// ee/slackapp/events.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package slackapp

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Event is a Slack Events API delivery, reduced to what a trigger needs.
// Everything here is a pure function over the wire payload, on purpose — the
// same reasoning ee/orchestrator/pr_comment.go gives: cheap to table-test,
// no DB or network involved in deciding what an event even is.
type Event struct {
	Type      string // "url_verification" or "event_callback"
	Challenge string // set only for url_verification
	TeamID    string
	EventType string // "app_mention", "message", ...
	UserID    string
	Text      string
	ChannelID string
	TS        string
	ThreadTS  string // empty when the message is not in a thread
}

type eventPayload struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	TeamID    string `json:"team_id"`
	Event     struct {
		Type     string `json:"type"`
		User     string `json:"user"`
		Text     string `json:"text"`
		Channel  string `json:"channel"`
		TS       string `json:"ts"`
		ThreadTS string `json:"thread_ts"`
	} `json:"event"`
}

// ParseEvent extracts an Event from a webhook body, reporting false when the
// payload is not a shape this integration recognizes.
func ParseEvent(body []byte) (Event, bool) {
	var p eventPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, false
	}
	switch p.Type {
	case "url_verification":
		if p.Challenge == "" {
			return Event{}, false
		}
		return Event{Type: p.Type, Challenge: p.Challenge}, true
	case "event_callback":
		if p.Event.Type == "" {
			return Event{}, false
		}
		return Event{
			Type:      p.Type,
			TeamID:    p.TeamID,
			EventType: p.Event.Type,
			UserID:    p.Event.User,
			Text:      p.Event.Text,
			ChannelID: p.Event.Channel,
			TS:        p.Event.TS,
			ThreadTS:  p.Event.ThreadTS,
		}, true
	default:
		return Event{}, false
	}
}

// Interactivity is a Block Kit button click (the continue/fork/new
// disambiguation prompt), delivered as application/x-www-form-urlencoded
// with the real JSON in a single "payload" field.
type Interactivity struct {
	Type        string // "block_actions"
	TeamID      string
	ChannelID   string
	MessageTS   string
	UserID      string
	ActionID    string
	ActionValue string
}

type interactivityPayload struct {
	Type string `json:"type"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Message struct {
		TS string `json:"ts"`
	} `json:"message"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

// ParseInteractivity extracts a button click from an interactivity delivery.
func ParseInteractivity(formBody []byte) (Interactivity, bool) {
	values, err := url.ParseQuery(string(formBody))
	if err != nil {
		return Interactivity{}, false
	}
	raw := values.Get("payload")
	if raw == "" {
		return Interactivity{}, false
	}
	var p interactivityPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return Interactivity{}, false
	}
	if p.Type != "block_actions" || len(p.Actions) == 0 {
		return Interactivity{}, false
	}
	act := p.Actions[0]
	if strings.TrimSpace(act.ActionID) == "" {
		return Interactivity{}, false
	}
	return Interactivity{
		Type:        p.Type,
		TeamID:      p.Team.ID,
		ChannelID:   p.Channel.ID,
		MessageTS:   p.Message.TS,
		UserID:      p.User.ID,
		ActionID:    act.ActionID,
		ActionValue: act.Value,
	}, true
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/slackapp/... -v`
Expected: PASS, all of Task 2's tests.

- [ ] **Step 9: Commit**

```bash
git add ee/slackapp/signature.go ee/slackapp/signature_test.go ee/slackapp/events.go ee/slackapp/events_test.go
git commit -m "feat(slackapp): add request signature verification and event/interactivity parsing"
```

### Task 3: Slack Web API client

**Files:**
- Create: `ee/slackapp/client.go`
- Create: `ee/slackapp/client_test.go`

**Interfaces:**
- Produces: `slackapp.Client{}` built via `slackapp.New(opts ...Option) *Client`, with methods `PostMessage(ctx, token, channel, threadTS, text string) (ts string, err error)`, `EditMessage(ctx, token, channel, ts, text string) error`, `AddReaction(ctx, token, channel, ts, emoji string) error`, `RemoveReaction(ctx, token, channel, ts, emoji string) error`, `ConversationHistory(ctx, token, channel string, oldest string, limit int) ([]Message, error)`, `ConversationReplies(ctx, token, channel, threadTS string) ([]Message, error)`, `PostInteractiveButtons(ctx, token, channel, threadTS, text string, buttons []Button) (ts string, err error)`, `ExchangeOAuthCode(ctx, clientID, clientSecret, code, redirectURI string) (OAuthResult, error)`.
- Consumes: nothing new (mirrors `ee/githubapp/client.go`'s `Option`/`WithHTTPClient` shape).

- [ ] **Step 1: Write the failing client test (against an `httptest.Server`, same style as `ee/githubapp`'s own client tests — check `ee/githubapp/client_test.go` for the exact stub-server pattern before writing this)**

```go
// ee/slackapp/client_test.go
package slackapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostMessageReturnsTS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": "100.001"})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	ts, err := c.PostMessage(t.Context(), "xoxb-test", "C1", "", "hello")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if ts != "100.001" {
		t.Fatalf("got ts %q", ts)
	}
}

func TestPostMessageReturnsErrorOnSlackNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "channel_not_found"})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.PostMessage(t.Context(), "xoxb-test", "C1", "", "hello"); err == nil {
		t.Fatal("expected an error when Slack reports ok=false")
	}
}

func TestAddReactionPostsExpectedFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if err := c.AddReaction(t.Context(), "xoxb-test", "C1", "100.001", "eyes"); err != nil {
		t.Fatalf("AddReaction: %v", err)
	}
	if gotBody["channel"] != "C1" || gotBody["timestamp"] != "100.001" || gotBody["name"] != "eyes" {
		t.Fatalf("got body %+v", gotBody)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/slackapp/... -run 'TestPostMessage|TestAddReaction' -v`
Expected: FAIL — `New`, `WithBaseURL`, `Client.PostMessage`, `Client.AddReaction` undefined.

- [ ] **Step 3: Implement the client**

```go
// ee/slackapp/client.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package slackapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls the Slack Web API. Every call carries the caller-supplied
// bot token explicitly rather than the Client holding one — a single Client
// serves every installed workspace, the same way ee/githubapp's Client mints
// tokens per-installation rather than being built for one.
type Client struct {
	http    *http.Client
	baseURL string
}

type Option func(*Client)

func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func New(opts ...Option) *Client {
	c := &Client{http: &http.Client{Timeout: 15 * time.Second}, baseURL: "https://slack.com/api"}
	for _, fn := range opts {
		fn(c)
	}
	return c
}

type slackEnvelope struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	TS    string `json:"ts"`
}

func (c *Client) post(ctx context.Context, token, method string, body map[string]interface{}) (slackEnvelope, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return slackEnvelope{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return slackEnvelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return slackEnvelope{}, fmt.Errorf("slackapp: %s: %w", method, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var env slackEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return slackEnvelope{}, fmt.Errorf("slackapp: %s: decode response: %w", method, err)
	}
	if !env.OK {
		return env, fmt.Errorf("slackapp: %s: %s", method, env.Error)
	}
	return env, nil
}

// PostMessage sends a new message, optionally into an existing thread
// (threadTS empty means a fresh top-level message). Returns the new
// message's own ts, which callers persist as the editable status message id.
func (c *Client) PostMessage(ctx context.Context, token, channel, threadTS, text string) (string, error) {
	body := map[string]interface{}{"channel": channel, "text": text}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}
	env, err := c.post(ctx, token, "chat.postMessage", body)
	if err != nil {
		return "", err
	}
	return env.TS, nil
}

// EditMessage overwrites an existing message's text — how the status
// message moves through created -> running -> done without spamming the
// channel with a new message per transition.
func (c *Client) EditMessage(ctx context.Context, token, channel, ts, text string) error {
	_, err := c.post(ctx, token, "chat.update", map[string]interface{}{"channel": channel, "ts": ts, "text": text})
	return err
}

func (c *Client) AddReaction(ctx context.Context, token, channel, ts, emoji string) error {
	_, err := c.post(ctx, token, "reactions.add", map[string]interface{}{"channel": channel, "timestamp": ts, "name": emoji})
	return err
}

func (c *Client) RemoveReaction(ctx context.Context, token, channel, ts, emoji string) error {
	_, err := c.post(ctx, token, "reactions.remove", map[string]interface{}{"channel": channel, "timestamp": ts, "name": emoji})
	// already_reacted / no_reaction races are not failures worth surfacing —
	// the emoji swap is a best-effort UI touch, same posture as the GitHub
	// comment-trigger's acknowledge().
	if err != nil && strings.Contains(err.Error(), "no_reaction") {
		return nil
	}
	return err
}

// Message is one line of channel/thread history, reduced to what context
// assembly (Task 8) needs.
type Message struct {
	UserID string `json:"user"`
	Text   string `json:"text"`
	TS     string `json:"ts"`
}

func (c *Client) history(ctx context.Context, token, method, channel string, extra url.Values) ([]Message, error) {
	q := url.Values{"channel": {channel}}
	for k, v := range extra {
		q[k] = v
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+method+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slackapp: %s: %w", method, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	var out struct {
		OK       bool      `json:"ok"`
		Error    string    `json:"error"`
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("slackapp: %s: decode: %w", method, err)
	}
	if !out.OK {
		return nil, fmt.Errorf("slackapp: %s: %s", method, out.Error)
	}
	return out.Messages, nil
}

// ConversationHistory returns channel messages, newest first, capped at
// limit — the fixed-lookback half of context assembly (Task 8).
func (c *Client) ConversationHistory(ctx context.Context, token, channel string, limit int) ([]Message, error) {
	return c.history(ctx, token, "conversations.history", channel, url.Values{"limit": {fmt.Sprintf("%d", limit)}})
}

// ConversationReplies returns a whole thread, oldest first.
func (c *Client) ConversationReplies(ctx context.Context, token, channel, threadTS string) ([]Message, error) {
	return c.history(ctx, token, "conversations.replies", channel, url.Values{"ts": {threadTS}})
}

// Button is one option in the continue/fork/new disambiguation prompt.
type Button struct {
	Label    string
	ActionID string
	Value    string
}

// PostInteractiveButtons posts a Block Kit message with one button per
// Button, for the ambiguous thread-reply case (Task 11).
func (c *Client) PostInteractiveButtons(ctx context.Context, token, channel, threadTS, text string, buttons []Button) (string, error) {
	var elements []map[string]interface{}
	for _, b := range buttons {
		elements = append(elements, map[string]interface{}{
			"type":      "button",
			"text":      map[string]interface{}{"type": "plain_text", "text": b.Label},
			"action_id": b.ActionID,
			"value":     b.Value,
		})
	}
	blocks := []map[string]interface{}{
		{"type": "section", "text": map[string]interface{}{"type": "mrkdwn", "text": text}},
		{"type": "actions", "elements": elements},
	}
	env, err := c.post(ctx, token, "chat.postMessage", map[string]interface{}{
		"channel": channel, "thread_ts": threadTS, "text": text, "blocks": blocks,
	})
	if err != nil {
		return "", err
	}
	return env.TS, nil
}

// OAuthResult is what Slack's oauth.v2.access returns on a successful
// install: the bot token to store, and which team it belongs to.
type OAuthResult struct {
	AccessToken string
	TeamID      string
	TeamName    string
}

var ErrOAuthExchangeFailed = errors.New("slackapp: oauth exchange failed")

// ExchangeOAuthCode trades the "Add to Slack" redirect's code for a bot
// token — the Slack half of the install flow Task 4 wires end to end.
func (c *Client) ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (OAuthResult, error) {
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth.v2.access", strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return OAuthResult{}, fmt.Errorf("slackapp: oauth exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var out struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		Team        struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return OAuthResult{}, fmt.Errorf("slackapp: oauth exchange: decode: %w", err)
	}
	if !out.OK || out.AccessToken == "" {
		return OAuthResult{}, fmt.Errorf("%w: %s", ErrOAuthExchangeFailed, out.Error)
	}
	return OAuthResult{AccessToken: out.AccessToken, TeamID: out.Team.ID, TeamName: out.Team.Name}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/slackapp/... -v`
Expected: PASS, all of Task 3's tests plus Task 2's (no regressions).

- [ ] **Step 5: Commit**

```bash
git add ee/slackapp/client.go ee/slackapp/client_test.go
git commit -m "feat(slackapp): add Slack Web API client (messages, reactions, history, OAuth exchange)"
```

---

## Phase C — OAuth install

### Task 4: "Add to Slack" install flow

**Files:**
- Create: `ee/orchestrator/slack_install_api.go`
- Create: `ee/orchestrator/slack_install_api_test.go`
- Modify: `ee/orchestrator/server.go` (add the `slackClient *slackapp.Client` field, construct it in `NewServer`, register the three new routes)

**Interfaces:**
- Consumes: `slackapp.New()`, `slackapp.Client.ExchangeOAuthCode` (Task 3); `store.UpsertSlackInstallation`, `store.SaveCredential` (Task 1); the existing `installState`/`signInstallState`/`verifyInstallState` helpers already defined in `ee/orchestrator/github_install_api.go` — reuse them as-is rather than duplicating a second signed-state mechanism (they are not GitHub-specific despite the file they live in).
- Produces: `GET /api/v1/integrations/slack/install`, `GET /api/v1/integrations/slack/oauth/callback`, `GET /api/v1/integrations/slack/installations`.

- [ ] **Step 1: Write the failing test for the install-URL endpoint**

```go
// ee/orchestrator/slack_install_api_test.go
package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
)

func TestHandleSlackInstallReturnsJSONURLWhenAcceptHeaderAsks(t *testing.T) {
	os.Setenv("KIWI_SLACK_CLIENT_ID", "client-123")
	defer os.Unsetenv("KIWI_SLACK_CLIENT_ID")

	s := newTestServer(t) // reuse whatever test-server constructor github_install_api_test.go uses
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack/install", nil)
	req.Header.Set("Accept", "application/json")
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleSlackInstall(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !containsAll(w.Body.String(), "slack.com/oauth/v2/authorize", "client-123", "state=") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestHandleSlackInstallRejectsUnauthenticated(t *testing.T) {
	os.Setenv("KIWI_SLACK_CLIENT_ID", "client-123")
	defer os.Unsetenv("KIWI_SLACK_CLIENT_ID")

	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack/install", nil)
	w := httptest.NewRecorder()
	s.handleSlackInstall(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
```

(`newTestServer(t)` and `containsAll` — use whatever test-server-construction helper and string-assertion helper `ee/orchestrator/github_install_api_test.go` already defines; do not build a second one.)

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestHandleSlackInstall -v`
Expected: FAIL — `handleSlackInstall` undefined.

- [ ] **Step 3: Implement the install flow**

```go
// ee/orchestrator/slack_install_api.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// slackScopes is the fixed set of bot scopes this integration asks for:
// posting/editing messages, reacting, reading channel and thread history,
// and receiving app_mention events. Kept as one constant rather than
// per-install configuration — every workspace gets the same bot.
const slackScopes = "app_mentions:read,chat:write,reactions:write,reactions:read,channels:history,groups:history,im:history"

func slackRedirectURI() string {
	return strings.TrimRight(dashboardAPIBaseURL(), "/") + "/api/v1/integrations/slack/oauth/callback"
}

// dashboardAPIBaseURL is the Control Plane's own public URL — reuse
// whatever env var github_install_api.go / server.go already reads for
// this (e.g. KIWI_API_BASE_URL); do not introduce a second name for the
// same concept. Adjust this helper to call that existing one directly if
// it already exists under a different name.
func dashboardAPIBaseURL() string {
	return strings.TrimRight(os.Getenv("KIWI_API_BASE_URL"), "/")
}

// handleSlackInstall serves GET /api/v1/integrations/slack/install,
// mirroring handleGithubInstall (github_install_api.go) exactly: the org is
// taken from the caller's own credentials and sealed into the signed state,
// never read back from Slack's redirect.
func (s *Server) handleSlackInstall(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	clientID := strings.TrimSpace(os.Getenv("KIWI_SLACK_CLIENT_ID"))
	if clientID == "" {
		http.Error(w, "slack app is not configured", http.StatusNotImplemented)
		return
	}

	state, err := signInstallState(installState{
		OrgID:  claims.OrgID,
		UserID: claims.UserID,
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	target := fmt.Sprintf("https://slack.com/oauth/v2/authorize?client_id=%s&scope=%s&redirect_uri=%s&state=%s",
		url.QueryEscape(clientID), url.QueryEscape(slackScopes), url.QueryEscape(slackRedirectURI()), url.QueryEscape(state))

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, http.StatusOK, map[string]string{"install_url": target})
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleSlackOAuthCallback serves GET /api/v1/integrations/slack/oauth/callback.
// Unauthenticated on purpose, exactly like handleGithubCallback: the signed
// state is the credential proving which org started this install.
func (s *Server) handleSlackOAuthCallback(w http.ResponseWriter, r *http.Request) {
	st, err := verifyInstallState(r.URL.Query().Get("state"))
	if err != nil {
		log.Printf("[slackapp] install callback rejected: %v", err)
		http.Error(w, "invalid or expired install link", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	clientID := os.Getenv("KIWI_SLACK_CLIENT_ID")
	clientSecret := os.Getenv("KIWI_SLACK_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" || s.slackClient == nil {
		http.Error(w, "slack app is not configured", http.StatusNotImplemented)
		return
	}

	result, err := s.slackClient.ExchangeOAuthCode(r.Context(), clientID, clientSecret, code, slackRedirectURI())
	if err != nil {
		log.Printf("[slackapp] oauth exchange failed: %v", err)
		http.Error(w, "could not complete the Slack install", http.StatusBadGateway)
		return
	}

	if err := s.storage.SaveCredential(r.Context(), st.OrgID, "SLACK_BOT_TOKEN", store.CredentialSlack, result.AccessToken); err != nil {
		log.Printf("[slackapp] persist bot token for %s: %v", st.OrgID, err)
		http.Error(w, "could not save the installation", http.StatusInternalServerError)
		return
	}
	if err := s.storage.UpsertSlackInstallation(r.Context(), &store.SlackInstallation{
		TeamID: result.TeamID, OrgID: st.OrgID, TeamName: result.TeamName, InstalledByUserID: st.UserID,
	}); err != nil {
		log.Printf("[slackapp] persist installation for %s: %v", st.OrgID, err)
		http.Error(w, "could not save the installation", http.StatusInternalServerError)
		return
	}

	log.Printf("[slackapp] org %s connected Slack workspace %s (team %s)", st.OrgID, result.TeamName, result.TeamID)
	http.Redirect(w, r, dashboardURL()+"/integrations?slack=connected", http.StatusFound)
}

// handleSlackInstallations serves GET /api/v1/integrations/slack/installations
// so the dashboard can show what's connected.
func (s *Server) handleSlackInstallations(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	list, err := s.storage.ListSlackInstallations(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []store.SlackInstallation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"installations": list})
}
```

If `installState` in `github_install_api.go` has no `Nonce`/`ExpiresAt` set anywhere convenient to reuse verbatim, keep using the exact same struct and signing helpers — do not create a parallel `slackInstallState`; two independent state-signing schemes in the same package is the kind of duplication this reuse is meant to avoid. Adjust field population above (`Nonce`, `ExpiresAt`) to match `signInstallState`'s real required fields exactly as they're already defined.

- [ ] **Step 4: Wire the Server struct and routes**

In `ee/orchestrator/server.go`, add the field next to `githubApp`:

```go
	// slackClient calls the Slack Web API on behalf of every connected
	// workspace; nil is valid and means Slack triggering is not configured.
	slackClient *slackapp.Client
```

In `NewServer`, next to `githubApp: newGitHubAppClient()`:

```go
		slackClient: slackapp.New(),
```

Near the other `/api/v1/integrations/...`-style routes (search for `handleGithubInstall`'s registration):

```go
	mux.HandleFunc("/api/v1/integrations/slack/install", s.handleSlackInstall)
	mux.HandleFunc("/api/v1/integrations/slack/installations", s.handleSlackInstallations)
```

Next to `root.HandleFunc("/api/v1/github/callback", ...)` (unauthenticated root mux, since the callback is unauthenticated):

```go
	root.HandleFunc("/api/v1/integrations/slack/oauth/callback", s.handleSlackOAuthCallback)
```

Add the import `"github.com/ibreakthecloud/kiwi/ee/slackapp"`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestHandleSlackInstall -v`
Expected: PASS

- [ ] **Step 6: Full build**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add ee/orchestrator/slack_install_api.go ee/orchestrator/slack_install_api_test.go ee/orchestrator/server.go
git commit -m "feat(orchestrator): add Slack OAuth install flow"
```

---

## Phase D — Channel bindings + first-trigger happy path

### Task 5: Channel bindings CRUD API

**Files:**
- Create: `ee/orchestrator/slack_bindings_api.go`
- Create: `ee/orchestrator/slack_bindings_api_test.go`
- Modify: `ee/orchestrator/server.go` (register routes)

**Interfaces:**
- Consumes: `store.CreateSlackChannelBinding`, `store.ListSlackChannelBindings`, `store.DeleteSlackChannelBinding` (Task 1).
- Produces: `POST /api/v1/integrations/slack/bindings`, `GET /api/v1/integrations/slack/bindings`, `DELETE /api/v1/integrations/slack/bindings/{id}`.

- [ ] **Step 1: Write the failing test**

```go
// ee/orchestrator/slack_bindings_api_test.go
package orchestrator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
)

func TestHandleCreateSlackBindingPersistsAndReturns201(t *testing.T) {
	s := newTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"team_id": "T1", "channel_id": "C1", "repo_url": "https://github.com/acme/widget",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/slack/bindings", bytes.NewReader(body))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleSlackBindings(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	list, err := s.storage.ListSlackChannelBindings(req.Context(), "org_1")
	if err != nil || len(list) != 1 || list[0].ChannelID != "C1" {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

func TestHandleListSlackBindingsIsOrgScoped(t *testing.T) {
	s := newTestServer(t)
	_ = s.storage.CreateSlackChannelBinding(t.Context(), &store.SlackChannelBinding{OrgID: "org_other", TeamID: "T2", ChannelID: "C2", RepoURL: "https://github.com/acme/other"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/slack/bindings", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org_1", UserID: "user_1"}))
	w := httptest.NewRecorder()

	s.handleSlackBindings(w, req)

	var out struct {
		Bindings []map[string]any `json:"bindings"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Bindings) != 0 {
		t.Fatalf("expected no bindings visible to org_1, got %v", out.Bindings)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run 'TestHandleCreateSlackBinding|TestHandleListSlackBindings' -v`
Expected: FAIL — `handleSlackBindings` undefined.

- [ ] **Step 3: Implement**

```go
// ee/orchestrator/slack_bindings_api.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleSlackBindings serves POST and GET /api/v1/integrations/slack/bindings.
func (s *Server) handleSlackBindings(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodPost:
		var body struct {
			TeamID       string `json:"team_id"`
			ChannelID    string `json:"channel_id"`
			RepoURL      string `json:"repo_url"`
			DefaultTestCmd string `json:"default_test_cmd"`
			DefaultRef   string `json:"default_ref"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.TeamID == "" || body.ChannelID == "" || body.RepoURL == "" {
			http.Error(w, "team_id, channel_id, and repo_url are required", http.StatusBadRequest)
			return
		}
		b := &store.SlackChannelBinding{
			OrgID: claims.OrgID, TeamID: body.TeamID, ChannelID: body.ChannelID,
			RepoURL: body.RepoURL, DefaultTestCmd: body.DefaultTestCmd, DefaultRef: body.DefaultRef,
			CreatedBy: claims.UserID,
		}
		if err := s.storage.CreateSlackChannelBinding(r.Context(), b); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, b)

	case http.MethodGet:
		list, err := s.storage.ListSlackChannelBindings(r.Context(), claims.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []store.SlackChannelBinding{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"bindings": list})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDeleteSlackBinding serves DELETE /api/v1/integrations/slack/bindings/{id}.
func (s *Server) handleDeleteSlackBinding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/integrations/slack/bindings/")
	if id == "" {
		http.Error(w, "missing binding id", http.StatusBadRequest)
		return
	}
	if err := s.storage.DeleteSlackChannelBinding(r.Context(), id, claims.OrgID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Register routes in `server.go`**

```go
	mux.HandleFunc("/api/v1/integrations/slack/bindings", s.handleSlackBindings)
	mux.HandleFunc("/api/v1/integrations/slack/bindings/", s.handleDeleteSlackBinding)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run 'TestHandleCreateSlackBinding|TestHandleListSlackBindings' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add ee/orchestrator/slack_bindings_api.go ee/orchestrator/slack_bindings_api_test.go ee/orchestrator/server.go
git commit -m "feat(orchestrator): add Slack channel binding CRUD API"
```

### Task 6: Webhook receiver + first-trigger happy path (channel-bound repo only)

Scope deliberately narrow: an `@mention` in a **bound** channel becomes a task. No repo inference, no multi-message context assembly, no investigation-only branch yet — those are Tasks 7-14, layered on top of this working core.

**Files:**
- Create: `ee/orchestrator/slack_webhook.go`
- Create: `ee/orchestrator/slack_trigger.go`
- Create: `ee/orchestrator/slack_trigger_test.go`
- Modify: `ee/orchestrator/server.go` (register the webhook route)

**Interfaces:**
- Consumes: `slackapp.VerifySignature`, `slackapp.ParseEvent` (Task 2); `slackapp.Client.PostMessage`, `.AddReaction` (Task 3); `store.GetSlackInstallationByTeamID`, `store.GetSlackChannelBinding`, `store.CreateSlackTriggeredTask` (Task 1); `s.planner.SubmitPlan` (`ee/planner`, exists); `s.storage.GetCredentialPlaintext(ctx, orgID, "SLACK_BOT_TOKEN")` (exists, Task 4 populates the row).
- Produces: `POST /api/v1/webhooks/slack/events`; `(s *Server) handleSlackTrigger(ctx context.Context, teamID, channelID, threadTS, userID, text string)`, called by the webhook handler and reused by Task 8/9's richer version.

- [ ] **Step 1: Write the failing trigger test**

```go
// ee/orchestrator/slack_trigger_test.go
package orchestrator

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestHandleSlackTriggerSubmitsAPlanWhenChannelIsBound(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SaveCredential(ctx, "org_1", "SLACK_BOT_TOKEN", store.CredentialSlack, "xoxb-test")
	_ = s.storage.CreateSlackChannelBinding(ctx, &store.SlackChannelBinding{
		OrgID: "org_1", TeamID: "T1", ChannelID: "C1", RepoURL: "https://github.com/acme/widget",
	})

	s.handleSlackTrigger(ctx, "T1", "C1", "", "U1", "<@U0BOT> fix the login bug")

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one queued task, got %d", len(tasks))
	}
	if tasks[0].Spec["repo_url"] != "https://github.com/acme/widget" {
		t.Fatalf("got spec %+v", tasks[0].Spec)
	}

	var rows []store.SlackTriggeredTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&rows)
	if len(rows) != 1 || rows[0].QueuedTaskID == "" {
		t.Fatalf("expected a SlackTriggeredTask row linking the thread to the task, got %v", rows)
	}
}

func TestHandleSlackTriggerNoOpsWhenNoInstallationForTeam(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()
	// No UpsertSlackInstallation call: unknown team.
	s.handleSlackTrigger(ctx, "T-unknown", "C1", "", "U1", "fix the bug")

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Find(&tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected no task submitted for an unrecognized team, got %d", len(tasks))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestHandleSlackTrigger -v`
Expected: FAIL — `handleSlackTrigger` undefined.

- [ ] **Step 3: Implement the webhook receiver**

```go
// ee/orchestrator/slack_webhook.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"io"
	"log"
	"net/http"
	"os"

	"github.com/ibreakthecloud/kiwi/ee/slackapp"
)

// handleSlackWebhook serves POST /api/v1/webhooks/slack/events — Slack's
// Events API and interactivity deliveries share one URL in Slack's own
// console, so both are handled here and dispatched by content type, the
// same shape github_webhook.go uses to dispatch by X-GitHub-Event.
func (s *Server) handleSlackWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	secret := os.Getenv("SLACK_SIGNING_SECRET")
	if secret == "" {
		log.Println("[slackapp] SLACK_SIGNING_SECRET not set; rejecting webhook (fail closed)")
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	ts := r.Header.Get("X-Slack-Request-Timestamp")
	sig := r.Header.Get("X-Slack-Signature")
	if !slackapp.VerifySignature(secret, ts, body, sig) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Interactivity deliveries are form-encoded; Events API deliveries are
	// raw JSON. Slack sets Content-Type accordingly, so branch on that
	// rather than trying to parse both shapes against every body.
	if ct := r.Header.Get("Content-Type"); len(ct) >= len("application/x-www-form-urlencoded") &&
		ct[:len("application/x-www-form-urlencoded")] == "application/x-www-form-urlencoded" {
		s.handleSlackInteractivity(r.Context(), body)
		w.WriteHeader(http.StatusOK)
		return
	}

	ev, ok := slackapp.ParseEvent(body)
	if !ok {
		w.WriteHeader(http.StatusOK) // unrecognized shape: not an error, nothing to do
		return
	}

	if ev.Type == "url_verification" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ev.Challenge))
		return
	}

	if ev.EventType == "app_mention" {
		// Handled off the request goroutine, same posture as
		// maybeAssembleRecord in daemon_api.go: Slack expects a fast 200 and
		// retries a delivery that doesn't get one within 3 seconds.
		go s.handleSlackTrigger(r.Context(), ev.TeamID, ev.ChannelID, ev.ThreadTS, ev.UserID, ev.Text)
	}
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 4: Implement the trigger pipeline (channel-bound happy path only)**

```go
// ee/orchestrator/slack_trigger.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/ibreakthecloud/kiwi/ee/planner"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleSlackTrigger turns an @mention into a task. Every path returns
// without an error and answers nothing back to the caller beyond a log line
// — the webhook handler has already answered Slack, matching
// handleCommentTrigger's "a refusal is a log line, never a failed
// delivery" posture.
//
// Scoped narrowly for now: repo resolution only checks a channel binding.
// Task 9 layers inline-override and LLM inference in ahead of this. Context
// is only the trigger message itself; Task 8 layers thread/channel history
// assembly in before this reaches the planner.
func (s *Server) handleSlackTrigger(ctx context.Context, teamID, channelID, threadTS, userID, text string) {
	inst, err := s.storage.GetSlackInstallationByTeamID(ctx, teamID)
	if err != nil {
		return // unknown team: nothing this delivery can do
	}

	token, err := s.storage.GetCredentialPlaintext(ctx, inst.OrgID, "SLACK_BOT_TOKEN")
	if err != nil || token == "" {
		log.Printf("[slackapp] org %s has an installation but no bot token", inst.OrgID)
		return
	}

	// A trigger with no thread yet starts its own: the trigger message's own
	// ts is the thread root every reply (and every status edit) anchors to.
	if threadTS == "" {
		threadTS = "" // set below once we know the status message's ts
	}

	instruction := instructionFromSlack(text)
	if instruction == "" {
		return
	}

	binding, err := s.storage.GetSlackChannelBinding(ctx, teamID, channelID)
	if err != nil || binding == nil {
		if s.slackClient != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS,
				"This channel isn't bound to a repository yet — an admin can set one up under Integrations.")
		}
		return
	}

	if s.slackClient != nil {
		s.slackClient.AddReaction(ctx, token, channelID, firstNonEmpty(threadTS, ""), "eyes")
	}

	result, err := s.planner.SubmitPlan(ctx, planner.PlanRequest{
		OrgID:   inst.OrgID,
		UserID:  userID,
		Task:    instruction,
		RepoURL: binding.RepoURL,
		Ref:     binding.DefaultRef,
		TestCmd: binding.DefaultTestCmd, // empty is fine: pkg/daemon infers it (see infer.go)
	})
	if err != nil {
		if s.slackClient != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, fmt.Sprintf("Couldn't start that task: %s", err.Error()))
		}
		return
	}

	rootTS := threadTS
	statusTS := ""
	if s.slackClient != nil {
		statusTS, err = s.slackClient.PostMessage(ctx, token, channelID, threadTS,
			fmt.Sprintf("Working on it — job `%s`.", result.JobID))
		if err != nil {
			log.Printf("[slackapp] posting status message for job %s: %v", result.JobID, err)
		}
		if rootTS == "" {
			rootTS = statusTS // a fresh top-level trigger starts its own thread at its own status reply
		}
	}

	row := &store.SlackTriggeredTask{
		OrgID: inst.OrgID, TeamID: teamID, ChannelID: channelID, ThreadTS: rootTS,
		QueuedTaskID: firstOf(result.TaskIDs), StatusMessageTS: statusTS, LastStatus: "running",
	}
	if err := s.storage.CreateSlackTriggeredTask(ctx, row); err != nil {
		log.Printf("[slackapp] persist triggered-task row for job %s: %v", result.JobID, err)
	}
}

func firstOf(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var errNotImplemented = errors.New("not implemented")

// handleSlackInteractivity is a placeholder wired up for real in Task 11
// (continue/fork/new button clicks). Left as a named no-op rather than
// omitted so slack_webhook.go's dispatch has something to call today.
func (s *Server) handleSlackInteractivity(ctx context.Context, formBody []byte) {
	_ = ctx
	_ = formBody
}
```

`instructionFromSlack` strips the `<@U0BOT>` mention syntax Slack actually sends (not the `@runkiwi` text GitHub uses) — implement it as a small regex against `<@[A-Z0-9]+>` mirroring `instructionFrom` in `pr_comment.go`, and delete the `errNotImplemented`/`_ = ctx` scaffolding once Task 11 gives `handleSlackInteractivity` a real body.

- [ ] **Step 5: Register the webhook route in `server.go`**

Next to `root.HandleFunc("/api/v1/webhooks/github", ...)`:

```go
	root.HandleFunc("/api/v1/webhooks/slack/events", s.handleSlackWebhook)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestHandleSlackTrigger -v`
Expected: PASS

- [ ] **Step 7: Full build**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add ee/orchestrator/slack_webhook.go ee/orchestrator/slack_trigger.go ee/orchestrator/slack_trigger_test.go ee/orchestrator/server.go
git commit -m "feat(orchestrator): Slack @mention in a bound channel submits a task"
```

---

## Phase E — Status lifecycle

### Task 7: Edit the status message on task completion

**Files:**
- Modify: `ee/orchestrator/daemon_api.go` (`handleDaemonResult`)
- Create: `ee/orchestrator/slack_completion.go`
- Create: `ee/orchestrator/slack_completion_test.go`

**Interfaces:**
- Consumes: `store.LatestSlackTriggeredTask` is not the right lookup here (that's for thread-reply classification, Task 11) — this needs "the `SlackTriggeredTask` row whose `QueuedTaskID` is this task." Add `store.GetSlackTriggeredTaskByQueuedTaskID(ctx, taskID string) (*SlackTriggeredTask, error)` to Task 1's store file (small addition — call it out when revisiting Task 1, or add here; either is fine since it's additive and doesn't change Task 1's already-committed shape).
- Produces: `(s *Server) reportSlackCompletion(ctx context.Context, taskID string, task *store.QueuedTask)`.

- [ ] **Step 1: Add the missing store lookup (small addition to Task 1's file)**

```go
// pkg/store/slack.go — append
func (s *PostgresStore) GetSlackTriggeredTaskByQueuedTaskID(ctx context.Context, taskID string) (*SlackTriggeredTask, error) {
	var t SlackTriggeredTask
	err := s.db.WithContext(ctx).Where("queued_task_id = ?", taskID).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
```

Add `GetSlackTriggeredTaskByQueuedTaskID(ctx context.Context, taskID string) (*SlackTriggeredTask, error)` to the `Store` interface in `pkg/store/store.go`, next to the other Slack methods added in Task 1.

- [ ] **Step 2: Write the failing test**

```go
// ee/orchestrator/slack_completion_test.go
package orchestrator

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestReportSlackCompletionEditsStatusMessageOnSuccess(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SaveCredential(ctx, "org_1", "SLACK_BOT_TOKEN", store.CredentialSlack, "xoxb-test")
	_ = s.storage.CreateSlackTriggeredTask(ctx, &store.SlackTriggeredTask{
		OrgID: "org_1", TeamID: "T1", ChannelID: "C1", ThreadTS: "100.001",
		QueuedTaskID: "task_1", StatusMessageTS: "100.002", LastStatus: "running",
	})

	edited := fakeSlackEdits(t, s) // test helper: swaps s.slackClient for a fake that records EditMessage/AddReaction calls

	prURL := "https://github.com/acme/widget/pull/9"
	task := &store.QueuedTask{ID: "task_1", OrgID: "org_1", Status: store.TaskSucceeded, ResultURL: &prURL}
	s.reportSlackCompletion(ctx, "task_1", task)

	if len(edited.messages) != 1 || edited.messages[0] != "100.002" {
		t.Fatalf("expected exactly one edit to ts 100.002, got %v", edited.messages)
	}

	var row store.SlackTriggeredTask
	s.db.WithContext(ctx).Where("queued_task_id = ?", "task_1").First(&row)
	if row.LastStatus != "succeeded" {
		t.Fatalf("expected last_status updated to succeeded, got %q", row.LastStatus)
	}
}

func TestReportSlackCompletionNoOpsForATaskWithNoSlackOrigin(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()
	task := &store.QueuedTask{ID: "task_not_slack", OrgID: "org_1", Status: store.TaskSucceeded}
	s.reportSlackCompletion(ctx, "task_not_slack", task) // must not panic or error
}
```

(`fakeSlackEdits` — write a small in-package fake implementing the subset of `slackapp.Client` methods this file calls, swapped onto `s.slackClient` for the test; follow whatever fake-swapping convention `ee/orchestrator`'s existing daemon tests use for `s.githubApp`, if one exists, rather than inventing a new style.)

- [ ] **Step 3: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestReportSlackCompletion -v`
Expected: FAIL — `reportSlackCompletion` undefined.

- [ ] **Step 4: Implement**

```go
// ee/orchestrator/slack_completion.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// reportSlackCompletion edits a Slack-triggered task's status message to its
// terminal state. Called from handleDaemonResult for EVERY task regardless
// of origin — it is a no-op (not an error) for a task that didn't come from
// Slack, which is the common case and must stay cheap and silent.
func (s *Server) reportSlackCompletion(ctx context.Context, taskID string, task *store.QueuedTask) {
	row, err := s.storage.GetSlackTriggeredTaskByQueuedTaskID(ctx, taskID)
	if err != nil || row == nil {
		return
	}
	if s.slackClient == nil {
		return
	}

	token, err := s.storage.GetCredentialPlaintext(ctx, row.OrgID, "SLACK_BOT_TOKEN")
	if err != nil || token == "" {
		return
	}

	// ResultURL and ResultDetail are *string on store.QueuedTask (nil means
	// "never set", same convention jobs_api.go and ver_hook.go already read
	// them with) — resolve both to plain strings once, up front.
	var resultURL, resultDetail string
	if task.ResultURL != nil {
		resultURL = *task.ResultURL
	}
	if task.ResultDetail != nil {
		resultDetail = *task.ResultDetail
	}

	var text, status string
	switch {
	case task.Status == store.TaskSucceeded && resultURL != "":
		text = fmt.Sprintf(":white_check_mark: Done — %s", resultURL)
		status = "succeeded"
	case task.Status == store.TaskSucceeded: // investigation-only completion (Task 12): no PR, findings in ResultDetail
		text = fmt.Sprintf(":white_check_mark: %s", truncateForSlack(resultDetail))
		status = "succeeded"
	default:
		text = fmt.Sprintf(":x: %s", truncateForSlack(resultDetail))
		status = "failed"
	}

	if row.StatusMessageTS != "" {
		if err := s.slackClient.EditMessage(ctx, token, row.ChannelID, row.StatusMessageTS, text); err != nil {
			log.Printf("[slackapp] editing status message for task %s: %v", taskID, err)
		}
	}
	if err := s.storage.UpdateSlackTriggeredTaskStatus(ctx, row.ID, status, ""); err != nil {
		log.Printf("[slackapp] updating status row for task %s: %v", taskID, err)
	}
}

// truncateForSlack keeps a long investigation report or failure detail from
// blowing past Slack's per-message size limit, pointing at the dashboard
// task page for the rest — the spec's "view full report" behavior.
func truncateForSlack(detail string) string {
	const max = 2000
	if len(detail) <= max {
		return detail
	}
	return detail[:max] + "… (truncated — see the full report on the Kiwi dashboard)"
}
```

- [ ] **Step 5: Wire the call into `handleDaemonResult`**

In `ee/orchestrator/daemon_api.go`, right after the existing block:

```go
	var task store.QueuedTask
	if err := s.db.WithContext(r.Context()).Where("id = ?", req.TaskID).First(&task).Error; err == nil {
		s.meterKiwiUsage(r.Context(), &task, tokensIn, tokensOut, architectIn, architectOut)
	}
```

add:

```go
	if err := s.db.WithContext(r.Context()).Where("id = ?", req.TaskID).First(&task).Error; err == nil {
		s.reportSlackCompletion(r.Context(), req.TaskID, &task)
	}
```

(Re-querying `task` a second time is deliberate rather than reusing the block above verbatim: keep this addition as a single self-contained diff hunk immediately after the existing query, so a future reader can see the whole "what happens after CompleteTask" sequence in one place without needing to also trust that an unrelated earlier `err == nil` still holds this far down. If the existing block's `task` variable is still in scope and unmodified, reuse it directly instead of re-querying — check at implementation time.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestReportSlackCompletion -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add pkg/store/slack.go pkg/store/store.go ee/orchestrator/slack_completion.go ee/orchestrator/slack_completion_test.go ee/orchestrator/daemon_api.go
git commit -m "feat(orchestrator): edit the Slack status message when a triggered task completes"
```

---

## Phase F — Context assembly + repo inference

### Task 8: Context assembly (lookback + LLM sufficiency escalation)

**Files:**
- Create: `ee/orchestrator/slack_context.go`
- Create: `ee/orchestrator/slack_context_test.go`

**Interfaces:**
- Consumes: `slackapp.Client.ConversationHistory`, `.ConversationReplies` (Task 3); a `provider.Completer` (see Task 9's shared `slackCompleter()` helper — introduce it here since this task needs it first, Task 9 reuses it).
- Produces: `(s *Server) assembleSlackContext(ctx context.Context, token, channelID, threadTS, triggerText string) (string, error)` — returns the composed task description.

- [ ] **Step 1: Write the failing tests (against a fake `provider.Completer` and a fake message-history function, no network)**

```go
// ee/orchestrator/slack_context_test.go
package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestAssembleSlackContextUsesFixedLookbackWhenSufficient(t *testing.T) {
	history := []string{"U1: the login page 500s on bad passwords", "U2: seeing it in prod too"}
	complete := func(ctx context.Context, system, user string) (string, error) {
		if strings.Contains(user, "500s on bad passwords") {
			return `{"sufficient": true}`, nil
		}
		t.Fatalf("unexpected prompt: %s", user)
		return "", nil
	}
	got, err := assembleContext(context.Background(), complete, history, "fix this bug")
	if err != nil {
		t.Fatalf("assembleContext: %v", err)
	}
	if !strings.Contains(got, "500s on bad passwords") || !strings.Contains(got, "fix this bug") {
		t.Fatalf("got %q", got)
	}
}

func TestAssembleSlackContextEscalatesWhenInsufficient(t *testing.T) {
	calls := 0
	complete := func(ctx context.Context, system, user string) (string, error) {
		calls++
		if calls == 1 {
			return `{"sufficient": false}`, nil
		}
		return `{"sufficient": true}`, nil
	}
	history := []string{"U1: something's wrong"}
	escalated := []string{"U1: something's wrong", "U2: it's the login flow, 500 on bad password"}
	got, err := assembleContextEscalating(context.Background(), complete, history, escalated, "fix this")
	if err != nil {
		t.Fatalf("assembleContextEscalating: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected exactly one escalation call, got %d total calls", calls)
	}
	if !strings.Contains(got, "500 on bad password") {
		t.Fatalf("expected the escalated history in the result, got %q", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestAssembleSlackContext -v`
Expected: FAIL — `assembleContext`/`assembleContextEscalating` undefined.

- [ ] **Step 3: Implement**

```go
// ee/orchestrator/slack_context.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// fixedLookback is how many prior channel messages a fresh (non-thread)
// trigger pulls before asking whether that's enough. Tuning knob, not a
// contract — widen based on real usage rather than a guess made here.
const fixedLookback = 10

// escalatedLookback is how far back a second attempt goes when the first
// window was judged insufficient. Also a tuning knob.
const escalatedLookback = 50

type completeFunc func(ctx context.Context, system, user string) (string, error)

// slackCompleter builds a cheap Control-Plane-side LLM call, the same way
// SubmitPlan resolves a Kiwi-funded model's key via provider.PlatformKeyFor
// — these calls (context sufficiency, repo inference, thread-reply
// classification) all run on the Control Plane, not in a customer's daemon,
// so they need their own key rather than the org's.
func (s *Server) slackCompleter() (completeFunc, error) {
	model := os.Getenv("KIWI_SLACK_INFERENCE_MODEL")
	if model == "" {
		model = "gemini-flash-latest"
	}
	key, ok := provider.PlatformKeyFor("gemini")
	if !ok || key == "" {
		return nil, fmt.Errorf("no platform key configured for Slack inference")
	}
	p := provider.NewGeminiProviderWithModels(key, model, model)
	return p.Complete, nil
}

// assembleContext judges one window of history against the sufficiency
// question and returns the composed task description when the LLM says
// it's enough. No I/O: history is already fetched, complete is already
// bound — this is what makes the sufficiency logic itself table-testable.
func assembleContext(ctx context.Context, complete completeFunc, history []string, triggerText string) (string, error) {
	sufficient, err := isContextSufficient(ctx, complete, history, triggerText)
	if err != nil {
		return "", err
	}
	if !sufficient {
		return "", errInsufficientContext
	}
	return composeTaskDescription(history, triggerText), nil
}

var errInsufficientContext = fmt.Errorf("insufficient context in the fixed lookback window")

// assembleContextEscalating tries the fixed window first, and only pulls the
// wider escalated window when the first judged itself insufficient — the
// escalation is one extra call, not a habit, so the common case (already
// discussed in-thread) pays for exactly one LLM round trip.
func assembleContextEscalating(ctx context.Context, complete completeFunc, history, escalatedHistory []string, triggerText string) (string, error) {
	got, err := assembleContext(ctx, complete, history, triggerText)
	if err == nil {
		return got, nil
	}
	if err != errInsufficientContext {
		return "", err
	}
	return assembleContext(ctx, complete, escalatedHistory, triggerText)
}

func isContextSufficient(ctx context.Context, complete completeFunc, history []string, triggerText string) (bool, error) {
	system := "You judge whether a Slack conversation gives enough context to act on an instruction. " +
		"Respond with ONLY a JSON object: {\"sufficient\": true|false}."
	user := fmt.Sprintf("Instruction: %s\n\nConversation so far:\n%s", triggerText, strings.Join(history, "\n"))
	resp, err := complete(ctx, system, user)
	if err != nil {
		return false, err
	}
	var out struct {
		Sufficient bool `json:"sufficient"`
	}
	start, end := strings.IndexByte(resp, '{'), strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return false, fmt.Errorf("no JSON object in sufficiency response")
	}
	if err := json.Unmarshal([]byte(resp[start:end+1]), &out); err != nil {
		return false, fmt.Errorf("parse sufficiency response: %w", err)
	}
	return out.Sufficient, nil
}

func composeTaskDescription(history []string, triggerText string) string {
	if len(history) == 0 {
		return triggerText
	}
	return fmt.Sprintf("Context from the conversation:\n%s\n\nInstruction: %s", strings.Join(history, "\n"), triggerText)
}

// fetchSlackContext gets the fixed-lookback window (whole thread if
// threadTS is set, else the last fixedLookback channel messages) and, when
// judged insufficient, the escalated one — the I/O half assembleContext and
// assembleContextEscalating deliberately have none of.
func (s *Server) fetchSlackContext(ctx context.Context, token, channelID, threadTS, triggerText string) (string, error) {
	complete, err := s.slackCompleter()
	if err != nil {
		log.Printf("[slackapp] no completer available, falling back to trigger text only: %v", err)
		return triggerText, nil
	}

	fetch := func(limit int) ([]string, error) {
		var msgs []string
		if threadTS != "" {
			hist, err := s.slackClient.ConversationReplies(ctx, token, channelID, threadTS)
			if err != nil {
				return nil, err
			}
			for _, m := range hist {
				msgs = append(msgs, m.UserID+": "+m.Text)
			}
			return msgs, nil
		}
		hist, err := s.slackClient.ConversationHistory(ctx, token, channelID, limit)
		if err != nil {
			return nil, err
		}
		for _, m := range hist {
			msgs = append(msgs, m.UserID+": "+m.Text)
		}
		return msgs, nil
	}

	history, err := fetch(fixedLookback)
	if err != nil {
		return triggerText, nil // best effort: fall back to the bare trigger rather than fail the task
	}

	got, err := assembleContext(ctx, complete, history, triggerText)
	if err == nil {
		return got, nil
	}
	if err != errInsufficientContext {
		return triggerText, nil
	}

	escalated, err := fetch(escalatedLookback)
	if err != nil {
		return composeTaskDescription(history, triggerText), nil
	}
	got, err = assembleContext(ctx, complete, escalated, triggerText)
	if err != nil {
		// Still insufficient after escalating: use what we have rather than
		// refuse the task outright — the spec's fallback is asking the user
		// to clarify, which Task 9 wires as a reply when this returns "".
		return composeTaskDescription(escalated, triggerText), nil
	}
	return got, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestAssembleSlackContext -v`
Expected: PASS

- [ ] **Step 5: Wire `fetchSlackContext` into `handleSlackTrigger` (replaces the bare `instruction` used since Task 6)**

In `ee/orchestrator/slack_trigger.go`, replace:

```go
	instruction := instructionFromSlack(text)
	if instruction == "" {
		return
	}
```

with:

```go
	rawInstruction := instructionFromSlack(text)
	if rawInstruction == "" {
		return
	}
	instruction, err := s.fetchSlackContext(ctx, token, channelID, threadTS, rawInstruction)
	if err != nil {
		instruction = rawInstruction
	}
```

- [ ] **Step 6: Run the full trigger test suite to confirm no regression**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run 'TestHandleSlackTrigger|TestAssembleSlackContext' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add ee/orchestrator/slack_context.go ee/orchestrator/slack_context_test.go ee/orchestrator/slack_trigger.go
git commit -m "feat(orchestrator): assemble Slack thread/channel context before submitting a task"
```

### Task 9: Repo resolution — inline override, LLM inference, disambiguation

**Files:**
- Create: `ee/orchestrator/slack_repo.go`
- Create: `ee/orchestrator/slack_repo_test.go`
- Modify: `ee/orchestrator/slack_trigger.go`

**Interfaces:**
- Consumes: `s.slackCompleter()` (Task 8); `s.githubApp.ListRepositories` (exists, `ee/githubapp`); `s.storage.ListGitHubInstallations` (exists); `s.storage.GetSlackChannelBinding` (Task 1).
- Produces: `(s *Server) resolveSlackRepo(ctx context.Context, orgID, text string, binding *store.SlackChannelBinding) (repoURL string, ambiguousReply string)` — `ambiguousReply` non-empty means stop and reply instead of submitting.

- [ ] **Step 1: Write the failing tests for the pure priority-order function**

```go
// ee/orchestrator/slack_repo_test.go
package orchestrator

import (
	"context"
	"testing"
)

func TestInlineRepoOverrideWinsRegardlessOfBinding(t *testing.T) {
	got, ok := inlineRepoOverride("fix the bug in repo:acme/widget please")
	if !ok || got != "acme/widget" {
		t.Fatalf("got %q, ok=%v", got, ok)
	}
}

func TestInlineRepoOverrideAbsentReturnsFalse(t *testing.T) {
	if _, ok := inlineRepoOverride("fix the login bug"); ok {
		t.Fatal("expected no override to be found")
	}
}

func TestInferRepoPicksTheClearWinner(t *testing.T) {
	repos := []string{"auth-service", "billing-service", "docs-site"}
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"repo": "auth-service", "confidence": "high"}`, nil
	}
	got, ambiguous, err := inferRepo(context.Background(), complete, repos, "fix the login bug")
	if err != nil {
		t.Fatalf("inferRepo: %v", err)
	}
	if ambiguous || got != "auth-service" {
		t.Fatalf("got %q ambiguous=%v", got, ambiguous)
	}
}

func TestInferRepoReportsAmbiguousOnLowConfidence(t *testing.T) {
	repos := []string{"api-v1", "api-v2"}
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"repo": "", "confidence": "low", "candidates": ["api-v1", "api-v2"]}`, nil
	}
	_, ambiguous, err := inferRepo(context.Background(), complete, repos, "fix the bug")
	if err != nil {
		t.Fatalf("inferRepo: %v", err)
	}
	if !ambiguous {
		t.Fatal("expected ambiguous=true on low confidence")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run 'TestInlineRepoOverride|TestInferRepo' -v`
Expected: FAIL — `inlineRepoOverride`, `inferRepo` undefined.

- [ ] **Step 3: Implement**

```go
// ee/orchestrator/slack_repo.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// inlineOverrideRe matches "repo:owner/name" anywhere in the message —
// the explicit-override syntax, chosen for being unambiguous to both grep
// and a human skimming the message (unlike a bare "owner/name" token, which
// collides with normal English like "click and/or").
var inlineOverrideRe = regexp.MustCompile(`repo:([\w.-]+/[\w.-]+)`)

// inlineRepoOverride extracts an explicit repo:owner/name token, the
// highest-priority repo signal (spec §5, priority 1).
func inlineRepoOverride(text string) (string, bool) {
	m := inlineOverrideRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// inferRepo asks the LLM to pick the most likely target repo from the org's
// GitHub-installed repos, or report ambiguity rather than guess. Confidence
// threshold is deliberately conservative (only "high" auto-picks) — a
// tuning knob per the spec, widen based on real usage.
func inferRepo(ctx context.Context, complete completeFunc, repoNames []string, instruction string) (repo string, ambiguous bool, err error) {
	system := "You pick which repository a task refers to, from a fixed list. " +
		`Respond with ONLY JSON: {"repo": "<name or empty>", "confidence": "high|medium|low", "candidates": ["..."]}. ` +
		`Use "high" only when one repo is clearly the right target.`
	user := fmt.Sprintf("Candidate repositories:\n%s\n\nInstruction: %s", strings.Join(repoNames, "\n"), instruction)
	resp, cerr := complete(ctx, system, user)
	if cerr != nil {
		return "", false, cerr
	}
	start, end := strings.IndexByte(resp, '{'), strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return "", false, fmt.Errorf("no JSON object in repo-inference response")
	}
	var out struct {
		Repo       string `json:"repo"`
		Confidence string `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(resp[start:end+1]), &out); err != nil {
		return "", false, fmt.Errorf("parse repo-inference response: %w", err)
	}
	if out.Confidence != "high" || out.Repo == "" {
		return "", true, nil
	}
	return out.Repo, false, nil
}

// resolveSlackRepo runs the full priority order from the spec: inline
// override, then channel binding, then LLM inference, then disambiguation.
// A non-empty ambiguousReply means the caller must reply with it and NOT
// submit a task.
func (s *Server) resolveSlackRepo(ctx context.Context, orgID, text string, binding *store.SlackChannelBinding) (repoURL, ambiguousReply string) {
	if override, ok := inlineRepoOverride(text); ok {
		return "https://github.com/" + override, ""
	}
	if binding != nil {
		return binding.RepoURL, ""
	}

	installs, err := s.storage.ListGitHubInstallations(ctx, orgID)
	if err != nil || len(installs) == 0 || s.githubApp == nil {
		return "", "This channel isn't bound to a repository, and this org has no GitHub connection to infer one from — connect GitHub or bind this channel under Integrations."
	}

	var names []string
	nameToURL := map[string]string{}
	for _, inst := range installs {
		repos, err := s.githubApp.ListRepositories(ctx, inst.InstallationID)
		if err != nil {
			continue
		}
		for _, r := range repos {
			names = append(names, r.FullName)
			nameToURL[r.FullName] = r.HTMLURL
		}
	}
	if len(names) == 0 {
		return "", "Couldn't find any repositories to infer from — bind this channel to a repository under Integrations."
	}

	complete, cerr := s.slackCompleter()
	if cerr != nil {
		return "", "Couldn't determine which repository this is about — bind this channel to a repository under Integrations."
	}
	picked, ambiguous, err := inferRepo(ctx, complete, names, text)
	if err != nil || ambiguous || picked == "" {
		return "", fmt.Sprintf("Not sure which repository this is about — try mentioning it explicitly, e.g. `repo:%s`, or bind this channel under Integrations.", firstOf(names))
	}
	return nameToURL[picked], ""
}
```

- [ ] **Step 4: Run to verify the new tests pass**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run 'TestInlineRepoOverride|TestInferRepo' -v`
Expected: PASS

- [ ] **Step 5: Wire `resolveSlackRepo` into `handleSlackTrigger`, replacing the binding-only check from Task 6**

In `ee/orchestrator/slack_trigger.go`, replace:

```go
	binding, err := s.storage.GetSlackChannelBinding(ctx, teamID, channelID)
	if err != nil || binding == nil {
		if s.slackClient != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS,
				"This channel isn't bound to a repository yet — an admin can set one up under Integrations.")
		}
		return
	}
```

with:

```go
	binding, _ := s.storage.GetSlackChannelBinding(ctx, teamID, channelID) // nil is fine: resolveSlackRepo falls through
	repoURL, ambiguousReply := s.resolveSlackRepo(ctx, inst.OrgID, text, binding)
	if ambiguousReply != "" {
		if s.slackClient != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, ambiguousReply)
		}
		return
	}
```

and update the `SubmitPlan` call's `RepoURL: binding.RepoURL` to `RepoURL: repoURL`, and `Ref`/`TestCmd` to read from `binding` only when `binding != nil` (guard the field access — `binding` may now be `nil`).

- [ ] **Step 6: Run the full trigger suite**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestHandleSlackTrigger -v`
Expected: PASS (update the Task 6 test's binding-required assumption if it now needs a `binding: nil` + GitHub-installation-backed variant to stay accurate — add a new case rather than deleting the original).

- [ ] **Step 7: Commit**

```bash
git add ee/orchestrator/slack_repo.go ee/orchestrator/slack_repo_test.go ee/orchestrator/slack_trigger.go
git commit -m "feat(orchestrator): resolve Slack trigger repo via inline override, binding, or inference"
```

---

## Phase G — Thread replies: continue / fork / new

### Task 10: `SubmitFork` in `ee/planner`

**Files:**
- Modify: `ee/planner/service.go`
- Create: `ee/planner/fork_test.go`

**Interfaces:**
- Consumes: `store.OriginFork` (already declared, `pkg/store/lineage.go:26`, currently unused — this task is its first real caller); `jobBranchName`-equivalent knowledge (`"kiwi/" + JobID`, currently only known inside `pkg/daemon`; duplicate the one-line format here rather than importing `pkg/daemon` from `ee/planner`, since that import would be backwards — `ee/planner` is a dependency of `pkg/daemon`'s callers, not the reverse).
- Produces: `(s *Service) SubmitFork(ctx context.Context, in ForkInput) (*SubmitResult, error)` where `ForkInput{OrgID, UserID string, ParentTask *store.QueuedTask, Instruction string}`.

- [ ] **Step 1: Write the failing test**

`ee/planner/planner_test.go`'s `TestServiceSubmitPlanPersistsAndEnqueues` is the real harness to mirror: `newTestStore(t)` + `seedAdmissibleOrg(t, s, orgID)` + `NewService(s, NewHeuristicPlanner(), nil)`, then read results back via `s.DB()`/`s.LeaseNextTask`.

```go
// ee/planner/fork_test.go
package planner

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestSubmitForkStartsFromTheParentsBranchWithANewJobID(t *testing.T) {
	s := newTestStore(t)
	seedAdmissibleOrg(t, s, "o1")
	svc := NewService(s, NewHeuristicPlanner(), nil)
	ctx := context.Background()

	// The parent as it would exist after an ordinary SubmitPlan: a real
	// QueuedTask row with a job id and a repo_url in its spec.
	parentID := "job_parent-w0"
	parent := &store.QueuedTask{
		ID: parentID, OrgID: "o1", JobID: "job_parent", Origin: store.OriginSubmit, RootTaskID: parentID,
		Spec: map[string]interface{}{"repo_url": "https://github.com/x/y"},
	}
	if err := s.DB().Create(parent).Error; err != nil {
		t.Fatalf("seed parent task: %v", err)
	}

	result, err := svc.SubmitFork(ctx, ForkInput{
		OrgID: "o1", UserID: "u1", ParentTask: parent, Instruction: "try the alternate approach instead",
	})
	if err != nil {
		t.Fatalf("SubmitFork: %v", err)
	}
	if result.JobID == parent.JobID {
		t.Fatal("expected a fork to get its own job id, not reuse the parent's")
	}

	var tasks []store.QueuedTask
	if err := s.DB().Where("job_id = ?", result.JobID).Find(&tasks).Error; err != nil || len(tasks) == 0 {
		t.Fatalf("expected at least one task enqueued under the new job id, got %v err=%v", tasks, err)
	}
	if tasks[0].Origin != store.OriginFork {
		t.Fatalf("expected Origin=fork, got %q", tasks[0].Origin)
	}
	if tasks[0].ParentTaskID == nil || *tasks[0].ParentTaskID != parent.ID {
		t.Fatal("expected ParentTaskID to point back at the source task")
	}
	if tasks[0].RootTaskID != tasks[0].ID {
		t.Fatal("expected a fork to start its own thread (RootTaskID == its own ID), not extend the parent's")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/planner/... -run TestSubmitFork -v`
Expected: FAIL — `SubmitFork`, `ForkInput` undefined.

- [ ] **Step 3: Implement**

```go
// ee/planner/service.go — append

// ForkInput describes a request to start a new, independent task from an
// existing task's current branch — a sibling line of work, not a
// continuation of the same one. Where SubmitContinuation deliberately keeps
// the parent's job id (so the existing pull request updates in place), a
// fork deliberately does NOT: it gets its own job id, its own branch, and
// its own pull request, starting from wherever the parent's branch
// currently stands.
type ForkInput struct {
	OrgID       string
	UserID      string
	ParentTask  *store.QueuedTask
	Instruction string
}

// SubmitFork is a thin wrapper over SubmitPlan: everything about admission,
// entitlement, and manifest creation a fresh submit needs, a fork needs too
// — the only difference is where Ref points. Pointing it at
// "kiwi/"+ParentTask.JobID (the parent's own job branch, per jobBranchName
// in pkg/daemon/delivery.go) is what makes the daemon's ordinary clone-and-
// checkout start from the parent's work instead of from the repository's
// default branch.
func (s *Service) SubmitFork(ctx context.Context, in ForkInput) (*SubmitResult, error) {
	if in.OrgID == "" {
		return nil, fmt.Errorf("org id is required")
	}
	if in.ParentTask == nil {
		return nil, fmt.Errorf("a fork needs the task it forks from")
	}
	if in.Instruction == "" {
		return nil, fmt.Errorf("a fork needs an instruction")
	}

	repoURL, _ := in.ParentTask.Spec["repo_url"].(string)
	model, _ := in.ParentTask.Spec["model"].(string)
	testCmd, _ := in.ParentTask.Spec["test_cmd"].(string)

	result, err := s.SubmitPlan(ctx, PlanRequest{
		OrgID:   in.OrgID,
		UserID:  in.UserID,
		Task:    in.Instruction,
		RepoURL: repoURL,
		Ref:     "kiwi/" + in.ParentTask.JobID,
		Model:   model,
		TestCmd: testCmd,
	})
	if err != nil {
		return nil, err
	}

	// SubmitPlan has no notion of lineage — it always writes OriginSubmit and
	// a fresh root. Overwrite that on the row(s) it just created, in the same
	// spirit as buildContinuationTask setting Origin/ParentTaskID explicitly:
	// a fork's tasks need to say where they came from without SubmitPlan's
	// ordinary path needing to know forks exist at all.
	parentID := in.ParentTask.ID
	if err := s.store.DB().WithContext(ctx).Model(&store.QueuedTask{}).
		Where("job_id = ?", result.JobID).
		Updates(map[string]interface{}{"origin": store.OriginFork, "parent_task_id": parentID}).Error; err != nil {
		return nil, fmt.Errorf("label fork lineage for job %s: %w", result.JobID, err)
	}

	return result, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/planner/... -run TestSubmitFork -v`
Expected: PASS

- [ ] **Step 5: Full build**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./pkg/... ./ee/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add ee/planner/service.go ee/planner/fork_test.go
git commit -m "feat(planner): add SubmitFork — start a new task from an existing task's branch"
```

### Task 11: Thread-reply classification (continue / fork / new / ambiguous)

**Files:**
- Create: `ee/orchestrator/slack_thread_reply.go`
- Create: `ee/orchestrator/slack_thread_reply_test.go`
- Modify: `ee/orchestrator/slack_trigger.go` (branch to this path when a `SlackTriggeredTask` already exists for the thread)
- Modify: `ee/orchestrator/slack_webhook.go` (`handleSlackInteractivity`, replacing Task 6's placeholder)

**Interfaces:**
- Consumes: `s.storage.LatestSlackTriggeredTask` (Task 1); `s.planner.SubmitContinuation` (exists); `s.planner.SubmitFork` (Task 10); `slackapp.Client.PostInteractiveButtons` (Task 3); `slackapp.ParseInteractivity` (Task 2).
- Produces: `(s *Server) classifyThreadReply(ctx context.Context, complete completeFunc, latestSummary, newMessage string) (verdict string, err error)` where verdict ∈ `continue|fork|new|ambiguous`; `(s *Server) handleSlackThreadReply(ctx context.Context, teamID, channelID, threadTS, userID, text string, existing *store.SlackTriggeredTask)`.

- [ ] **Step 1: Write the failing classifier test**

```go
// ee/orchestrator/slack_thread_reply_test.go
package orchestrator

import (
	"context"
	"testing"
)

func TestClassifyThreadReplyContinue(t *testing.T) {
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"verdict": "continue"}`, nil
	}
	got, err := classifyThreadReply(context.Background(), complete, "PR #9 fixes the null check", "also handle the empty-string case")
	if err != nil || got != "continue" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestClassifyThreadReplyRejectsUnknownVerdict(t *testing.T) {
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"verdict": "something-else"}`, nil
	}
	if _, err := classifyThreadReply(context.Background(), complete, "summary", "message"); err == nil {
		t.Fatal("expected an error for an unrecognized verdict")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestClassifyThreadReply -v`
Expected: FAIL — `classifyThreadReply` undefined.

- [ ] **Step 3: Implement**

```go
// ee/orchestrator/slack_thread_reply.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/planner"
	"github.com/ibreakthecloud/kiwi/ee/slackapp"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

const (
	verdictContinue  = "continue"
	verdictFork      = "fork"
	verdictNew       = "new"
	verdictAmbiguous = "ambiguous"
)

// classifyThreadReply asks the Architect-equivalent classifier whether a
// reply in an already-actioned thread continues that work, forks off it,
// starts something unrelated, or is genuinely unclear. No I/O beyond the
// completion call, so the decision logic is table-testable on its own.
func classifyThreadReply(ctx context.Context, complete completeFunc, latestSummary, newMessage string) (string, error) {
	system := "You classify a follow-up message in a thread where Kiwi already produced work. " +
		`Respond with ONLY JSON: {"verdict": "continue|fork|new|ambiguous"}. ` +
		`"continue" if the message refines or extends the same fix. "fork" if it wants a different approach starting from the same work. ` +
		`"new" if it's unrelated. "ambiguous" if you genuinely cannot tell.`
	user := fmt.Sprintf("What Kiwi already did:\n%s\n\nNew message:\n%s", latestSummary, newMessage)
	resp, err := complete(ctx, system, user)
	if err != nil {
		return "", err
	}
	start, end := strings.IndexByte(resp, '{'), strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return "", fmt.Errorf("no JSON object in classification response")
	}
	var out struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(resp[start:end+1]), &out); err != nil {
		return "", fmt.Errorf("parse classification response: %w", err)
	}
	switch out.Verdict {
	case verdictContinue, verdictFork, verdictNew, verdictAmbiguous:
		return out.Verdict, nil
	default:
		return "", fmt.Errorf("unrecognized verdict %q", out.Verdict)
	}
}

// summaryForClassification is what the classifier sees as "what Kiwi
// already did" — the parent task's own reported detail (its PR body or
// investigation summary), which is already the most compact accurate
// account of that work that exists anywhere.
func summaryForClassification(parent *store.QueuedTask) string {
	if parent.ResultDetail != "" {
		return parent.ResultDetail
	}
	return parent.Spec["task"].(string)
}

// handleSlackThreadReply is handleSlackTrigger's counterpart for a reply in
// a thread that already has a task: classify, then continue / fork / new /
// ask.
func (s *Server) handleSlackThreadReply(ctx context.Context, teamID, channelID, threadTS, userID, text string, existing *store.SlackTriggeredTask) {
	inst, err := s.storage.GetSlackInstallationByTeamID(ctx, teamID)
	if err != nil {
		return
	}
	token, err := s.storage.GetCredentialPlaintext(ctx, inst.OrgID, "SLACK_BOT_TOKEN")
	if err != nil || token == "" || s.slackClient == nil {
		return
	}

	instruction := instructionFromSlack(text)
	if instruction == "" {
		return
	}

	var parent store.QueuedTask
	if err := s.db.WithContext(ctx).Where("id = ?", existing.QueuedTaskID).First(&parent).Error; err != nil {
		return
	}

	complete, cerr := s.slackCompleter()
	if cerr != nil {
		return
	}
	verdict, err := classifyThreadReply(ctx, complete, summaryForClassification(&parent), instruction)
	if err != nil {
		verdict = verdictAmbiguous
	}

	switch verdict {
	case verdictContinue:
		sessionID := ""
		if sess, serr := s.storage.GetAgentSessionByTask(ctx, inst.OrgID, parent.ID); serr == nil && sess != nil {
			sessionID = sess.ID
		}
		task, err := s.planner.SubmitContinuation(ctx, planner.ContinuationInput{
			OrgID: inst.OrgID, ParentTask: &parent, Instruction: instruction, SessionID: sessionID, Origin: store.OriginPRComment,
		})
		if err != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, fmt.Sprintf("Couldn't continue that task: %s", err.Error()))
			return
		}
		s.recordSlackThreadTask(ctx, inst.OrgID, teamID, channelID, threadTS, task.ID, token, "Continuing…")

	case verdictFork:
		result, err := s.planner.SubmitFork(ctx, planner.ForkInput{OrgID: inst.OrgID, UserID: userID, ParentTask: &parent, Instruction: instruction})
		if err != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, fmt.Sprintf("Couldn't fork that task: %s", err.Error()))
			return
		}
		s.recordSlackThreadTask(ctx, inst.OrgID, teamID, channelID, threadTS, firstOf(result.TaskIDs), token, fmt.Sprintf("Forking into a new attempt — job `%s`.", result.JobID))

	case verdictNew:
		repoURL, _ := parent.Spec["repo_url"].(string)
		result, err := s.planner.SubmitPlan(ctx, planner.PlanRequest{OrgID: inst.OrgID, UserID: userID, Task: instruction, RepoURL: repoURL})
		if err != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, fmt.Sprintf("Couldn't start that task: %s", err.Error()))
			return
		}
		s.recordSlackThreadTask(ctx, inst.OrgID, teamID, channelID, threadTS, firstOf(result.TaskIDs), token, fmt.Sprintf("Starting a new, unrelated task — job `%s`.", result.JobID))

	default: // ambiguous
		s.slackClient.PostInteractiveButtons(ctx, token, channelID, threadTS,
			"Not sure whether that's a continuation, a different approach, or something new — which did you mean?",
			[]slackapp.Button{
				{Label: "Continue", ActionID: "slack_thread_continue", Value: existing.ID + "|" + instruction},
				{Label: "Fork", ActionID: "slack_thread_fork", Value: existing.ID + "|" + instruction},
				{Label: "New task", ActionID: "slack_thread_new", Value: existing.ID + "|" + instruction},
			})
	}
}

func (s *Server) recordSlackThreadTask(ctx context.Context, orgID, teamID, channelID, threadTS, taskID, token, statusText string) {
	statusTS, err := s.slackClient.PostMessage(ctx, token, channelID, threadTS, statusText)
	if err != nil {
		log.Printf("[slackapp] posting status message: %v", err)
	}
	row := &store.SlackTriggeredTask{OrgID: orgID, TeamID: teamID, ChannelID: channelID, ThreadTS: threadTS, QueuedTaskID: taskID, StatusMessageTS: statusTS, LastStatus: "running"}
	if err := s.storage.CreateSlackTriggeredTask(ctx, row); err != nil {
		log.Printf("[slackapp] persist triggered-task row for task %s: %v", taskID, err)
	}
}

var errUnhandledInteraction = errors.New("unhandled interaction")

// handleSlackInteractivity replaces Task 6's placeholder: it now resolves a
// continue/fork/new button click back to the ambiguous case above.
func (s *Server) handleSlackInteractivity(ctx context.Context, formBody []byte) {
	in, ok := slackapp.ParseInteractivity(formBody)
	if !ok {
		return
	}
	parts := strings.SplitN(in.ActionValue, "|", 2)
	if len(parts) != 2 {
		return
	}
	triggeredTaskID, instruction := parts[0], parts[1]

	var existing store.SlackTriggeredTask
	if err := s.db.WithContext(ctx).Where("id = ?", triggeredTaskID).First(&existing).Error; err != nil {
		return
	}
	var parent store.QueuedTask
	if err := s.db.WithContext(ctx).Where("id = ?", existing.QueuedTaskID).First(&parent).Error; err != nil {
		return
	}

	inst, err := s.storage.GetSlackInstallationByTeamID(ctx, in.TeamID)
	if err != nil {
		return
	}
	token, err := s.storage.GetCredentialPlaintext(ctx, inst.OrgID, "SLACK_BOT_TOKEN")
	if err != nil || token == "" {
		return
	}

	switch in.ActionID {
	case "slack_thread_continue":
		task, err := s.planner.SubmitContinuation(ctx, planner.ContinuationInput{OrgID: inst.OrgID, ParentTask: &parent, Instruction: instruction})
		if err == nil {
			s.recordSlackThreadTask(ctx, inst.OrgID, in.TeamID, in.ChannelID, existing.ThreadTS, task.ID, token, "Continuing…")
		}
	case "slack_thread_fork":
		result, err := s.planner.SubmitFork(ctx, planner.ForkInput{OrgID: inst.OrgID, ParentTask: &parent, Instruction: instruction})
		if err == nil {
			s.recordSlackThreadTask(ctx, inst.OrgID, in.TeamID, in.ChannelID, existing.ThreadTS, firstOf(result.TaskIDs), token, fmt.Sprintf("Forking — job `%s`.", result.JobID))
		}
	case "slack_thread_new":
		repoURL, _ := parent.Spec["repo_url"].(string)
		result, err := s.planner.SubmitPlan(ctx, planner.PlanRequest{OrgID: inst.OrgID, Task: instruction, RepoURL: repoURL})
		if err == nil {
			s.recordSlackThreadTask(ctx, inst.OrgID, in.TeamID, in.ChannelID, existing.ThreadTS, firstOf(result.TaskIDs), token, fmt.Sprintf("Starting a new task — job `%s`.", result.JobID))
		}
	}
}
```

- [ ] **Step 4: Wire the branch point into `handleSlackTrigger`**

At the top of `handleSlackTrigger` in `ee/orchestrator/slack_trigger.go`, right after resolving `inst`, add:

```go
	if threadTS != "" {
		if existing, err := s.storage.LatestSlackTriggeredTask(ctx, teamID, channelID, threadTS); err == nil && existing != nil {
			s.handleSlackThreadReply(ctx, teamID, channelID, threadTS, userID, text, existing)
			return
		}
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run 'TestClassifyThreadReply|TestHandleSlackTrigger' -v`
Expected: PASS

- [ ] **Step 6: Full build**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add ee/orchestrator/slack_thread_reply.go ee/orchestrator/slack_thread_reply_test.go ee/orchestrator/slack_trigger.go
git commit -m "feat(orchestrator): classify Slack thread replies into continue/fork/new/ambiguous"
```

---

## Phase H — Investigation-only completion + test-cmd fallback

### Task 12: Investigation-only classification in `pkg/session`

**Files:**
- Modify: `pkg/session/session.go` (`Task` struct, gets a new field)
- Modify: `pkg/session/architect.go` (`PlanInput`, `Plan` prompt)
- Modify: `pkg/session/spec.go` (`Spec`, new field)
- Modify: `pkg/daemon/session_run.go` (bypass the "left the repository unchanged" failure when investigation-only)
- Create/modify: `pkg/session/investigation_test.go`

**Interfaces:**
- Consumes: nothing new — this is the safeguard itself.
- Produces: `session.Task.InvestigationOnly bool`; `session.Spec.NoDiffExpected bool` (set by the Architect's own JSON response, not by the caller — the Architect is the one deciding, given the task, whether this round should produce a diff); `session.Result` gains no new field — `Result.Success` plus the last round's `Spec.NoDiffExpected` is what `session_run.go` reads.

- [ ] **Step 1: Write the failing test asserting the safeguard**

```go
// pkg/session/investigation_test.go
package session

import "testing"

func TestParseSpecAcceptsNoDiffExpectedOnApprove(t *testing.T) {
	resp := `{"verdict": "approve", "summary": "Found the root cause: a nil check missing in auth.go.", "no_diff_expected": true}`
	s, err := parseSpec(resp)
	if err != nil {
		t.Fatalf("parseSpec: %v", err)
	}
	if !s.NoDiffExpected {
		t.Fatal("expected NoDiffExpected to round-trip as true")
	}
}

func TestParseSpecDefaultsNoDiffExpectedToFalse(t *testing.T) {
	resp := `{"verdict": "approve", "summary": "Fixed it."}`
	s, err := parseSpec(resp)
	if err != nil {
		t.Fatalf("parseSpec: %v", err)
	}
	if s.NoDiffExpected {
		t.Fatal("expected NoDiffExpected to default to false when the Architect doesn't say otherwise — a normal task must never accidentally skip the diff requirement")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/session/... -run TestParseSpec -v`
Expected: FAIL — `Spec.NoDiffExpected` undefined.

- [ ] **Step 3: Add the field to `Spec` and thread it through**

In `pkg/session/spec.go`, add to the `Spec` struct:

```go
	// NoDiffExpected marks an approving verdict where the Architect judges
	// no code change is warranted — the task was answered by investigation,
	// not by a fix. Set by the Architect's own response, never inferred
	// after the fact from an empty diff: a code-fixing task that produces
	// no diff must still fail, and the only thing that tells those two
	// cases apart is whether the Architect declared this one investigation-
	// only BEFORE the round ran. See pkg/daemon/session_run.go's use of
	// this field for exactly where that distinction is enforced.
	NoDiffExpected bool `json:"no_diff_expected"`
```

In `pkg/session/session.go`, add to the `Task` struct (near `TestCmd`):

```go
	// InvestigationOnly hints to the Architect that this task may not need
	// a code change — e.g. "investigate this bug" from Slack. It is a hint,
	// not a mandate: the Architect still decides per round via
	// Spec.NoDiffExpected, since even an investigation-flagged task can turn
	// out to need a real fix once the Architect has actually looked.
	InvestigationOnly bool
```

Find where `session.Task` fields are read into the Architect's `PlanInput` (near `TestCmd: task.TestCmd` around `session.go:506`) and add:

```go
		InvestigationOnly: task.InvestigationOnly,
```

Add the matching field to `architect.go`'s `PlanInput` struct (near its own `TestCmd` field):

```go
	// InvestigationOnly is the caller's hint, surfaced in the prompt so the
	// Architect knows a no-diff-expected approval is on the table for this
	// task specifically, rather than something it has to intuit.
	InvestigationOnly bool
```

In `architect.go`'s `Plan` method, right after the existing `fmt.Fprintf(&b, "\n# Verification command\n%s\n", orNone(in.TestCmd))` line, add:

```go
	if in.InvestigationOnly {
		b.WriteString("\nThis task may be answerable by investigation alone, with no code change required. " +
			"If so, set \"no_diff_expected\": true on your approving verdict and put your findings in \"summary\" — " +
			"that becomes the final report instead of a pull request. Only do this when you are confident no fix is " +
			"warranted; if the investigation reveals a real bug to fix, treat it as an ordinary task instead.\n")
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./pkg/session/... -run TestParseSpec -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for a pure decision function**

No existing test covers `runSession`'s post-loop branching directly (there is no `session_run_test.go` today — `pkg/daemon`'s coverage of the neighboring "left the repository unchanged" case, `no_changes_test.go`, tests the lower-level `publishResult` used by the legacy single-file loop, not this session-mode path). Rather than build a heavy integration harness (fake `session.Runner`, fake git delivery, a real temp git repo) to exercise one branch, extract the branch itself into a pure function first — the same "pure decision, thin imperative wrapper" split `pr_comment.go`/`pr_comment_trigger.go` already use elsewhere in this codebase — so it's a plain unit test against a `session.Result` literal, no fakes needed:

```go
// pkg/daemon/investigation_test.go
package daemon

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/session"
)

func TestInvestigationOutcomeMatchesOnNoDiffExpectedSuccess(t *testing.T) {
	res := session.Result{Success: true, NoDiffExpected: true, Summary: "Root cause: nil check missing in auth.go."}
	out, matched := investigationOutcome(res)
	if !matched {
		t.Fatal("expected a match for a successful, no-diff-expected result")
	}
	if !out.ok || out.prURL != "" || out.detail != "Root cause: nil check missing in auth.go." {
		t.Fatalf("got %+v", out)
	}
}

func TestInvestigationOutcomeDoesNotMatchAnOrdinarySuccess(t *testing.T) {
	res := session.Result{Success: true, NoDiffExpected: false, Summary: "Fixed it."}
	if _, matched := investigationOutcome(res); matched {
		t.Fatal("an ordinary (diff-expected) success must fall through to the normal PR-publish path, not be short-circuited here")
	}
}

func TestInvestigationOutcomeDoesNotMatchAFailure(t *testing.T) {
	res := session.Result{Success: false, NoDiffExpected: true}
	if _, matched := investigationOutcome(res); matched {
		t.Fatal("a failed round must never be reported as the investigation-only success path, whatever NoDiffExpected says")
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/daemon/... -run TestInvestigationOutcome -v`
Expected: FAIL — `investigationOutcome` undefined.

- [ ] **Step 7: Implement the pure function and wire it in**

Add `NoDiffExpected bool` to `pkg/session/session.go`'s `Result` struct (find it near `Success`/`Summary`/`Rounds`/`CostUSD`) and set it wherever `Result` is built on an approving verdict — find where `Result{Success: true, ...}` is constructed on `VerdictApprove` and add `NoDiffExpected: spec.NoDiffExpected` there.

```go
// pkg/daemon/investigation_test.go's counterpart — append to session_run.go
// investigationOutcome reports the taskResult for a successful round the
// Architect explicitly declared needs no diff, or (matched=false) says
// nothing — meaning the caller should fall through to the ordinary
// PR-publish path. Kept pure and separate from runSession's imperative flow
// so the decision itself — not the git/session plumbing around it — is what
// a test exercises.
func investigationOutcome(res session.Result) (taskResult, bool) {
	if !res.Success || !res.NoDiffExpected {
		return taskResult{}, false
	}
	return taskResult{ok: true, detail: truncateDetail(res.Summary)}, true
}
```

Then in `pkg/daemon/session_run.go`, change:

```go
	gitToken, gitErr := d.resolveGitToken(ctx, spec.ID, deps.leaseID, creds)
	if gitErr != nil {
		return taskResult{detail: truncateDetail(gitErr.Error()), events: prog.all()}
	}
```

to:

```go
	if out, matched := investigationOutcome(res); matched {
		out.events = prog.all()
		return out
	}

	gitToken, gitErr := d.resolveGitToken(ctx, spec.ID, deps.leaseID, creds)
	if gitErr != nil {
		return taskResult{detail: truncateDetail(gitErr.Error()), events: prog.all()}
	}
```

This runs BEFORE `publishResultFrom` is ever called, so an investigation-only success never reaches the `errNoChanges` check at all — the two are handled as genuinely different outcomes rather than one falling through into the other's error path.

- [ ] **Step 8: Run both new tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./pkg/daemon/... -run TestInvestigationOutcome -v`
Expected: PASS, all three cases.

- [ ] **Step 9: Wire `InvestigationOnly` from the Slack trigger through to the queued task**

In `ee/planner/planner.go`, add to `PlanRequest`:

```go
	// InvestigationOnly hints the Architect that this task may be answerable
	// without a code change. Currently set only by the Slack trigger path.
	InvestigationOnly bool `json:"investigation_only,omitempty"`
```

In `ee/planner/service.go`'s `SubmitPlan`, add `"investigation_only": req.InvestigationOnly` to the `spec` map built per-worker (next to `"test_cmd": workerTestCmd(w, req)`).

In `pkg/daemon/daemon.go` (or wherever `spec.InvestigationOnly` would need reading into `session.Task` alongside the existing `TestCmd: deps.testCmd` in `session_run.go`), add `InvestigationOnly: spec.InvestigationOnly` to the `session.Task{...}` literal built in `runSession` — this requires `agent.WorkerSpec` to also carry an `InvestigationOnly bool` field decoded from `spec["investigation_only"]`; add it next to `WorkerSpec.TestCmd` in `pkg/agent/agent.go`.

- [ ] **Step 10: Full build and test**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./pkg/... ./ee/...`
Expected: clean.

- [ ] **Step 11: Commit**

```bash
git add pkg/session/spec.go pkg/session/session.go pkg/session/architect.go pkg/session/investigation_test.go \
        pkg/daemon/session_run.go pkg/daemon/investigation_test.go pkg/daemon/daemon.go \
        pkg/agent/agent.go ee/planner/planner.go ee/planner/service.go
git commit -m "feat(session): add investigation-only completion path with upfront-classification safeguard"
```

- [ ] **Step 12: Set `InvestigationOnly` from the Slack classifier**

Back in `ee/orchestrator/slack_trigger.go`'s `handleSlackTrigger`, the classification of whether THIS trigger looks investigation-shaped happens as a cheap hint from the same completer used elsewhere in this pipeline (not a hard requirement — the Architect still decides per Step 3 above). Add a small classifier mirroring `classifyThreadReply`'s shape:

```go
// investigationHint asks whether the instruction reads like "investigate/
// find out/explain" rather than "fix/add/change" — a cheap, non-binding
// signal passed to the Architect as InvestigationOnly. Wrong in either
// direction is not costly: false gives the Architect no permission to skip
// the diff it would still be free to conclude isn't needed on the object
// task facts anyway, and true still only hints — the Architect is the one
// that actually decides via NoDiffExpected.
func investigationHint(ctx context.Context, complete completeFunc, instruction string) bool {
	system := `Respond with ONLY JSON: {"investigation_only": true|false}. ` +
		`True if the instruction only asks to investigate, explain, or report — false if it asks for a code change.`
	resp, err := complete(ctx, system, instruction)
	if err != nil {
		return false
	}
	start, end := strings.IndexByte(resp, '{'), strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 {
		return false
	}
	var out struct {
		InvestigationOnly bool `json:"investigation_only"`
	}
	json.Unmarshal([]byte(resp[start:end+1]), &out)
	return out.InvestigationOnly
}
```

Wire it into the `SubmitPlan` call: `InvestigationOnly: investigationHint(ctx, someCompleter, instruction)` — reuse `s.slackCompleter()` from Task 8, falling back to `false` if unavailable (the Architect-side default is safe either way).

- [ ] **Step 13: Test, build, commit**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -v && CGO_ENABLED=0 go build ./...`
Expected: PASS / clean.

```bash
git add ee/orchestrator/slack_trigger.go
git commit -m "feat(orchestrator): hint investigation-only classification from the Slack trigger"
```

### Task 13: Architect-runtime test-cmd fallback

**Files:**
- Modify: `pkg/daemon/daemon.go` (the hard-fail at line ~704 when `testCmd == ""`)
- Modify: `pkg/session/architect.go` (handle an empty `TestCmd` in the opening prompt)
- Create/modify: `pkg/daemon/infer_test.go` or `pkg/session/architect_test.go` (whichever already covers the relevant surface — check both before choosing)

**Interfaces:**
- Consumes: `orNone(in.TestCmd)` (exists in `architect.go`) already renders `""` as `"None"` in the prompt — the missing piece is purely daemon.go's early refusal, not the Architect's prompt rendering.
- Produces: no new exported function — this is a small behavior change plus a test proving the loop no longer requires a pre-existing baseline run when no command exists.

- [ ] **Step 1: Write the failing test**

No existing test covers this exact branch either (`grep -rn "none could be inferred" pkg/daemon/*_test.go` finds nothing — the string only appears in `daemon.go` itself). Same approach as Task 12: extract the condition into a pure, directly-testable predicate rather than standing up a full sandbox/repo-clone integration test for one boolean check.

```go
// pkg/daemon/infer_test.go — append (this file already tests inferTestCmd itself)
func TestTestCmdRequiredIsTrueWhenEmptyAndNotInvestigationOnly(t *testing.T) {
	if !testCmdRequired("", false) {
		t.Fatal("an ordinary task with no test command (inferred or given) must still be required to have one")
	}
}

func TestTestCmdRequiredIsFalseWhenInvestigationOnly(t *testing.T) {
	if testCmdRequired("", true) {
		t.Fatal("an investigation-only task must be allowed to proceed with no test command")
	}
}

func TestTestCmdRequiredIsFalseWhenACommandExists(t *testing.T) {
	if testCmdRequired("go test ./...", false) {
		t.Fatal("a task with a real test command is never blocked by this check, investigation-only or not")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/daemon/... -run TestTestCmdRequired -v`
Expected: FAIL — `testCmdRequired` undefined.

- [ ] **Step 3: Implement**

Add the predicate next to `inferTestCmd` in `pkg/daemon/infer.go`:

```go
// testCmdRequired reports whether a task with no test command (given or
// inferred) must be refused. False only for a task explicitly hinted
// investigation-only — everything else keeps today's behavior exactly,
// including a code-fixing task with no detectable convention.
func testCmdRequired(testCmd string, investigationOnly bool) bool {
	return testCmd == "" && !investigationOnly
}
```

In `pkg/daemon/daemon.go`, change:

```go
	if testCmd == "" {
		return taskResult{detail: "no test command, and none could be inferred from the repo — set one under Advanced options so the fix can be verified", events: prog.all()}
	}
```

to:

```go
	if testCmdRequired(testCmd, spec.InvestigationOnly) {
		return taskResult{detail: "no test command, and none could be inferred from the repo — set one under Advanced options so the fix can be verified", events: prog.all()}
	}
```

This is deliberately narrow: an empty `TestCmd` proceeds into the session loop ONLY when the task was hinted investigation-only. A normal code-fixing task with no detectable test command still fails clearly at submit-adjacent time, exactly as it does today — broadening this to every task would mean a code change could be "verified" by nothing at all, which is a materially different (and much weaker) guarantee than the spec asked for. The Architect, on seeing `orNone(in.TestCmd) == "None"` in its prompt, still decides per round whether to proceed, and — per Task 12 — can only mark a round `NoDiffExpected` when it genuinely judges no fix is warranted; a round that DOES modify code with no verification command run is accepted today already (this is not new — `TestCmd` has always been optional per `workerTestCmd`'s doc comment), so this task changes exactly one thing: which callers are allowed to reach that state with a still-empty command after inference fails.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./pkg/daemon/... -run TestTestCmdRequired -v`
Expected: PASS, all three cases.

- [ ] **Step 5: Full build**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./pkg/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/daemon/daemon.go pkg/daemon/infer.go pkg/daemon/infer_test.go
git commit -m "feat(daemon): let an investigation-only task proceed with no inferable test command"
```

### Task 14: GitHub issue creation on investigation-only completion

**Files:**
- Modify: `ee/orchestrator/slack_completion.go`
- Create: `ee/orchestrator/slack_issue_test.go`

**Interfaces:**
- Consumes: `createIssueComment`-adjacent GitHub primitive — check `ee/orchestrator/github_api.go` or wherever `createIssueComment`/`addReaction` (used by `pr_comment_trigger.go`) actually live, and add a sibling `createIssue(ctx, api, token, owner, repo, title, body string) (htmlURL string, err error)` there following the exact same request-building pattern (do not duplicate the pattern into a new file if a shared "GitHub REST helper" file already holds `createIssueComment`).
- Produces: issue creation wired into `reportSlackCompletion`, gated on the original instruction having asked for one.

- [ ] **Step 1: Add `createIssue` next to the existing GitHub REST helpers**

`ee/orchestrator/github_pr_calls.go` already has `createIssueComment` and a `githubRequest(ctx, method, url, token, bodyMap) (*http.Response, error)` helper it and `addReaction` both build on — add a sibling using that exact same helper rather than a new HTTP client:

```go
// ee/orchestrator/github_pr_calls.go — append
// createIssue files a new issue and returns its html_url. Used only by the
// Slack investigation-only completion path (ee/orchestrator/slack_completion.go),
// and only when the triggering instruction explicitly asked for one.
func createIssue(ctx context.Context, api, token, owner, repo, title, body string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues", api, owner, repo)
	resp, err := githubRequest(ctx, http.MethodPost, url, token, map[string]string{"title": title, "body": body})
	if err != nil {
		return "", fmt.Errorf("create issue on %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("create issue on %s/%s returned %d: %s", owner, repo, resp.StatusCode, string(respBody))
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("decode created issue: %w", err)
	}
	return out.HTMLURL, nil
}
```

Add `encoding/json` to the file's imports if not already present (`createIssueComment` doesn't need it, but this does).

- [ ] **Step 2: Write the failing test**

```go
// ee/orchestrator/slack_issue_test.go
package orchestrator

import "testing"

func TestWantsIssueCreationDetectsExplicitAsk(t *testing.T) {
	if !wantsIssueCreation("investigate this and create a github issue") {
		t.Fatal("expected an explicit ask to be detected")
	}
	if wantsIssueCreation("investigate this bug") {
		t.Fatal("expected no issue creation without an explicit ask")
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestWantsIssueCreation -v`
Expected: FAIL — `wantsIssueCreation` undefined.

- [ ] **Step 4: Implement the detector and wire it in**

```go
// ee/orchestrator/slack_completion.go — append
import "strings"

// wantsIssueCreation reports whether the instruction explicitly asked for a
// GitHub issue — a bounded, opt-in action, not a default behavior for every
// investigation-only completion.
func wantsIssueCreation(instruction string) bool {
	lower := strings.ToLower(instruction)
	return strings.Contains(lower, "create a github issue") || strings.Contains(lower, "create an issue") || strings.Contains(lower, "open an issue") || strings.Contains(lower, "file an issue")
}
```

In `reportSlackCompletion`, inside the `task.Status == store.TaskSucceeded` (investigation-only) branch, after computing `text`, add:

```go
	if task.Status == store.TaskSucceeded && resultURL == "" && wantsIssueCreation(taskInstruction(task)) {
		if ghToken, ok := s.installationToken(ctx, row.OrgID); ok {
			owner, repo, ok := ownerRepoFromSpec(task.Spec)
			if ok {
				if url, err := createIssue(ctx, githubAPIDefault, ghToken, owner, repo, issueTitle(task), resultDetail); err == nil {
					text += fmt.Sprintf("\nFiled as %s", url)
				} else {
					log.Printf("[slackapp] creating issue for task %s: %v", taskID, err)
				}
			}
		}
	}
```

(`resultURL`/`resultDetail` are the plain-string locals `reportSlackCompletion` already derefs at the top of the function per Task 7 — this block reuses them rather than re-reading the `*string` fields.)

Add the three small helpers this calls, using `gitcache.ParseRepo` — already the standard way this codebase turns a stored `repo_url` back into owner/repo (see `ee/planner/repo_auth.go`'s call to it) — rather than a second hand-rolled URL splitter:

```go
// ee/orchestrator/slack_completion.go — append
import "github.com/ibreakthecloud/kiwi/pkg/gitcache"

// taskInstruction is the original objective this task was given, stored on
// every QueuedTask's spec under "task" (see PlannedWorker.Task /
// SubmitPlan's spec map). Empty when the field is somehow missing rather
// than panicking — wantsIssueCreation("") is simply false.
func taskInstruction(task *store.QueuedTask) string {
	s, _ := task.Spec["task"].(string)
	return s
}

// ownerRepoFromSpec resolves the task's repo_url back to an owner/repo pair
// for the GitHub issues API, which addresses by owner/repo rather than by
// URL.
func ownerRepoFromSpec(spec map[string]interface{}) (owner, repo string, ok bool) {
	url, _ := spec["repo_url"].(string)
	r, ok := gitcache.ParseRepo(url)
	if !ok {
		return "", "", false
	}
	return r.Owner, r.Name, true
}

// issueTitle derives a short issue title from the task's own instruction —
// its first line, since an instruction can run to several sentences but a
// GitHub issue title is meant to be a one-line summary.
func issueTitle(task *store.QueuedTask) string {
	instruction := taskInstruction(task)
	if i := strings.IndexByte(instruction, '\n'); i != -1 {
		instruction = instruction[:i]
	}
	if len(instruction) > 120 {
		instruction = instruction[:120] + "…"
	}
	if instruction == "" {
		return "Kiwi investigation"
	}
	return instruction
}
```

- [ ] **Step 5: Run to verify the new test passes**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestWantsIssueCreation -v`
Expected: PASS

- [ ] **Step 6: Full build**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add ee/orchestrator/slack_completion.go ee/orchestrator/slack_issue_test.go
git commit -m "feat(orchestrator): file a GitHub issue on investigation-only completion when explicitly asked"
```

---

## Phase I — Frontend

### Task 15: Dashboard — Add to Slack, channel bindings, README

**Files:**
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/app/(dashboard)/integrations/page.tsx`
- Create: `frontend/src/app/(dashboard)/integrations/slack/page.tsx`
- Modify: `README.md`

**Interfaces:**
- Consumes: `GET/POST /api/v1/integrations/slack/install`, `GET /api/v1/integrations/slack/installations`, `GET/POST /api/v1/integrations/slack/bindings`, `DELETE /api/v1/integrations/slack/bindings/{id}` (Tasks 4-5).
- Produces: `SlackInstallation`, `SlackChannelBinding` TS interfaces + `api.slack*` client methods; a working "Add to Slack" button; a channel-bindings management page.

**Read first, before writing:** `frontend/AGENTS.md` — this Next.js version has breaking changes from training-data conventions; check `node_modules/next/dist/docs/` for anything this task touches that looks unfamiliar (routing, data fetching) before assuming standard Next.js behavior.

- [ ] **Step 1: Add the API types and client methods**

In `frontend/src/lib/api.ts`, next to the `GithubInstallation`/`GithubRepo` interfaces:

```typescript
export interface SlackInstallation {
  team_id: string;
  org_id: string;
  team_name: string;
  installed_by_user_id: string;
  created_at: string;
  updated_at: string;
}

export interface SlackChannelBinding {
  id: string;
  org_id: string;
  team_id: string;
  channel_id: string;
  repo_url: string;
  default_test_cmd: string;
  default_ref: string;
  created_by: string;
  created_at: string;
}
```

Next to wherever `listMonitors`/`createMonitor`/`cancelMonitor` are defined (the object those live on — likely `export const api = { ... }` or similar; match that exact shape):

```typescript
  getSlackInstallURL: () =>
    fetchApi<{ install_url: string }>("/api/v1/integrations/slack/install", {
      headers: { Accept: "application/json" },
    }),
  listSlackInstallations: () =>
    fetchApi<{ installations: SlackInstallation[] }>("/api/v1/integrations/slack/installations"),
  listSlackBindings: () =>
    fetchApi<{ bindings: SlackChannelBinding[] }>("/api/v1/integrations/slack/bindings"),
  createSlackBinding: (input: { team_id: string; channel_id: string; repo_url: string; default_test_cmd?: string; default_ref?: string }) =>
    fetchApi<SlackChannelBinding>("/api/v1/integrations/slack/bindings", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deleteSlackBinding: (id: string) =>
    fetchApi<void>(`/api/v1/integrations/slack/bindings/${id}`, { method: "DELETE" }),
```

- [ ] **Step 2: Add the "Add to Slack" button to the Integrations page**

Read `frontend/src/app/(dashboard)/integrations/page.tsx` first to find how the existing "Connect GitHub" button is implemented (it calls `api.handleGithubInstall`-equivalent and navigates the browser to the returned `install_url`, per `github_install_api.go`'s own comment about why JSON-then-navigate is required for a bearer-token SPA). Add a Slack card immediately below it, following the exact same pattern:

```tsx
  async function connectSlack() {
    const { install_url } = await api.getSlackInstallURL();
    window.location.href = install_url;
  }
```

wired to a button labeled "Add to Slack", styled consistently with the existing GitHub connect card (copy its className/layout rather than inventing new styles), and rendering `installations.map(i => i.team_name)` below it when `listSlackInstallations()` returns any rows (fetched the same way the page already fetches `listGithubInstallations`).

- [ ] **Step 3: Build the channel bindings page**

```tsx
// frontend/src/app/(dashboard)/integrations/slack/page.tsx
"use client";

import { useEffect, useState } from "react";
import { api, SlackChannelBinding } from "@/lib/api";

export default function SlackBindingsPage() {
  const [bindings, setBindings] = useState<SlackChannelBinding[]>([]);
  const [teamID, setTeamID] = useState("");
  const [channelID, setChannelID] = useState("");
  const [repoURL, setRepoURL] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function refresh() {
    const { bindings } = await api.listSlackBindings();
    setBindings(bindings);
  }

  useEffect(() => {
    refresh();
  }, []);

  async function onCreate(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await api.createSlackBinding({ team_id: teamID, channel_id: channelID, repo_url: repoURL });
      setTeamID("");
      setChannelID("");
      setRepoURL("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create binding");
    }
  }

  async function onDelete(id: string) {
    await api.deleteSlackBinding(id);
    await refresh();
  }

  return (
    <div>
      <h1>Slack Channel Bindings</h1>
      <p>Bind a Slack channel to a repository so an @mention in it knows which repo to act on.</p>

      <form onSubmit={onCreate}>
        <input placeholder="Team ID (e.g. T0123456)" value={teamID} onChange={(e) => setTeamID(e.target.value)} required />
        <input placeholder="Channel ID (e.g. C0123456)" value={channelID} onChange={(e) => setChannelID(e.target.value)} required />
        <input placeholder="Repo URL" value={repoURL} onChange={(e) => setRepoURL(e.target.value)} required />
        <button type="submit">Bind channel</button>
      </form>
      {error && <p role="alert">{error}</p>}

      <ul>
        {bindings.map((b) => (
          <li key={b.id}>
            {b.channel_id} → {b.repo_url}
            <button onClick={() => onDelete(b.id)}>Remove</button>
          </li>
        ))}
      </ul>
    </div>
  );
}
```

Match this page's actual markup/styling to whatever component library and layout wrapper `frontend/src/app/(dashboard)/monitors/page.tsx` uses (table component, page header, form styling) — the skeleton above is functionally complete but intentionally unstyled; copy the Monitors page's real JSX structure rather than inventing new UI conventions for one page.

- [ ] **Step 4: Manually verify in the browser**

Run: `make run-local` (or however the frontend dev server starts per the README), open the Integrations page, confirm the "Add to Slack" button navigates to a Slack OAuth URL containing `client_id`, `scope`, `redirect_uri`, and `state` query params (this works even without a real Slack app configured — you're checking the URL construction, not completing a real OAuth round trip). Open `/integrations/slack`, submit the binding form, confirm the new row appears and Remove deletes it.

- [ ] **Step 5: Update the README**

Add a section documenting Slack triggering (mirror the structure of whatever section already documents the `@runkiwi` PR-comment trigger, per README.md:269 referenced in the codebase survey): how to connect ("Add to Slack" in Integrations), how to bind a channel, the `@mention` syntax, the `repo:owner/name` inline override, and that a reply in an already-actioned thread continues/forks/asks depending on what it says.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/api.ts frontend/src/app/\(dashboard\)/integrations/page.tsx frontend/src/app/\(dashboard\)/integrations/slack/page.tsx README.md
git commit -m "feat(frontend): add Slack install button and channel binding management"
```

---

## Final verification (run once, after all 15 tasks)

```bash
gofmt -l cmd/ pkg/ ee/
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test ./pkg/... ./ee/...
CGO_ENABLED=0 go build ./...
cd frontend && npm run build
```

All five must be clean/passing before this branch is considered done, per CLAUDE.md §2.
