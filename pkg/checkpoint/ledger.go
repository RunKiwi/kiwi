package checkpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// Ledger gives replay-safety to side-effecting operations (external API calls,
// git pushes, comment posts). Each effect is keyed by hash(job_id, seq,
// signature) and committed exactly once; a crash-and-replay consults the ledger
// and short-circuits anything already committed instead of firing it twice
// (issue #37).
type Ledger struct {
	store store.Store
}

func NewLedger(s store.Store) *Ledger { return &Ledger{store: s} }

// effectID is the stable idempotency key for a side effect.
func effectID(jobID string, seq int64, signature string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", jobID, seq, signature)))
	return hex.EncodeToString(sum[:])
}

// Check reports whether an effect has already been committed, returning its
// cached result URI if so.
func (l *Ledger) Check(ctx context.Context, jobID string, seq int64, signature string) (resultURI string, committed bool, err error) {
	eff, err := l.store.GetSideEffect(ctx, effectID(jobID, seq, signature))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if eff.ResultURI != nil {
		resultURI = *eff.ResultURI
	}
	return resultURI, true, nil
}

// Commit records that an effect has fired, so future replays skip it.
func (l *Ledger) Commit(ctx context.Context, jobID string, seq int64, signature, effectType, resultURI string) error {
	eff := &store.SideEffect{
		ID:         effectID(jobID, seq, signature),
		JobID:      jobID,
		EffectType: effectType,
	}
	if resultURI != "" {
		eff.ResultURI = &resultURI
	}
	return l.store.RecordSideEffect(ctx, eff)
}

// Do runs fn exactly once per (jobID, seq, signature). On the first call it
// executes fn and records the effect; on any replay it returns the cached
// result without invoking fn again. This is the primitive the resume path
// relies on to guarantee "replay never double-fires an effect".
func (l *Ledger) Do(ctx context.Context, jobID string, seq int64, signature, effectType string, fn func() (string, error)) (string, error) {
	if uri, committed, err := l.Check(ctx, jobID, seq, signature); err != nil {
		return "", err
	} else if committed {
		return uri, nil
	}
	resultURI, err := fn()
	if err != nil {
		return "", err
	}
	if err := l.Commit(ctx, jobID, seq, signature, effectType, resultURI); err != nil {
		return "", err
	}
	return resultURI, nil
}
