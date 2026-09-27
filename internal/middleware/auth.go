// Package middleware provides HTTP middleware for the Local Model Router.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/kalle/local-model-router/internal/keystore"
	"github.com/kalle/local-model-router/pkg/openai"
)

// contextKey is used for context values.
type contextKey string

const (
	// APIKeyContextKey is the context key for the authenticated API key.
	APIKeyContextKey contextKey = "api_key"
)

// Auth creates authentication middleware.
type Auth struct {
	store   keystore.Store
	enabled bool
}

// NewAuth creates a new Auth middleware.
func NewAuth(store keystore.Store, enabled bool) *Auth {
	return &Auth{
		store:   store,
		enabled: enabled,
	}
}

// Middleware returns the authentication middleware handler.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if disabled
		if !a.enabled {
			next.ServeHTTP(w, r)
			return
		}
		
		// Extract API key from Authorization header
		apiKey := extractAPIKey(r)
		if apiKey == "" {
			openai.WriteError(w, openai.ErrUnauthorized("Missing API key. Provide it via Authorization: Bearer <key>"))
			return
		}
		
		// Validate API key
		key, err := a.store.GetByHash(r.Context(), apiKey)
		if err != nil {
			if err == keystore.ErrKeyNotFound || err == keystore.ErrInvalidKey {
				openai.WriteError(w, openai.ErrUnauthorized("Invalid API key"))
				return
			}
			openai.WriteError(w, openai.ErrInternal("Failed to validate API key"))
			return
		}
		
		// Check if key is valid (enabled and not expired)
		if !key.IsValid() {
			if !key.Enabled {
				openai.WriteError(w, openai.ErrUnauthorized("API key is disabled"))
				return
			}
			if key.IsExpired() {
				openai.WriteError(w, openai.ErrUnauthorized("API key has expired"))
				return
			}
		}
		
		// Record usage (async to not block request)
		go a.store.RecordUsage(context.Background(), key.ID)
		
		// Add key to context
		ctx := context.WithValue(r.Context(), APIKeyContextKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireScope creates middleware that checks for a specific scope.
func (a *Auth) RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip scope check if auth is disabled
			if !a.enabled {
				next.ServeHTTP(w, r)
				return
			}
			
			key := GetAPIKey(r.Context())
			if key == nil {
				openai.WriteError(w, openai.ErrUnauthorized("Authentication required"))
				return
			}
			
			if !key.HasScope(scope) {
				openai.WriteError(w, openai.ErrForbidden("Insufficient permissions"))
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
}

// extractAPIKey extracts the API key from the request.
// Supports Authorization: Bearer <key> and x-api-key header.
func extractAPIKey(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get("Authorization")
	if auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}
	
	// Check x-api-key header (common alternative)
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}
	
	return ""
}

// GetAPIKey retrieves the authenticated API key from the context.
func GetAPIKey(ctx context.Context) *keystore.APIKey {
	key, ok := ctx.Value(APIKeyContextKey).(*keystore.APIKey)
	if !ok {
		return nil
	}
	return key
}
