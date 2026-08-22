// pkg/store/slack.go
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *PostgresStore) UpsertSlackInstallation(ctx context.Context, inst *SlackInstallation) error {
	if inst == nil || inst.TeamID == "" {
		return errors.New("slack installation needs a team id")
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "team_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"org_id", "team_name", "installed_by_user_id", "updated_at"}),
	}).Create(inst).Error
}

// SetSlackBotToken encrypts and stores a workspace's bot token directly on
// its own SlackInstallation row, keyed by team_id — not the generic
// Credential table, unique on (org_id, name), which a second workspace
// connected by the same org would silently overwrite the first's token in.
// The installation row must already exist (UpsertSlackInstallation first).
func (s *PostgresStore) SetSlackBotToken(ctx context.Context, teamID, plaintext string) error {
	enc, err := crypto.EncryptAtRest(plaintext)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&SlackInstallation{}).
		Where("team_id = ?", teamID).
		Update("encrypted_bot_token", enc).Error
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

// CreateSlackTriggeredTask assigns an id that sorts by creation order even
// when two rows land on the same wall-clock timestamp — SQLite's default
// time column precision (used in tests) and, in principle, two Postgres
// inserts within the same microsecond can both tie on created_at, and
// LatestSlackTriggeredTask has to return a deterministic, actually-latest
// row regardless: a thread's continue/fork/new classification reads whatever
// it picks as "current context".
func (s *PostgresStore) CreateSlackTriggeredTask(ctx context.Context, t *SlackTriggeredTask) error {
	if t.ID == "" {
		t.ID = fmt.Sprintf("stt_%020d_%s", time.Now().UnixNano(), randHex(4))
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
		Order("created_at desc, id desc").
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

// RecordSlackEvent claims a Slack Events API delivery's event_id, reporting
// fresh=false when it has already been claimed — Slack retries a delivery
// that did not get a 200 within 3 seconds, and again later if that retry
// also fails, so the same event_id can arrive more than once for reasons
// that have nothing to do with anything actually going wrong. The unique
// index on event_id is what makes this atomic under concurrent deliveries
// of the same retry: a conflict here IS the answer "already claimed", the
// same shape QueuedTask.TriggerCommentID gives GitHub PR-comment redelivery.
func (s *PostgresStore) RecordSlackEvent(ctx context.Context, eventID string) (fresh bool, err error) {
	if eventID == "" {
		// Not every delivery carries one worth deduping on (Slack's own
		// docs don't guarantee event_id on every payload shape) — treat a
		// missing id as "cannot dedup this one", not as a claim that blocks
		// every other id-less delivery behind it.
		return true, nil
	}
	res := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&SlackProcessedEvent{EventID: eventID})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

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
