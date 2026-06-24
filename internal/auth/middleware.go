package auth

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

const (
	contextKeyUserID contextKey = iota
	contextKeyOrgID
	contextKeyEmail
	contextKeyRole
)

// SessionLookupFn is a function that looks up a session by token hash.
// Returns userID, orgID, email, role, expiresAt, and whether the session exists.
type SessionLookupFn func(tokenHash string) (userID, orgID, email, role string, expiresAt time.Time, ok bool)

// RequireAuth returns a chi middleware that validates Bearer tokens.
// On success it injects user/org info into the request context.
// On failure it returns 401.
func RequireAuth(lookup SessionLookupFn) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("Authorization")
			if raw == "" {
				http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(raw, "Bearer ")
			if token == raw || token == "" {
				http.Error(w, `{"error":"invalid Authorization header format"}`, http.StatusUnauthorized)
				return
			}
			hash := HashToken(token)
			userID, orgID, email, role, expiresAt, ok := lookup(hash)
			if !ok || time.Now().After(expiresAt) {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}
			ctx := r.Context()
			ctx = context.WithValue(ctx, contextKeyUserID, userID)
			ctx = context.WithValue(ctx, contextKeyOrgID, orgID)
			ctx = context.WithValue(ctx, contextKeyEmail, email)
			ctx = context.WithValue(ctx, contextKeyRole, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts the authenticated user ID from context.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyUserID).(string)
	return v
}

// OrgIDFromContext extracts the authenticated org ID from context.
func OrgIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyOrgID).(string)
	return v
}

// EmailFromContext extracts the authenticated user email from context.
func EmailFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyEmail).(string)
	return v
}

// RoleFromContext extracts the authenticated user role from context.
func RoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyRole).(string)
	return v
}
