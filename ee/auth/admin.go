// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/audit"
	"github.com/ibreakthecloud/kiwi/ee/billing"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// AdminRouter registers admin-only API endpoints for managing orgs, users, and
// keys. Access is two-tier: most routes are gated by authorizeOrgAccess, which
// accepts either a global super-admin (KIWI_SUPER_ADMIN_EMAILS, or the
// KIWI_SERVER_TOKEN) or an org-scoped "admin" role acting on their own org. A
// small set of org-lifecycle routes — activate, suspend, plan, grant — plus
// the top-level org create/list and /admin/stats remain gated by
// isAdminAuthorized directly, so only a super-admin can reach them.
func AdminRouter(db *gorm.DB, mux *http.ServeMux) {
	mux.HandleFunc("/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if !isAdminAuthorized(r) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleAdminUsersSearch(db, w, r)
	})

	mux.HandleFunc("/admin/stats", func(w http.ResponseWriter, r *http.Request) {
		if !isAdminAuthorized(r) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		handleAdminStats(db, w, r)
	})

	mux.HandleFunc("/admin/metrics/fleet", func(w http.ResponseWriter, r *http.Request) {
		if !isAdminAuthorized(r) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		handleAdminMetricsFleet(db, w, r)
	})

	mux.HandleFunc("/admin/orgs", func(w http.ResponseWriter, r *http.Request) {
		if !isAdminAuthorized(r) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}

		switch r.Method {
		case http.MethodPost:
			handleCreateOrg(db, w, r)
		case http.MethodGet:
			handleListOrgs(db, w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/admin/orgs/", func(w http.ResponseWriter, r *http.Request) {
		// /admin/orgs/{orgID}/users[/{userID}/keys[/{keyID}]]
		path := strings.TrimPrefix(r.URL.Path, "/admin/orgs/")
		parts := strings.Split(path, "/")

		switch {
		case len(parts) == 2 && parts[1] == "activate":
			if !isAdminAuthorized(r) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			orgID := parts[0]
			if r.Method == http.MethodPost {
				handleActivateOrg(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "suspend":
			if !isAdminAuthorized(r) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			orgID := parts[0]
			if r.Method == http.MethodPost {
				handleSuspendOrg(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "usage":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handleOrgUsageAdmin(db, w, r, orgID)

		case len(parts) == 2 && parts[1] == "audit":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handleOrgAuditLogsAdmin(db, w, r, orgID)

		case len(parts) == 2 && parts[1] == "model_usage":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handleOrgModelUsageAdmin(db, w, r, orgID)

		case len(parts) == 2 && parts[1] == "provider":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			switch r.Method {
			case http.MethodPut:
				handleSaveProviderConfig(db, w, r, orgID)
			case http.MethodGet:
				handleGetProviderConfig(db, w, r, orgID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "users":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			switch r.Method {
			case http.MethodPost:
				handleCreateUser(db, w, r, orgID)
			case http.MethodGet:
				handleListUsers(db, w, r, orgID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 4 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			userID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			switch r.Method {
			case http.MethodPost:
				handleCreateAPIKey(db, w, r, orgID, userID)
			case http.MethodGet:
				handleListAPIKeys(db, w, r, orgID, userID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 4 && parts[1] == "users" && parts[3] == "sessions":
			orgID := parts[0]
			userID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handleListSessions(db, w, r, orgID, userID)

		case len(parts) == 5 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			keyID := parts[4]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodDelete {
				handleRevokeAPIKey(db, w, r, orgID, keyID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "plan":
			if !isAdminAuthorized(r) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			orgID := parts[0]
			if r.Method == http.MethodPost {
				handleUpdateOrgPlan(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "grant":
			if !isAdminAuthorized(r) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			orgID := parts[0]
			if r.Method == http.MethodPost {
				handleGrantOrgMinutes(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "join_requests":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodGet {
				handleListJoinRequests(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 4 && parts[1] == "join_requests" && parts[3] == "approve":
			orgID := parts[0]
			reqID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPost {
				handleApproveJoinRequest(db, w, r, orgID, reqID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 4 && parts[1] == "join_requests" && parts[3] == "deny":
			orgID := parts[0]
			reqID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPost {
				handleDenyJoinRequest(db, w, r, orgID, reqID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "domain_join":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPut {
				handleToggleDomainJoin(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "name":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPut {
				handleUpdateOrgName(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
	})

	// Auth validation endpoint (used by the dashboard to verify tokens and get user info).

	mux.HandleFunc("/auth/validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claims := ClaimsFromContext(r.Context())
		if claims == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Look up org name for display.
		orgName := claims.OrgID
		activationState := "inactive"
		plan := "free"
		domainJoin := false
		primaryDomain := ""
		var org Organization
		if err := db.First(&org, "id = ?", claims.OrgID).Error; err == nil {
			orgName = org.Name
			activationState = org.ActivationState
			plan = org.Plan
			domainJoin = org.DomainJoin
			primaryDomain = org.PrimaryDomain
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id":          claims.UserID,
			"email":            claims.Email,
			"name":             claims.Name,
			"org_id":           claims.OrgID,
			"org_name":         orgName,
			"role":             claims.Role,
			"activation_state": activationState,
			"plan":             plan,
			"domain_join":      domainJoin,
			"primary_domain":   primaryDomain,
		})
	})
}

func isAdminAuthorized(r *http.Request) bool {
	claims := ClaimsFromContext(r.Context())
	if claims != nil {
		// Allow bootstrap server token
		if claims.UserID == "system" {
			return true
		}

		// Allow global super admins
		if IsSuperAdmin(claims.Email) {
			return true
		}
	}

	// Fallback to KIWI_SERVER_TOKEN
	expectedToken := os.Getenv("KIWI_SERVER_TOKEN")
	if expectedToken != "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == expectedToken {
				return true
			}
		}
	}

	return false
}

// authorizeOrgAccess grants access to super-admins (via isAdminAuthorized) or
// to an org-scoped admin acting on their own org.
func authorizeOrgAccess(r *http.Request, orgID string) bool {
	if isAdminAuthorized(r) {
		return true
	}
	claims := ClaimsFromContext(r.Context())
	return claims != nil && claims.IsAdmin() && claims.OrgID == orgID
}

func handleCreateOrg(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "Bad request: 'name' is required", http.StatusBadRequest)
		return
	}

	id, err := generateHexID(4)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	org := Organization{
		ID:        id,
		Name:      body.Name,
		CreatedAt: time.Now(),
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&org).Error; err != nil {
			return err
		}
		if org.Plan == "" || org.Plan == "free" {
			if err := tx.Create(FreeLimits(org.ID)).Error; err != nil {
				return err
			}
		}
		if err := CreateDefaultFleet(tx, org.ID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "Organization name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create organization", http.StatusInternalServerError)
		return
	}

	_ = LogAuditEvent(db, r, "CREATE", "ORG", org.ID, fmt.Sprintf("Created organization %q", org.Name))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(org)
}

func handleListOrgs(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	var orgs []Organization
	if err := db.Order("created_at desc").Find(&orgs).Error; err != nil {
		http.Error(w, "Failed to list organizations", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orgs)
}

func handleActivateOrg(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	if err := ActivateOrg(db, orgID); err != nil {
		http.Error(w, "Failed to activate org: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = LogAuditEvent(db, r, "ACTIVATE", "ORG", orgID, "Admin manually activated org")
	w.WriteHeader(http.StatusOK)
}

func handleSuspendOrg(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	if err := SuspendOrg(db, orgID); err != nil {
		http.Error(w, "Failed to suspend org: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = LogAuditEvent(db, r, "SUSPEND", "ORG", orgID, "Admin manually suspended org")
	w.WriteHeader(http.StatusOK)
}

func handleCreateUser(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	// Verify the org exists.
	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	var body struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		http.Error(w, "Bad request: 'email' is required", http.StatusBadRequest)
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if body.Role != "admin" && body.Role != "member" {
		http.Error(w, "Bad request: role must be 'admin' or 'member'", http.StatusBadRequest)
		return
	}

	id, err := generateHexID(4)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	user := User{
		ID:        id,
		Email:     body.Email,
		Name:      body.Name,
		OrgID:     orgID,
		Role:      body.Role,
		CreatedAt: time.Now(),
	}
	if err := db.Create(&user).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "Email already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	_ = LogAuditEvent(db, r, "CREATE", "USER", user.ID, fmt.Sprintf("Registered user %q (%s) with role %q", user.Name, user.Email, user.Role))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func handleListUsers(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	var users []User
	if err := db.Where("org_id = ?", orgID).Order("created_at desc").Find(&users).Error; err != nil {
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

func handleCreateAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, userID string) {
	// Verify user exists and belongs to orgID — without this, an org-scoped
	// admin (authorized only for their own orgID) could mint a key for a
	// user in a different org just by supplying that user's ID.
	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil || user.OrgID != orgID {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	var body struct {
		Label     string `json:"label"`
		ExpiresIn string `json:"expires_in"` // Go duration string, e.g. "720h" for 30 days
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if body.Label == "" {
		body.Label = "default"
	}

	var expiresAt *time.Time
	if body.ExpiresIn != "" {
		d, err := time.ParseDuration(body.ExpiresIn)
		if err != nil {
			http.Error(w, "Bad request: invalid expires_in duration", http.StatusBadRequest)
			return
		}
		t := time.Now().Add(d)
		expiresAt = &t
	}

	plaintext, apiKey, err := GenerateAPIKey(userID, body.Label, expiresAt)
	if err != nil {
		http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
		return
	}

	if err := db.Create(apiKey).Error; err != nil {
		http.Error(w, "Failed to save API key", http.StatusInternalServerError)
		return
	}

	_ = LogAuditEvent(db, r, "CREATE", "API_KEY", apiKey.ID, fmt.Sprintf("Generated API Key %q for user ID %s", apiKey.Label, apiKey.UserID))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key_id":     apiKey.ID,
		"key":        plaintext, // Shown once, never stored in plaintext
		"label":      apiKey.Label,
		"user_id":    apiKey.UserID,
		"created_at": apiKey.CreatedAt,
		"expires_at": apiKey.ExpiresAt,
	})
}

func handleListAPIKeys(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, userID string) {
	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil || user.OrgID != orgID {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	var keys []APIKey
	if err := db.Where("user_id = ? AND revoked_at IS NULL", userID).Order("created_at desc").Find(&keys).Error; err != nil {
		http.Error(w, "Failed to list keys", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// dashboardSessionResponse is what handleListSessions returns: the stored
// DashboardSession fields plus the derived length ops actually asked for
// ("how long was their session"), so the API answers that directly instead
// of making every caller subtract LastActivityAt - StartedAt itself.
type dashboardSessionResponse struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	OrgID           string    `json:"org_id"`
	StartedAt       time.Time `json:"started_at"`
	LastActivityAt  time.Time `json:"last_activity_at"`
	DurationSeconds float64   `json:"duration_seconds"`
}

// handleListSessions returns a user's most recent dashboard sessions,
// newest first, so ops can see session-length history rather than a single
// last-seen timestamp.
func handleListSessions(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, userID string) {
	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil || user.OrgID != orgID {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	var sessions []DashboardSession
	if err := db.Where("user_id = ?", userID).Order("started_at desc").Limit(20).Find(&sessions).Error; err != nil {
		http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
		return
	}
	resp := make([]dashboardSessionResponse, len(sessions))
	for i, s := range sessions {
		resp[i] = dashboardSessionResponse{
			ID:              s.ID,
			UserID:          s.UserID,
			OrgID:           s.OrgID,
			StartedAt:       s.StartedAt,
			LastActivityAt:  s.LastActivityAt,
			DurationSeconds: s.LastActivityAt.Sub(s.StartedAt).Seconds(),
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleRevokeAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, keyID string) {
	var key APIKey
	if err := db.First(&key, "id = ?", keyID).Error; err != nil {
		http.Error(w, "Key not found or already revoked", http.StatusNotFound)
		return
	}
	var user User
	if err := db.First(&user, "id = ?", key.UserID).Error; err != nil || user.OrgID != orgID {
		http.Error(w, "Key not found or already revoked", http.StatusNotFound)
		return
	}

	now := time.Now()
	result := db.Model(&APIKey{}).Where("id = ? AND revoked_at IS NULL", keyID).Update("revoked_at", &now)
	if result.Error != nil {
		http.Error(w, "Failed to revoke key", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected == 0 {
		http.Error(w, "Key not found or already revoked", http.StatusNotFound)
		return
	}

	_ = LogAuditEvent(db, r, "REVOKE", "API_KEY", keyID, "Revoked API Key")

	w.WriteHeader(http.StatusNoContent)
}

// generateHexID generates a random hex string of the given byte length.
func generateHexID(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func handleOrgUsageAdmin(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	// Verify org exists
	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	from, to, err := billing.ParseDateParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	usage, err := billing.GetOrgUsage(db, orgID, from, to)
	if err != nil {
		http.Error(w, "Failed to aggregate usage statistics: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usage)
}

func handleSaveProviderConfig(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	// Verify org exists
	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	var body struct {
		ProviderName string `json:"provider_name"`
		APIKey       string `json:"api_key"`
		ActorModel   string `json:"actor_model"`
		CriticModel  string `json:"critic_model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if body.ProviderName == "" {
		body.ProviderName = "anthropic"
	}

	// Encrypt the API key if provided
	var encryptedKey string
	if body.APIKey != "" {
		enc, err := EncryptKey(body.APIKey)
		if err != nil {
			http.Error(w, "Failed to encrypt API key", http.StatusInternalServerError)
			return
		}
		encryptedKey = enc
	}

	// Create or update OrgProviderConfig
	config := OrgProviderConfig{
		OrgID:        orgID,
		ProviderName: body.ProviderName,
		ActorModel:   body.ActorModel,
		CriticModel:  body.CriticModel,
	}

	// Fetch existing config to preserve encrypted key if no new key is sent
	var existing OrgProviderConfig
	if err := db.First(&existing, "org_id = ?", orgID).Error; err == nil {
		if encryptedKey != "" {
			config.EncryptedAPIKey = encryptedKey
		} else {
			config.EncryptedAPIKey = existing.EncryptedAPIKey
		}
		if err := db.Model(&existing).Updates(&config).Error; err != nil {
			http.Error(w, "Failed to update provider config", http.StatusInternalServerError)
			return
		}
	} else {
		config.EncryptedAPIKey = encryptedKey
		if err := db.Create(&config).Error; err != nil {
			http.Error(w, "Failed to create provider config", http.StatusInternalServerError)
			return
		}
	}

	_ = LogAuditEvent(db, r, "UPDATE", "PROVIDER", config.OrgID, fmt.Sprintf("Updated LLM provider configuration (actor: %s, critic: %s)", config.ActorModel, config.CriticModel))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func handleGetProviderConfig(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	// Verify org exists
	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	config, err := GetProviderConfig(db, orgID)
	if err != nil {
		http.Error(w, "Failed to load provider config", http.StatusInternalServerError)
		return
	}
	if config == nil {
		config = &OrgProviderConfig{
			OrgID:        orgID,
			ProviderName: "anthropic",
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func handleOrgAuditLogsAdmin(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	// Verify org exists
	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	logs, err := audit.GetOrgAuditLogs(db, orgID)
	if err != nil {
		http.Error(w, "Failed to load audit logs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

// AdminUsageRow is one row of the platform-wide model or provider usage
// breakdown: CostUSD is the real dollar cost recorded at metering time
// regardless of who funded it, and KiwiCostUSD is the subset of that spent on
// Kiwi-funded (free tier) work — the number a super-admin actually needs to
// watch for abuse, since it is never billed back to anyone.
type AdminUsageRow struct {
	Model       string  `json:"model,omitempty"`
	Provider    string  `json:"provider"`
	TaskCount   int64   `json:"task_count"`
	CostUSD     float64 `json:"cost_usd"`
	KiwiCostUSD float64 `json:"kiwi_cost_usd"`
	TokensIn    int64   `json:"tokens_in"`
	TokensOut   int64   `json:"tokens_out"`
}

func handleAdminMetricsFleet(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	var daemons []store.Daemon
	if err := db.Find(&daemons).Error; err != nil {
		http.Error(w, "Failed to load fleet metrics", http.StatusInternalServerError)
		return
	}
	var activeContainers int
	for _, d := range daemons {
		activeContainers += d.ActiveContainers
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_daemons":     len(daemons),
		"active_containers": activeContainers,
	})
}

func handleAdminStats(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	var resp struct {
		TotalOrgs             int64            `json:"total_orgs"`
		OrgsByPlan            map[string]int64 `json:"orgs_by_plan"`
		OrgsByActivationState map[string]int64 `json:"orgs_by_activation_state"`
		SignupsLast7Days      int64            `json:"signups_last_7_days"`
		SignupsLast30Days     int64            `json:"signups_last_30_days"`
		TotalAgentMinutes     float64          `json:"total_agent_minutes"`
		TasksByStatus         map[string]int64 `json:"tasks_by_status"`
		ModelUsage            []AdminUsageRow  `json:"model_usage"`
		ProviderUsage         []AdminUsageRow  `json:"provider_usage"`
	}
	resp.OrgsByPlan = make(map[string]int64)
	resp.OrgsByActivationState = make(map[string]int64)

	db.Model(&Organization{}).Count(&resp.TotalOrgs)

	var planCounts []struct {
		Plan  string
		Count int64
	}
	db.Model(&Organization{}).Select("plan, count(*) as count").Group("plan").Scan(&planCounts)
	for _, pc := range planCounts {
		resp.OrgsByPlan[pc.Plan] = pc.Count
	}

	var stateCounts []struct {
		ActivationState string
		Count           int64
	}
	db.Model(&Organization{}).Select("activation_state, count(*) as count").Group("activation_state").Scan(&stateCounts)
	for _, sc := range stateCounts {
		resp.OrgsByActivationState[sc.ActivationState] = sc.Count
	}

	now := time.Now()
	db.Model(&Organization{}).Where("created_at >= ?", now.Add(-7*24*time.Hour)).Count(&resp.SignupsLast7Days)
	db.Model(&Organization{}).Where("created_at >= ?", now.Add(-30*24*time.Hour)).Count(&resp.SignupsLast30Days)

	db.Table("jobs").Select("COALESCE(SUM(agent_minutes), 0)").Scan(&resp.TotalAgentMinutes)

	resp.TasksByStatus = adminTaskStatusCounts(db, "")
	resp.ModelUsage, resp.ProviderUsage = adminModelUsage(db, "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// adminTaskStatusCounts counts queued_tasks by status, scoped to orgID when
// non-empty. This is the same "task" unit the platform-wide task queue widget
// counts, so a per-org breakdown stays comparable to it.
func adminTaskStatusCounts(db *gorm.DB, orgID string) map[string]int64 {
	q := db.Table("queued_tasks")
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	var counts []struct {
		Status string
		Count  int64
	}
	q.Select("status, count(*) as count").Group("status").Scan(&counts)
	out := make(map[string]int64, len(counts))
	for _, c := range counts {
		out[c.Status] = c.Count
	}
	return out
}

// adminModelUsage aggregates worker and planner spend into per-model and
// per-provider totals, scoped to orgID when non-empty (empty means
// platform-wide). It reads full rows rather than grouping on the
// model/planner_model JSON path in SQL, because that path only exists as
// Postgres jsonb in production but the test suite runs on SQLite — the same
// tradeoff buildSpend makes for the per-org /api/v1/spend breakdown.
func adminModelUsage(db *gorm.DB, orgID string) (byModel []AdminUsageRow, byProvider []AdminUsageRow) {
	type modelAgg struct {
		taskCount   int64
		costUSD     float64
		kiwiCostUSD float64
		tokensIn    int64
		tokensOut   int64
	}
	agg := map[string]*modelAgg{}
	bump := func(model, funding string, cost float64, tokensIn, tokensOut int64) {
		if model == "" {
			return
		}
		a, ok := agg[model]
		if !ok {
			a = &modelAgg{}
			agg[model] = a
		}
		a.taskCount++
		a.costUSD += cost
		if funding == store.FundingKiwi {
			a.kiwiCostUSD += cost
		}
		a.tokensIn += tokensIn
		a.tokensOut += tokensOut
	}

	var taskRows []struct {
		Spec      map[string]interface{} `gorm:"serializer:json"`
		Funding   string
		CostUSD   float64
		TokensIn  int64
		TokensOut int64
	}
	taskQuery := db.Table("queued_tasks").Select("spec, funding, cost_usd, tokens_in, tokens_out")
	if orgID != "" {
		taskQuery = taskQuery.Where("org_id = ?", orgID)
	}
	taskQuery.Find(&taskRows)
	for _, t := range taskRows {
		model, _ := t.Spec["model"].(string)
		bump(model, t.Funding, t.CostUSD, t.TokensIn, t.TokensOut)
	}

	var jobRows []struct {
		Inputs           map[string]interface{} `gorm:"serializer:json"`
		Funding          string
		PlannerCostUSD   float64
		PlannerTokensIn  int64
		PlannerTokensOut int64
	}
	jobQuery := db.Table("jobs").Select("inputs, funding, planner_cost_usd, planner_tokens_in, planner_tokens_out")
	if orgID != "" {
		jobQuery = jobQuery.Where("org_id = ?", orgID)
	}
	jobQuery.Find(&jobRows)
	for _, j := range jobRows {
		model, _ := j.Inputs["planner_model"].(string)
		bump(model, j.Funding, j.PlannerCostUSD, j.PlannerTokensIn, j.PlannerTokensOut)
	}

	providerAgg := map[string]*modelAgg{}
	byModel = make([]AdminUsageRow, 0, len(agg))
	for model, a := range agg {
		p := provider.ProviderOf(model)
		byModel = append(byModel, AdminUsageRow{
			Model: model, Provider: p, TaskCount: a.taskCount,
			CostUSD: a.costUSD, KiwiCostUSD: a.kiwiCostUSD,
			TokensIn: a.tokensIn, TokensOut: a.tokensOut,
		})
		pa, ok := providerAgg[p]
		if !ok {
			pa = &modelAgg{}
			providerAgg[p] = pa
		}
		pa.taskCount += a.taskCount
		pa.costUSD += a.costUSD
		pa.kiwiCostUSD += a.kiwiCostUSD
		pa.tokensIn += a.tokensIn
		pa.tokensOut += a.tokensOut
	}
	sort.Slice(byModel, func(i, j int) bool { return byModel[i].CostUSD > byModel[j].CostUSD })

	byProvider = make([]AdminUsageRow, 0, len(providerAgg))
	for p, a := range providerAgg {
		byProvider = append(byProvider, AdminUsageRow{
			Provider: p, TaskCount: a.taskCount,
			CostUSD: a.costUSD, KiwiCostUSD: a.kiwiCostUSD,
			TokensIn: a.tokensIn, TokensOut: a.tokensOut,
		})
	}
	sort.Slice(byProvider, func(i, j int) bool { return byProvider[i].CostUSD > byProvider[j].CostUSD })

	return byModel, byProvider
}

// AdminUserUsageRow is one user's task and cost breakdown within an org.
type AdminUserUsageRow struct {
	UserID      string  `json:"user_id"`
	Email       string  `json:"email"`
	TaskCount   int64   `json:"task_count"`
	Succeeded   int64   `json:"succeeded"`
	Failed      int64   `json:"failed"`
	CostUSD     float64 `json:"cost_usd"`
	KiwiCostUSD float64 `json:"kiwi_cost_usd"`
	TokensIn    int64   `json:"tokens_in"`
	TokensOut   int64   `json:"tokens_out"`
}

// adminPerUserUsage breaks an org's cost and task counts down by the user who
// submitted them. queued_tasks does not carry user_id itself — only the job it
// belongs to does — so tasks are attributed via their job's user_id. TaskCount
// is deliberately sourced from queued_tasks (not jobs), matching the "task"
// unit adminTaskStatusCounts and the platform-wide queue widget use; job rows
// only contribute planner cost/tokens, not an extra count.
func adminPerUserUsage(db *gorm.DB, orgID string) []AdminUserUsageRow {
	type userAgg struct {
		taskCount, succeeded, failed int64
		costUSD, kiwiCostUSD         float64
		tokensIn, tokensOut          int64
	}
	agg := map[string]*userAgg{}
	ensure := func(userID string) *userAgg {
		a, ok := agg[userID]
		if !ok {
			a = &userAgg{}
			agg[userID] = a
		}
		return a
	}

	var jobRows []struct {
		ID               string
		UserID           string
		Funding          string
		PlannerCostUSD   float64
		PlannerTokensIn  int64
		PlannerTokensOut int64
	}
	db.Table("jobs").Select("id, user_id, funding, planner_cost_usd, planner_tokens_in, planner_tokens_out").
		Where("org_id = ?", orgID).Find(&jobRows)

	jobUser := make(map[string]string, len(jobRows))
	for _, j := range jobRows {
		jobUser[j.ID] = j.UserID
		if j.UserID == "" {
			continue
		}
		a := ensure(j.UserID)
		a.costUSD += j.PlannerCostUSD
		if j.Funding == store.FundingKiwi {
			a.kiwiCostUSD += j.PlannerCostUSD
		}
		a.tokensIn += j.PlannerTokensIn
		a.tokensOut += j.PlannerTokensOut
	}

	var taskRows []struct {
		JobID     string
		Status    string
		Funding   string
		CostUSD   float64
		TokensIn  int64
		TokensOut int64
	}
	db.Table("queued_tasks").Select("job_id, status, funding, cost_usd, tokens_in, tokens_out").
		Where("org_id = ?", orgID).Find(&taskRows)

	for _, t := range taskRows {
		userID := jobUser[t.JobID]
		if userID == "" {
			continue
		}
		a := ensure(userID)
		a.taskCount++
		switch t.Status {
		case "SUCCEEDED":
			a.succeeded++
		case "FAILED":
			a.failed++
		}
		a.costUSD += t.CostUSD
		if t.Funding == store.FundingKiwi {
			a.kiwiCostUSD += t.CostUSD
		}
		a.tokensIn += t.TokensIn
		a.tokensOut += t.TokensOut
	}

	var users []User
	db.Where("org_id = ?", orgID).Find(&users)
	emailByID := make(map[string]string, len(users))
	for _, u := range users {
		emailByID[u.ID] = u.Email
	}

	out := make([]AdminUserUsageRow, 0, len(agg))
	for userID, a := range agg {
		out = append(out, AdminUserUsageRow{
			UserID: userID, Email: emailByID[userID],
			TaskCount: a.taskCount, Succeeded: a.succeeded, Failed: a.failed,
			CostUSD: a.costUSD, KiwiCostUSD: a.kiwiCostUSD,
			TokensIn: a.tokensIn, TokensOut: a.tokensOut,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CostUSD > out[j].CostUSD })
	return out
}

// handleOrgModelUsageAdmin serves GET /admin/orgs/{orgID}/model_usage: the
// same model/provider/task-status breakdown handleAdminStats reports
// platform-wide, scoped to one org, plus a per-user split of it.
func handleOrgModelUsageAdmin(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	modelUsage, providerUsage := adminModelUsage(db, orgID)

	resp := struct {
		ModelUsage    []AdminUsageRow     `json:"model_usage"`
		ProviderUsage []AdminUsageRow     `json:"provider_usage"`
		TasksByStatus map[string]int64    `json:"tasks_by_status"`
		PerUser       []AdminUserUsageRow `json:"per_user"`
	}{
		ModelUsage:    modelUsage,
		ProviderUsage: providerUsage,
		TasksByStatus: adminTaskStatusCounts(db, orgID),
		PerUser:       adminPerUserUsage(db, orgID),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleUpdateOrgPlan(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	var body struct {
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Plan == "" {
		http.Error(w, "Bad request: 'plan' is required", http.StatusBadRequest)
		return
	}

	if err := UpdateOrgPlanAndLimits(db, orgID, body.Plan); err != nil {
		http.Error(w, "Failed to update plan: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = LogAuditEvent(db, r, "UPDATE", "ORG_PLAN", orgID, fmt.Sprintf("Admin manually updated org plan to %q", body.Plan))
	w.WriteHeader(http.StatusOK)
}

func handleGrantOrgMinutes(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	var body struct {
		AgentMinutes float64 `json:"agent_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if body.AgentMinutes <= 0 {
		http.Error(w, "Bad request: 'agent_minutes' must be positive", http.StatusBadRequest)
		return
	}

	errNoLimits := errors.New("no limits row")
	errUnlimited := errors.New("unlimited")
	err := db.Transaction(func(tx *gorm.DB) error {
		var limits OrgLimits
		if err := tx.First(&limits, "org_id = ?", orgID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNoLimits
			}
			return err
		}
		// 0 means "unlimited"; adding a finite grant would silently DOWNGRADE an
		// unlimited org to a cap. Refuse rather than reduce.
		if limits.MaxAgentMinutesPerMonth == 0 {
			return errUnlimited
		}
		limits.MaxAgentMinutesPerMonth += body.AgentMinutes
		return tx.Save(&limits).Error
	})
	switch {
	case err == nil:
	case errors.Is(err, errNoLimits):
		http.Error(w, "Org has no limits row to grant against", http.StatusNotFound)
		return
	case errors.Is(err, errUnlimited):
		http.Error(w, "Org already has unlimited agent minutes", http.StatusBadRequest)
		return
	default:
		http.Error(w, "Failed to grant minutes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = LogAuditEvent(db, r, "UPDATE", "ORG_GRANT", orgID, fmt.Sprintf("Admin granted %.2f agent minutes", body.AgentMinutes))
	w.WriteHeader(http.StatusOK)
}

func handleUpdateOrgName(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		http.Error(w, "Bad request: 'name' is required", http.StatusBadRequest)
		return
	}

	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	// A targeted update, not db.Save(&org): Save rewrites every column from the
	// value read microseconds earlier, so a concurrent suspend/grant/plan-change
	// landing between the read above and the write would be silently reverted.
	if err := db.Model(&Organization{}).Where("id = ?", orgID).Update("name", name).Error; err != nil {
		// Postgres reports "duplicate key value violates unique constraint"
		// (lowercase); SQLite reports "UNIQUE constraint failed". Check both
		// cases rather than relying on GORM's TranslateError, which isn't
		// enabled anywhere in this codebase.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "unique") {
			http.Error(w, "Organization name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to rename organization", http.StatusInternalServerError)
		return
	}
	org.Name = name

	_ = LogAuditEvent(db, r, "UPDATE", "ORG_NAME", orgID, fmt.Sprintf("Renamed organization to %q", org.Name))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(org)
}

// AdminUserSearchRow is one row of the cross-tenant user directory search.
// LastActiveAt is omitted, not fabricated: User carries no activity column of
// its own (DashboardSession does, per-user, but joining it here would make
// this a different, heavier query than "search users by name/email" — add it
// as a follow-up if the frontend needs it, with its own test).
type AdminUserSearchRow struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	OrgID        string    `json:"org_id"`
	OrgName      string    `json:"org_name"`
	Role         string    `json:"role"`
	AuthProvider string    `json:"auth_provider,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// handleAdminUsersSearch serves GET /admin/users?search=&limit=&offset=,
// searching by email or name substring across every org.
func handleAdminUsersSearch(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	q := db.Model(&User{})
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("LOWER(email) LIKE LOWER(?) OR LOWER(name) LIKE LOWER(?)", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		http.Error(w, "Failed to count users", http.StatusInternalServerError)
		return
	}

	var users []User
	if err := q.Order("created_at desc").Limit(limit).Offset(offset).Find(&users).Error; err != nil {
		http.Error(w, "Failed to search users", http.StatusInternalServerError)
		return
	}

	orgIDs := make([]string, 0, len(users))
	seen := map[string]bool{}
	for _, u := range users {
		if !seen[u.OrgID] {
			seen[u.OrgID] = true
			orgIDs = append(orgIDs, u.OrgID)
		}
	}
	var orgs []Organization
	orgName := map[string]string{}
	if len(orgIDs) > 0 {
		db.Where("id IN ?", orgIDs).Find(&orgs)
		for _, o := range orgs {
			orgName[o.ID] = o.Name
		}
	}

	rows := make([]AdminUserSearchRow, len(users))
	for i, u := range users {
		provider := ""
		if u.OAuthProvider != nil {
			provider = *u.OAuthProvider
		}
		rows[i] = AdminUserSearchRow{
			ID:           u.ID,
			Email:        u.Email,
			Name:         u.Name,
			OrgID:        u.OrgID,
			OrgName:      orgName[u.OrgID],
			Role:         u.Role,
			AuthProvider: provider,
			CreatedAt:    u.CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"users": rows, "total": total})
}
