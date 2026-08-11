package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// What a review comment on a Kiwi pull request is allowed to do.
//
// This is per-org rather than a product-wide rule because the right answer
// depends on how a team reviews. A team that wants Kiwi to behave like a
// colleague sets "any"; a team that wants it to speak only when spoken to
// leaves it at "mention"; a team that wants none of it sets "off".
const (
	// PRCommentModeOff ignores comments entirely.
	PRCommentModeOff = "off"
	// PRCommentModeMention acts only on a comment that mentions Kiwi. The
	// default, and deliberately the conservative one: on "any", a comment
	// saying "thanks, looks good" spends a round of somebody's allowance.
	PRCommentModeMention = "mention"
	// PRCommentModeAny treats every comment from a user with write access as an
	// instruction.
	PRCommentModeAny = "any"
)

// DefaultPRCommentMode is what an org gets before anyone chooses.
const DefaultPRCommentMode = PRCommentModeMention

func validPRCommentMode(mode string) bool {
	switch mode {
	case PRCommentModeOff, PRCommentModeMention, PRCommentModeAny:
		return true
	}
	return false
}

// PRCommentMode reports what a comment does for this org.
//
// A missing org is not an error: this is consulted from a webhook, where the
// only useful answer is "what should I do", and failing the delivery because a
// row is missing would teach GitHub to disable the hook. An unrecognised
// stored value falls back to the default for the same reason — the setting is
// a safety rail, and a broken rail must fail closed rather than open.
func (s *PostgresStore) PRCommentMode(ctx context.Context, orgID string) (string, error) {
	if orgID == "" {
		return DefaultPRCommentMode, nil
	}
	var org Organization
	err := s.db.WithContext(ctx).Select("pr_comment_mode").Where("id = ?", orgID).First(&org).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DefaultPRCommentMode, nil
		}
		return DefaultPRCommentMode, err
	}
	if !validPRCommentMode(org.PRCommentMode) {
		return DefaultPRCommentMode, nil
	}
	return org.PRCommentMode, nil
}

// SetPRCommentMode records what a comment should do for this org.
func (s *PostgresStore) SetPRCommentMode(ctx context.Context, orgID, mode string) error {
	if !validPRCommentMode(mode) {
		return fmt.Errorf("unknown pr comment mode %q", mode)
	}
	res := s.db.WithContext(ctx).Model(&Organization{}).
		Where("id = ?", orgID).
		Update("pr_comment_mode", mode)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// Creating the row here would leave an organisation with no name and no
		// plan. A caller setting a mode for an org that does not exist has a bug.
		return fmt.Errorf("organization %q not found", orgID)
	}
	return nil
}
