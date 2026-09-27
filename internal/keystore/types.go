// Package keystore provides API key storage and management.
package keystore

import (
	"context"
	"time"
)

// APIKey represents an API key in the system.
type APIKey struct {
	// ID is the unique identifier (first 8 chars of the key).
	ID string `json:"id"`
	
	// Name is a human-readable name for the key.
	Name string `json:"name"`
	
	// Hash is the bcrypt hash of the full API key.
	Hash string `json:"-"`
	
	// Scopes defines what the key can access.
	Scopes []string `json:"scopes"`
	
	// RateLimit is the requests per minute limit (0 = unlimited).
	RateLimit int `json:"rate_limit"`
	
	// Enabled indicates if the key is active.
	Enabled bool `json:"enabled"`
	
	// Metadata contains arbitrary key-value data.
	Metadata map[string]string `json:"metadata,omitempty"`
	
	// CreatedAt is when the key was created.
	CreatedAt time.Time `json:"created_at"`
	
	// LastUsedAt is when the key was last used.
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	
	// ExpiresAt is when the key expires (nil = never).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	
	// UsageCount is the total number of requests made with this key.
	UsageCount int64 `json:"usage_count"`
}

// IsExpired returns true if the key has expired.
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*k.ExpiresAt)
}

// IsValid returns true if the key is enabled and not expired.
func (k *APIKey) IsValid() bool {
	return k.Enabled && !k.IsExpired()
}

// HasScope returns true if the key has the specified scope.
func (k *APIKey) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope || s == "*" {
			return true
		}
	}
	return false
}

// CreateKeyRequest contains the parameters for creating a new API key.
type CreateKeyRequest struct {
	// Name is a human-readable name for the key.
	Name string `json:"name"`
	
	// Scopes defines what the key can access.
	Scopes []string `json:"scopes"`
	
	// RateLimit is the requests per minute limit (0 = unlimited).
	RateLimit int `json:"rate_limit"`
	
	// Metadata contains arbitrary key-value data.
	Metadata map[string]string `json:"metadata,omitempty"`
	
	// ExpiresAt is when the key expires (nil = never).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// UpdateKeyRequest contains the parameters for updating an API key.
type UpdateKeyRequest struct {
	// Name is a human-readable name for the key.
	Name *string `json:"name,omitempty"`
	
	// Scopes defines what the key can access.
	Scopes []string `json:"scopes,omitempty"`
	
	// RateLimit is the requests per minute limit (0 = unlimited).
	RateLimit *int `json:"rate_limit,omitempty"`
	
	// Enabled indicates if the key is active.
	Enabled *bool `json:"enabled,omitempty"`
	
	// Metadata contains arbitrary key-value data.
	Metadata map[string]string `json:"metadata,omitempty"`
	
	// ExpiresAt is when the key expires (nil = never).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Store defines the interface for API key storage.
type Store interface {
	// Create creates a new API key and returns the full key (only shown once).
	Create(ctx context.Context, req CreateKeyRequest) (fullKey string, key *APIKey, err error)
	
	// Get retrieves an API key by ID.
	Get(ctx context.Context, id string) (*APIKey, error)
	
	// GetByHash retrieves an API key by verifying against its hash.
	// This is used for authentication.
	GetByHash(ctx context.Context, fullKey string) (*APIKey, error)
	
	// List returns all API keys (without hashes).
	List(ctx context.Context) ([]*APIKey, error)
	
	// Update updates an API key.
	Update(ctx context.Context, id string, req UpdateKeyRequest) (*APIKey, error)
	
	// Delete deletes an API key.
	Delete(ctx context.Context, id string) error
	
	// RecordUsage records that a key was used.
	RecordUsage(ctx context.Context, id string) error
	
	// Close closes the store and releases resources.
	Close() error
}

// Common scopes for API keys.
const (
	// ScopeAll grants access to all endpoints.
	ScopeAll = "*"
	
	// ScopeChat grants access to chat completion endpoints.
	ScopeChat = "chat"
	
	// ScopeEmbeddings grants access to embedding endpoints.
	ScopeEmbeddings = "embeddings"
	
	// ScopeModels grants access to list models.
	ScopeModels = "models"
)
