package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
)

// contextKey is a private type to prevent context key collisions.
type contextKey int

const claimsKey contextKey = 0

// UserClaims represents the identity extracted from a valid API key.
type UserClaims struct {
	UserID string
	Email  string
	Name   string
	OrgID  string
	Role   string // "admin" or "member"
}

// IsAdmin returns true if the user has the admin role.
func (c *UserClaims) IsAdmin() bool {
	return c.Role == "admin"
}

// ClaimsFromContext retrieves the UserClaims from the request context,
// or nil if no claims are present.
func ClaimsFromContext(ctx context.Context) *UserClaims {
	claims, _ := ctx.Value(claimsKey).(*UserClaims)
	return claims
}

// ContextWithClaims returns a new context with the given claims attached.
func ContextWithClaims(ctx context.Context, claims *UserClaims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// AuthMiddleware validates the Authorization: Bearer <token> header and injects
// UserClaims into the request context. It supports two token types:
//
//  1. Bootstrap admin token: the static KIWI_SERVER_TOKEN env var. When matched,
//     it injects admin claims with a reserved system user ID. This allows initial
//     setup (creating the first org, user, and API keys) before any user exists.
//
//  2. User API keys: looked up via SHA-256 hash in the api_keys table, mapped to
//     a user and org. The key must not be expired or revoked.
func AuthMiddleware(db *gorm.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exclude public paths from auth middleware so browser can load dashboard directly.
		if r.URL.Path == "/" || r.URL.Path == "/dashboard" {
			next.ServeHTTP(w, r)
			return
		}

		token := extractBearerToken(r)
		if token == "" {
			// check for session cookie
			cookie, err := r.Cookie(SessionCookieName)
			if err == nil && cookie.Value != "" {
				sess, err := VerifySession(cookie.Value)
				if err == nil {
					var user User
					if err := db.First(&user, "id = ?", sess.UserID).Error; err == nil {
						claims := &UserClaims{
							UserID: user.ID,
							Email:  user.Email,
							Name:   user.Name,
							OrgID:  user.OrgID,
							Role:   user.Role,
						}
						next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
						return
					}
				}
			}

			http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
			return
		}

		// 1. Check bootstrap admin token first.
		bootstrapToken := os.Getenv("KIWI_SERVER_TOKEN")
		if bootstrapToken != "" && constantTimeEqual(token, bootstrapToken) {
			claims := &UserClaims{
				UserID: "system",
				Email:  "system@kiwi.local",
				Name:   "System Admin",
				OrgID:  "system",
				Role:   "admin",
			}
			next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
			return
		}

		// 2. Look up user API key by hash.
		keyHash := hashToken(token)
		var apiKey APIKey
		if err := db.Where("key_hash = ?", keyHash).First(&apiKey).Error; err != nil {
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}

		if apiKey.IsRevoked() {
			http.Error(w, "Unauthorized: token has been revoked", http.StatusUnauthorized)
			return
		}
		if apiKey.IsExpired() {
			http.Error(w, "Unauthorized: token has expired", http.StatusUnauthorized)
			return
		}

		// Resolve the user and org.
		var user User
		if err := db.First(&user, "id = ?", apiKey.UserID).Error; err != nil {
			http.Error(w, "Unauthorized: user not found", http.StatusUnauthorized)
			return
		}

		claims := &UserClaims{
			UserID: user.ID,
			Email:  user.Email,
			Name:   user.Name,
			OrgID:  user.OrgID,
			Role:   user.Role,
		}
		next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
	})
}

// AuthFunc is a simpler auth function that returns claims or an error,
// suitable for use in individual handlers without full middleware wrapping.
func AuthFunc(db *gorm.DB, r *http.Request) (*UserClaims, error) {
	token := extractBearerToken(r)
	if token == "" {
		cookie, err := r.Cookie(SessionCookieName)
		if err == nil && cookie.Value != "" {
			sess, err := VerifySession(cookie.Value)
			if err == nil {
				var user User
				if err := db.First(&user, "id = ?", sess.UserID).Error; err == nil {
					return &UserClaims{
						UserID: user.ID,
						Email:  user.Email,
						Name:   user.Name,
						OrgID:  user.OrgID,
						Role:   user.Role,
					}, nil
				}
			}
		}
		return nil, fmt.Errorf("missing Authorization header")
	}

	// Check bootstrap admin token.
	bootstrapToken := os.Getenv("KIWI_SERVER_TOKEN")
	if bootstrapToken != "" && constantTimeEqual(token, bootstrapToken) {
		return &UserClaims{
			UserID: "system",
			Email:  "system@kiwi.local",
			Name:   "System Admin",
			OrgID:  "system",
			Role:   "admin",
		}, nil
	}

	// Look up user API key by hash.
	keyHash := hashToken(token)
	var apiKey APIKey
	if err := db.Where("key_hash = ?", keyHash).First(&apiKey).Error; err != nil {
		return nil, fmt.Errorf("invalid token")
	}
	if apiKey.IsRevoked() {
		return nil, fmt.Errorf("token has been revoked")
	}
	if apiKey.IsExpired() {
		return nil, fmt.Errorf("token has expired")
	}

	var user User
	if err := db.First(&user, "id = ?", apiKey.UserID).Error; err != nil {
		return nil, fmt.Errorf("user not found")
	}

	return &UserClaims{
		UserID: user.ID,
		Email:  user.Email,
		Name:   user.Name,
		OrgID:  user.OrgID,
		Role:   user.Role,
	}, nil
}

// extractBearerToken extracts the token from the Authorization header.
func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// hashToken produces a SHA-256 hex digest of the given plaintext token.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// constantTimeEqual performs a constant-time comparison to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// GenerateAPIKey creates a new API key for a user. It returns the plaintext key
// (shown to the user once) and the APIKey model (persisted with the hash).
func GenerateAPIKey(userID, label string, expiresAt *time.Time) (plaintext string, key *APIKey, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("failed to generate random key: %w", err)
	}
	plaintext = "kiwi_" + hex.EncodeToString(raw) // 69-char key with prefix

	id := make([]byte, 4)
	if _, err := rand.Read(id); err != nil {
		return "", nil, fmt.Errorf("failed to generate key ID: %w", err)
	}

	key = &APIKey{
		ID:        hex.EncodeToString(id),
		KeyHash:   hashToken(plaintext),
		UserID:    userID,
		Label:     label,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}
	return plaintext, key, nil
}
