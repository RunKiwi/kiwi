package audit

import (
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"gorm.io/gorm"
)

// AuditLog represents a security audit event in Kiwi.
type AuditLog struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID      string    `gorm:"index;not null" json:"org_id"`
	UserID     string    `gorm:"index" json:"user_id"`
	UserEmail  string    `json:"user_email"`
	Action     string    `gorm:"index;not null" json:"action"`   // "CREATE", "READ", "UPDATE", "DELETE", "EXECUTE", "REVOKE"
	Resource   string    `gorm:"index;not null" json:"resource"` // "TASK", "API_KEY", "USER", "ORG", "PROVIDER"
	ResourceID string    `gorm:"index" json:"resource_id"`
	Details    string    `json:"details"`
	ClientIP   string    `json:"client_ip"`
	CreatedAt  time.Time `json:"created_at"`
}

// TableName overrides the default GORM table name.
func (AuditLog) TableName() string { return "audit_logs" }

// LogEvent logs a security audit event.
func LogEvent(db *gorm.DB, r *http.Request, action, resource, resourceID, details string) error {
	var orgID string
	var userID string
	var userEmail string
	var clientIP string

	if r != nil {
		clientIP = r.RemoteAddr
		claims := auth.ClaimsFromContext(r.Context())
		if claims != nil {
			orgID = claims.OrgID
			userID = claims.UserID
			userEmail = claims.UserEmail
		}
	}

	// For background system operations without claims, default to "system"
	if orgID == "" {
		orgID = "system"
	}
	if userID == "" {
		userID = "system"
	}

	log := AuditLog{
		OrgID:      orgID,
		UserID:     userID,
		UserEmail:  userEmail,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    details,
		ClientIP:   clientIP,
		CreatedAt:  time.Now(),
	}

	return db.Create(&log).Error
}

// GetOrgAuditLogs retrieves audit logs for an organization.
func GetOrgAuditLogs(db *gorm.DB, orgID string) ([]AuditLog, error) {
	var logs []AuditLog
	err := db.Order("created_at DESC").Where("org_id = ?", orgID).Find(&logs).Error
	return logs, err
}

// LogEventDirect logs a security audit event with explicit metadata (e.g. for background tasks).
func LogEventDirect(db *gorm.DB, orgID, userID, email, action, resource, resourceID, details, clientIP string) error {
	if orgID == "" {
		orgID = "system"
	}
	if userID == "" {
		userID = "system"
	}
	log := AuditLog{
		OrgID:      orgID,
		UserID:     userID,
		UserEmail:  email,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    details,
		ClientIP:   clientIP,
		CreatedAt:  time.Now(),
	}
	return db.Create(&log).Error
}
