package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ajah/core/internal/db"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ── Auth helpers ──────────────────────────────────────────────────────────────

func authGenerateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func authGenerateToken() (token, tokenHash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	token = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(token))
	tokenHash = hex.EncodeToString(sum[:])
	return token, tokenHash, nil
}

func authHashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func authHashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func authCheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func authCreateSession(ctx context.Context, store *db.Store, userID, orgID string, logger *zap.Logger) (token string, expiresAt time.Time, err error) {
	var tokenHash string
	token, tokenHash, err = authGenerateToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(30 * 24 * time.Hour)
	sess := db.AuthSession{
		ID:        authGenerateID(),
		UserID:    userID,
		OrgID:     orgID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}
	if err = store.CreateAuthSession(ctx, sess); err != nil {
		logger.Error("create auth session", zap.Error(err))
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// ── Auth handlers ─────────────────────────────────────────────────────────────

// registerHandler — POST /auth/register
// Creates a new org + owner user and returns a session token.
func registerHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrgName  string `json:"org_name"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		req.OrgName = strings.TrimSpace(req.OrgName)
		if req.OrgName == "" || req.Email == "" || req.Password == "" {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"org_name, email, and password are required"}`, http.StatusBadRequest)
			return
		}
		if len(req.Password) < 8 {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
			return
		}

		slug := strings.ToLower(strings.ReplaceAll(req.OrgName, " ", "-"))
		existing, err := store.OrgBySlug(r.Context(), slug)
		if err != nil {
			logger.Error("check org slug", zap.Error(err))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if existing != nil {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"organisation name already taken"}`, http.StatusConflict)
			return
		}

		existingUser, err := store.UserByEmail(r.Context(), req.Email)
		if err != nil {
			logger.Error("check email", zap.Error(err))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if existingUser != nil {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"email already registered"}`, http.StatusConflict)
			return
		}

		hash, err := authHashPassword(req.Password)
		if err != nil {
			logger.Error("hash password", zap.Error(err))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		orgID := authGenerateID()
		if err := store.CreateOrg(r.Context(), db.Org{ID: orgID, Name: req.OrgName, Slug: slug}); err != nil {
			logger.Error("create org", zap.Error(err))
			http.Error(w, `{"error":"failed to create organisation"}`, http.StatusInternalServerError)
			return
		}

		userID := authGenerateID()
		if err := store.CreateUser(r.Context(), db.User{
			ID: userID, OrgID: orgID, Email: req.Email, PasswordHash: hash, Role: "owner",
		}); err != nil {
			logger.Error("create user", zap.Error(err))
			http.Error(w, `{"error":"failed to create user"}`, http.StatusInternalServerError)
			return
		}

		token, expiresAt, err := authCreateSession(r.Context(), store, userID, orgID, logger)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      token,
			"expires_at": expiresAt,
			"user":       map[string]interface{}{"id": userID, "email": req.Email, "role": "owner"},
			"org":        map[string]interface{}{"id": orgID, "name": req.OrgName, "slug": slug},
		})
	}
}

// loginHandler — POST /auth/login
func loginHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		if req.Email == "" || req.Password == "" {
			http.Error(w, `{"error":"email and password are required"}`, http.StatusBadRequest)
			return
		}

		user, err := store.UserByEmail(r.Context(), req.Email)
		if err != nil {
			logger.Error("lookup user", zap.Error(err))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		// Same error for wrong email and wrong password — prevent enumeration
		if user == nil || authCheckPassword(user.PasswordHash, req.Password) != nil {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"invalid email or password"}`, http.StatusUnauthorized)
			return
		}

		token, expiresAt, err := authCreateSession(r.Context(), store, user.ID, user.OrgID, logger)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      token,
			"expires_at": expiresAt,
			"user":       map[string]interface{}{"id": user.ID, "email": user.Email, "role": user.Role},
			"org_id":     user.OrgID,
		})
	}
}

// logoutHandler — POST /auth/logout
func logoutHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Authorization")
		token := strings.TrimPrefix(raw, "Bearer ")
		if token != "" && token != raw {
			tokenHash := authHashToken(token)
			if err := store.DeleteAuthSession(r.Context(), tokenHash); err != nil {
				logger.Warn("delete session", zap.Error(err))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// meHandler — GET /auth/me
// When AUTH_ENABLED=false, returns a synthetic anonymous user so the dashboard
// can detect auth-disabled mode without a token.
func meHandler(store *db.Store, logger *zap.Logger, authEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth disabled — return anonymous user, dashboard skips login
		if !authEnabled {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Ajah-Auth-Disabled", "true")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth_enabled": false,
				"user": map[string]interface{}{
					"id":    "anonymous",
					"email": "self-hosted",
					"role":  "owner",
				},
				"org": map[string]interface{}{
					"id":   "local",
					"name": "Self-hosted",
					"slug": "self-hosted",
				},
			})
			return
		}

		raw := r.Header.Get("Authorization")
		token := strings.TrimPrefix(raw, "Bearer ")
		if token == "" || token == raw {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		tokenHash := authHashToken(token)
		sess, user, err := store.SessionByTokenHash(r.Context(), tokenHash)
		if err != nil {
			logger.Error("lookup session", zap.Error(err))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if sess == nil || time.Now().After(sess.ExpiresAt) {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
			return
		}
		org, err := store.OrgByID(r.Context(), user.OrgID)
		if err != nil || org == nil {
			http.Error(w, `{"error":"org not found"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"auth_enabled": true,
			"user": map[string]interface{}{"id": user.ID, "email": user.Email, "role": user.Role},
			"org":  map[string]interface{}{"id": org.ID, "name": org.Name, "slug": org.Slug},
		})
	}
}

// listTeamHandler — GET /auth/team
func listTeamHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.Header.Get("X-Ajah-Org-ID")
		if orgID == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		users, err := store.ListOrgUsers(r.Context(), orgID)
		if err != nil {
			logger.Error("list org users", zap.Error(err))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		type safeUser struct {
			ID        string    `json:"id"`
			Email     string    `json:"email"`
			Role      string    `json:"role"`
			CreatedAt time.Time `json:"created_at"`
		}
		safe := make([]safeUser, len(users))
		for i, u := range users {
			safe[i] = safeUser{ID: u.ID, Email: u.Email, Role: u.Role, CreatedAt: u.CreatedAt}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"users": safe})
	}
}
