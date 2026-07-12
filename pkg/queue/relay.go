package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/nats-io/nats.go/jetstream"
	"gorm.io/gorm"
)

type Relay struct {
	db *gorm.DB
	js jetstream.JetStream
}

func NewRelay(db *gorm.DB, js jetstream.JetStream) *Relay {
	return &Relay{db: db, js: js}
}

func (r *Relay) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.processBatch(ctx)
		}
	}
}

func (r *Relay) processBatch(ctx context.Context) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var outboxes []store.Outbox
		if err := tx.Raw(`
			SELECT * FROM outboxes
			WHERE published_at IS NULL
			ORDER BY created_at ASC
			LIMIT 50
			FOR UPDATE SKIP LOCKED
		`).Scan(&outboxes).Error; err != nil {
			return err
		}

		if len(outboxes) == 0 {
			return nil
		}

		for i := range outboxes {
			ob := &outboxes[i]

			// We will publish JSON like `{"job_id": "..."}`
			jobID, ok := ob.Payload["job_id"].(string)
			if !ok {
				return fmt.Errorf("missing job_id in outbox payload")
			}

			// We will publish JSON like `{"job_id": "..."}`
			msgPayload := []byte(fmt.Sprintf(`{"job_id":"%s"}`, jobID))

			_, errPublish := r.js.Publish(ctx, ob.Topic, msgPayload, jetstream.WithMsgID(fmt.Sprintf("outbox-%d", ob.ID)))
			if errPublish != nil {
				return fmt.Errorf("failed to publish outbox %d: %w", ob.ID, errPublish)
			}

			now := time.Now()
			ob.PublishedAt = &now
			if err := tx.Model(ob).Select("published_at").Updates(ob).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		fmt.Printf("[Relay] Error processing batch: %v\n", err)
	}
}
