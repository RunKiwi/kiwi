package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExecutionRecord is one signed, chained provenance record for a job.
type ExecutionRecord struct {
	RecordID        string `gorm:"primaryKey"`
	OrgID           string `gorm:"index:idx_exec_records_org_job,unique,priority:1"`
	JobID           string `gorm:"index:idx_exec_records_org_job,unique,priority:2"`
	Ver             string
	PrevRecordHash  string
	RecordHash      string
	Body            json.RawMessage `gorm:"type:jsonb"`
	ExecSignature   string
	RecordSignature string
	SigningKeyID    string
	CreatedAt       time.Time
}

func (ExecutionRecord) TableName() string { return "execution_records" }

// ExecutionRecordHead is the per-org chain tip: one row, one write per record.
type ExecutionRecordHead struct {
	OrgID     string `gorm:"primaryKey"`
	HeadHash  string
	UpdatedAt time.Time
}

func (ExecutionRecordHead) TableName() string { return "execution_record_heads" }

// ErrRecordExists reports that a record for this job was already appended. It
// is not a failure: assembly is triggered per reported task, so a multi-worker
// job attempts it more than once and all but the first must no-op.
var ErrRecordExists = errors.New("store: execution record already exists for job")

// AppendExecutionRecord atomically appends one record to an org's chain.
//
// The chain head is read *inside* the transaction, under a row lock, and handed
// to build so the record is constructed against the head it will actually link
// to. Reading the head first and building outside would let two jobs finishing
// concurrently in the same org derive the same prev_record_hash, and one of
// them would be silently dropped — a gap that is indistinguishable from
// tampering in a log whose entire purpose is to be tamper-evident.
//
// build receives the current head ("" for an org's first record) and returns
// the record to insert, whose PrevRecordHash must equal that head.
func (s *PostgresStore) AppendExecutionRecord(
	ctx context.Context,
	orgID, jobID string,
	build func(prevHash string) (*ExecutionRecord, error),
) (*ExecutionRecord, error) {
	var out *ExecutionRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Idempotency inside the same transaction that inserts, so a concurrent
		// duplicate cannot slip between the check and the write.
		var existing ExecutionRecord
		err := tx.Where("org_id = ? AND job_id = ?", orgID, jobID).First(&existing).Error
		if err == nil {
			return ErrRecordExists
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var head ExecutionRecordHead
		headErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("org_id = ?", orgID).First(&head).Error
		if headErr != nil && !errors.Is(headErr, gorm.ErrRecordNotFound) {
			return headErr
		}
		genesis := errors.Is(headErr, gorm.ErrRecordNotFound)

		prev := ""
		if !genesis {
			prev = head.HeadHash
		}

		rec, err := build(prev)
		if err != nil {
			return err
		}
		if rec == nil {
			return errors.New("store: build returned no record")
		}
		if rec.PrevRecordHash != prev {
			return errors.New("store: built record does not link to the current chain head")
		}

		if err := tx.Create(rec).Error; err != nil {
			return err
		}

		if genesis {
			if err := tx.Create(&ExecutionRecordHead{
				OrgID: orgID, HeadHash: rec.RecordHash, UpdatedAt: time.Now(),
			}).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&ExecutionRecordHead{}).
				Where("org_id = ?", orgID).
				Updates(map[string]interface{}{
					"head_hash":  rec.RecordHash,
					"updated_at": time.Now(),
				}).Error; err != nil {
				return err
			}
		}
		out = rec
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetExecutionRecordChainHead returns the current chain head for an org, or ""
// when the org has no records yet.
func (s *PostgresStore) GetExecutionRecordChainHead(ctx context.Context, orgID string) (string, error) {
	var head ExecutionRecordHead
	err := s.db.WithContext(ctx).Where("org_id = ?", orgID).First(&head).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return head.HeadHash, nil
}

// GetExecutionRecord returns an org's record for a job. It is scoped by org so
// a job ID from another tenant can never resolve.
func (s *PostgresStore) GetExecutionRecord(ctx context.Context, orgID, jobID string) (*ExecutionRecord, error) {
	var rec ExecutionRecord
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND job_id = ?", orgID, jobID).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetQueuedTask returns one task by ID. The caller must check OrgID before
// acting on it — this is a lookup by primary key, not an authorization check.
func (s *PostgresStore) GetQueuedTask(ctx context.Context, taskID string) (*QueuedTask, error) {
	var t QueuedTask
	if err := s.db.WithContext(ctx).Where("id = ?", taskID).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// GetManifest returns a manifest by its ID.
func (s *PostgresStore) GetManifest(ctx context.Context, id string) (*Manifest, error) {
	var m Manifest
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}
