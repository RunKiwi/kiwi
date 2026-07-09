package billing

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// UserUsage represents the billing/cost aggregate for a specific user.
type UserUsage struct {
	UserID       string  `json:"user_id"`
	UserEmail    string  `json:"user_email"`
	TotalCost    float64 `json:"total_cost"`
	TaskCount    int     `json:"task_count"`
	SuccessCount int     `json:"success_count"`
	FailedCount  int     `json:"failed_count"`
}

// OrgUsage represents the billing/cost aggregate for an organization.
type OrgUsage struct {
	OrgID        string      `json:"org_id"`
	TotalCost    float64     `json:"total_cost"`
	TaskCount    int         `json:"task_count"`
	SuccessCount int         `json:"success_count"`
	FailedCount  int         `json:"failed_count"`
	TopUsers     []UserUsage `json:"top_users,omitempty"`
}

// GetOrgUsage aggregates task metrics and billing data for an organization within a date range.
func GetOrgUsage(db *gorm.DB, orgID string, from, to time.Time) (*OrgUsage, error) {
	var usage OrgUsage
	usage.OrgID = orgID

	var stats struct {
		TotalCost float64
		Total     int64
		Success   int64
		Failed    int64
	}

	err := db.Table("task_states").
		Where("org_id = ? AND created_at >= ? AND created_at <= ?", orgID, from, to).
		Select("COALESCE(SUM(cost), 0) as total_cost, COUNT(*) as total, SUM(CASE WHEN status = 'SUCCESS' THEN 1 ELSE 0 END) as success, SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed").
		Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	usage.TotalCost = stats.TotalCost
	usage.TaskCount = int(stats.Total)
	usage.SuccessCount = int(stats.Success)
	usage.FailedCount = int(stats.Failed)

	// Fetch top users in the org by total cost
	var userStats []struct {
		UserID    string
		UserEmail string
		Cost      float64
		Total     int64
		Success   int64
		Failed    int64
	}

	err = db.Table("task_states").
		Where("org_id = ? AND created_at >= ? AND created_at <= ?", orgID, from, to).
		Select("user_id, user_email, COALESCE(SUM(cost), 0) as cost, COUNT(*) as total, SUM(CASE WHEN status = 'SUCCESS' THEN 1 ELSE 0 END) as success, SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed").
		Group("user_id, user_email").
		Order("cost DESC").
		Limit(10).
		Scan(&userStats).Error

	if err != nil {
		return nil, err
	}

	for _, us := range userStats {
		usage.TopUsers = append(usage.TopUsers, UserUsage{
			UserID:       us.UserID,
			UserEmail:    us.UserEmail,
			TotalCost:    us.Cost,
			TaskCount:    int(us.Total),
			SuccessCount: int(us.Success),
			FailedCount:  int(us.Failed),
		})
	}

	return &usage, nil
}

// GetUserUsage aggregates task metrics and billing data for a user within a date range.
func GetUserUsage(db *gorm.DB, userID string, from, to time.Time) (*UserUsage, error) {
	var stats struct {
		UserID    string
		UserEmail string
		TotalCost float64
		Total     int64
		Success   int64
		Failed    int64
	}

	err := db.Table("task_states").
		Where("user_id = ? AND created_at >= ? AND created_at <= ?", userID, from, to).
		Select("user_id, user_email, COALESCE(SUM(cost), 0) as total_cost, COUNT(*) as total, SUM(CASE WHEN status = 'SUCCESS' THEN 1 ELSE 0 END) as success, SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed").
		Group("user_id, user_email").
		Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	return &UserUsage{
		UserID:       stats.UserID,
		UserEmail:    stats.UserEmail,
		TotalCost:    stats.TotalCost,
		TaskCount:    int(stats.Total),
		SuccessCount: int(stats.Success),
		FailedCount:  int(stats.Failed),
	}, nil
}

// ParseDateParams extracts start and end time filters from HTTP query parameters,
// falling back to start of current month and now respectively.
func ParseDateParams(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now()
	// Default 'from' is start of current month
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	to := now

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		} else if sec, err := strconv.ParseInt(fromStr, 10, 64); err == nil {
			from = time.Unix(sec, 0)
		} else {
			return from, to, fmt.Errorf("invalid 'from' date format (use RFC3339 or Unix timestamp)")
		}
	}

	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		} else if sec, err := strconv.ParseInt(toStr, 10, 64); err == nil {
			to = time.Unix(sec, 0)
		} else {
			return from, to, fmt.Errorf("invalid 'to' date format (use RFC3339 or Unix timestamp)")
		}
	}

	return from, to, nil
}
