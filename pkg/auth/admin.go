package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/billing"
	"gorm.io/gorm"
)

// AdminRouter registers admin-only API endpoints for managing orgs, users, and keys.
// All endpoints require the caller to have the "admin" role.
func AdminRouter(db *gorm.DB, mux *http.ServeMux) {
	mux.HandleFunc("/admin/orgs", func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil || !claims.IsAdmin() {
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
		claims := ClaimsFromContext(r.Context())
		if claims == nil || !claims.IsAdmin() {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}

		// /admin/orgs/{orgID}/users[/{userID}/keys[/{keyID}]]
		path := strings.TrimPrefix(r.URL.Path, "/admin/orgs/")
		parts := strings.Split(path, "/")

		switch {
		case len(parts) == 2 && parts[1] == "usage":
			orgID := parts[0]
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handleOrgUsageAdmin(db, w, r, orgID)

		case len(parts) == 2 && parts[1] == "provider":
			orgID := parts[0]
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
			switch r.Method {
			case http.MethodPost:
				handleCreateUser(db, w, r, orgID)
			case http.MethodGet:
				handleListUsers(db, w, r, orgID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 4 && parts[1] == "users" && parts[3] == "keys":
			userID := parts[2]
			switch r.Method {
			case http.MethodPost:
				handleCreateAPIKey(db, w, r, userID)
			case http.MethodGet:
				handleListAPIKeys(db, w, r, userID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 5 && parts[1] == "users" && parts[3] == "keys":
			keyID := parts[4]
			if r.Method == http.MethodDelete {
				handleRevokeAPIKey(db, w, r, keyID)
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
		var org Organization
		if err := db.First(&org, "id = ?", claims.OrgID).Error; err == nil {
			orgName = org.Name
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"user_id":  claims.UserID,
			"email":    claims.Email,
			"name":     claims.Name,
			"org_id":   claims.OrgID,
			"org_name": orgName,
			"role":     claims.Role,
		})
	})
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
	if err := db.Create(&org).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "Organization name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create organization", http.StatusInternalServerError)
		return
	}

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

func handleCreateAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, userID string) {
	// Verify user exists.
	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
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

func handleListAPIKeys(db *gorm.DB, w http.ResponseWriter, r *http.Request, userID string) {
	var keys []APIKey
	if err := db.Where("user_id = ? AND revoked_at IS NULL", userID).Order("created_at desc").Find(&keys).Error; err != nil {
		http.Error(w, "Failed to list keys", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

func handleRevokeAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, keyID string) {
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
