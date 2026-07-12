package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte, msgID string) error
}

type Relay struct {
	store store.Store
	pub   Publisher
}

func NewRelay(s store.Store, pub Publisher) *Relay {
	return &Relay{store: s, pub: pub}
}

func (r *Relay) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processed, err := r.processBatch(ctx)
			if err != nil {
				log.Printf("[Relay] Error processing batch: %v\n", err)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				ticker.Reset(backoff)
			} else {
				backoff = 1 * time.Second
				ticker.Reset(backoff)
			}
			_ = processed
		}
	}
}

func (r *Relay) processBatch(ctx context.Context) (int, error) {
	var processed int
	err := r.store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var outboxes []store.Outbox
		query := `SELECT * FROM outbox WHERE published_at IS NULL ORDER BY created_at ASC LIMIT 50`
		if tx.Dialector.Name() == "postgres" {
			query += " FOR UPDATE SKIP LOCKED"
		}
		if err := tx.Raw(query).Scan(&outboxes).Error; err != nil {
			return err
		}

		if len(outboxes) == 0 {
			return nil
		}

		for i := range outboxes {
			ob := &outboxes[i]

			if _, ok := ob.Payload["job_id"].(string); !ok {
				log.Printf("[Relay] skipping malformed outbox %d: missing job_id", ob.ID)
				now := time.Now()
				ob.PublishedAt = &now
				tx.Model(ob).Select("published_at").Updates(ob)
				continue
			}

			msgPayload, errMarshal := json.Marshal(ob.Payload)
			if errMarshal != nil {
				return fmt.Errorf("failed to marshal outbox payload %d: %w", ob.ID, errMarshal)
			}

			errPublish := r.pub.Publish(ctx, ob.Topic, msgPayload, fmt.Sprintf("outbox-%d", ob.ID))
			if errPublish != nil {
				return fmt.Errorf("failed to publish outbox %d: %w", ob.ID, errPublish)
			}

			now := time.Now()
			ob.PublishedAt = &now
			if err := tx.Model(ob).Select("published_at").Updates(ob).Error; err != nil {
				return err
			}
			processed++
		}
		return nil
	})

	return processed, err
}
