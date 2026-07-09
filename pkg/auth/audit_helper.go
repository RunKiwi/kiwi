package auth

import (
	"net/http"

	"github.com/ibreakthecloud/kiwi/pkg/audit"
	"gorm.io/gorm"
)

// LogAuditEvent extracts the caller identity from the request context and writes
// an audit record. It lives in the auth package (not audit) so that the audit
// package stays a leaf and does not import auth — importing auth from audit
// would create an import cycle (auth already depends on audit).
func LogAuditEvent(db *gorm.DB, r *http.Request, action, resource, resourceID, details string) error {
	var orgID, userID, userEmail, clientIP string
	if r != nil {
		clientIP = r.RemoteAddr
		if claims := ClaimsFromContext(r.Context()); claims != nil {
			orgID = claims.OrgID
			userID = claims.UserID
			userEmail = claims.Email
		}
	}
	// audit.LogEventDirect defaults empty orgID/userID to "system".
	return audit.LogEventDirect(db, orgID, userID, userEmail, action, resource, resourceID, details, clientIP)
}
