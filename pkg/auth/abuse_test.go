package auth

import (
	"testing"
	"time"

	"gorm.io/gorm"
)

func newAbuseDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDB("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Create(&Organization{ID: "o1", Name: "o1", Plan: "free", ActivationState: "active"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	return db
}

func TestRecordAbuseStrike_BelowThresholdDoesNotSuspend(t *testing.T) {
	db := newAbuseDB(t)

	for i := 1; i <= 2; i++ {
		suspended, strikes, err := RecordAbuseStrike(db, "o1", 3, time.Hour)
		if err != nil {
			t.Fatalf("strike %d: %v", i, err)
		}
		if suspended {
			t.Fatalf("strike %d should not suspend (threshold 3)", i)
		}
		if strikes != i {
			t.Errorf("strike %d: want count %d, got %d", i, i, strikes)
		}
	}

	var org Organization
	db.First(&org, "id = ?", "o1")
	if org.ActivationState != "active" {
		t.Errorf("org should stay active below threshold, got %s", org.ActivationState)
	}
}

func TestRecordAbuseStrike_SuspendsAtThreshold(t *testing.T) {
	db := newAbuseDB(t)

	var suspended bool
	var err error
	for i := 1; i <= 3; i++ {
		suspended, _, err = RecordAbuseStrike(db, "o1", 3, time.Hour)
		if err != nil {
			t.Fatalf("strike %d: %v", i, err)
		}
	}
	if !suspended {
		t.Fatal("third strike should suspend")
	}

	var org Organization
	db.First(&org, "id = ?", "o1")
	if org.ActivationState != "suspended" {
		t.Errorf("org should be suspended at threshold, got %s", org.ActivationState)
	}
	if org.AbuseStrikes != 0 {
		t.Errorf("strikes should reset to 0 on suspend, got %d", org.AbuseStrikes)
	}
}

func TestRecordAbuseStrike_DecaysOutsideWindow(t *testing.T) {
	db := newAbuseDB(t)

	// Two strikes inside the window.
	if _, _, err := RecordAbuseStrike(db, "o1", 3, time.Hour); err != nil {
		t.Fatalf("strike 1: %v", err)
	}
	if _, _, err := RecordAbuseStrike(db, "o1", 3, time.Hour); err != nil {
		t.Fatalf("strike 2: %v", err)
	}

	// Backdate the last strike beyond the window; the next one must decay to 1.
	past := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&Organization{}).Where("id = ?", "o1").UpdateColumn("last_abuse_at", past).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	suspended, strikes, err := RecordAbuseStrike(db, "o1", 3, time.Hour)
	if err != nil {
		t.Fatalf("decayed strike: %v", err)
	}
	if suspended {
		t.Error("a decayed strike must not suspend")
	}
	if strikes != 1 {
		t.Errorf("count should restart at 1 after decay, got %d", strikes)
	}
}
