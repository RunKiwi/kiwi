package store

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// AutoRemediate reports whether org orgID has opted into auto-spawning a fix
// task on a REGRESSION verdict. Fails closed: an unknown org or any store
// error reports false, matching PRCommentMode's fallback-to-safe-default
// pattern in pr_comment_mode.go.
func (s *PostgresStore) AutoRemediate(ctx context.Context, orgID string) (bool, error) {
	if orgID == "" {
		return false, nil
	}
	var org Organization
	err := s.db.WithContext(ctx).Select("auto_remediate").Where("id = ?", orgID).First(&org).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return org.AutoRemediate, nil
}

// SetAutoRemediate updates org orgID's auto-remediate toggle.
func (s *PostgresStore) SetAutoRemediate(ctx context.Context, orgID string, on bool) error {
	res := s.db.WithContext(ctx).Model(&Organization{}).
		Where("id = ?", orgID).
		Update("auto_remediate", on)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("organization not found")
	}
	return nil
}
