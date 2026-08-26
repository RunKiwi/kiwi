package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Which payer defaultWorkerModelFor/architectModelFor (ee/planner) reach for
// when nothing else — an explicit submit, a Slack channel binding — names a
// model. Per-org because a BYOC org with its own provider keys may prefer to
// never run on Kiwi's dime even where Kiwi would fund a default for free.
const (
	// ModelSourceKiwi prefers a Kiwi-funded catalog model. The default: it
	// needs no key from the org at all, which is the only kind of default
	// that works for an org that has connected nothing of its own.
	ModelSourceKiwi = "kiwi"
	// ModelSourceBYOK skips the Kiwi-funded cascade entirely and goes
	// straight to the org's own key (DefaultWorkerModel/DefaultArchitectModel).
	ModelSourceBYOK = "byok"
)

// DefaultModelSource is what an org gets before anyone chooses.
const DefaultModelSource = ModelSourceKiwi

func validModelSource(source string) bool {
	switch source {
	case ModelSourceKiwi, ModelSourceBYOK:
		return true
	}
	return false
}

// ModelSource reports this org's default-model-source preference.
//
// A missing org or an unrecognised stored value both fall back to the
// default rather than erroring — this is consulted from the submit path,
// where the only useful answer is "which default should apply," matching
// PRCommentMode's fail-closed-to-the-safe-default pattern.
func (s *PostgresStore) ModelSource(ctx context.Context, orgID string) (string, error) {
	if orgID == "" {
		return DefaultModelSource, nil
	}
	var org Organization
	err := s.db.WithContext(ctx).Select("default_model_source").Where("id = ?", orgID).First(&org).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DefaultModelSource, nil
		}
		return DefaultModelSource, err
	}
	if !validModelSource(org.DefaultModelSource) {
		return DefaultModelSource, nil
	}
	return org.DefaultModelSource, nil
}

// SetModelSource records this org's default-model-source preference.
func (s *PostgresStore) SetModelSource(ctx context.Context, orgID, source string) error {
	if !validModelSource(source) {
		return fmt.Errorf("unknown model source %q", source)
	}
	res := s.db.WithContext(ctx).Model(&Organization{}).
		Where("id = ?", orgID).
		Update("default_model_source", source)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("organization %q not found", orgID)
	}
	return nil
}
