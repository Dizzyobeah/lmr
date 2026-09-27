package keystore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kalle/local-model-router/internal/crypto"
	_ "modernc.org/sqlite"
)

var (
	// ErrKeyNotFound indicates the API key was not found.
	ErrKeyNotFound = errors.New("API key not found")
	
	// ErrKeyExists indicates an API key with this ID already exists.
	ErrKeyExists = errors.New("API key already exists")
	
	// ErrInvalidKey indicates the API key is invalid.
	ErrInvalidKey = errors.New("invalid API key")
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteStore creates a new SQLite-based key store.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	
	store := &SQLiteStore{db: db}
	
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	
	return store, nil
}

// migrate creates the necessary tables.
func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS api_keys (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		hash TEXT NOT NULL,
		scopes TEXT NOT NULL DEFAULT '["*"]',
		rate_limit INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1,
		metadata TEXT NOT NULL DEFAULT '{}',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME,
		expires_at DATETIME,
		usage_count INTEGER NOT NULL DEFAULT 0
	);
	
	CREATE INDEX IF NOT EXISTS idx_api_keys_enabled ON api_keys(enabled);
	CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON api_keys(expires_at);
	`
	
	_, err := s.db.Exec(schema)
	return err
}

// Create creates a new API key.
func (s *SQLiteStore) Create(ctx context.Context, req CreateKeyRequest) (string, *APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Generate API key
	fullKey, keyID, err := crypto.GenerateAPIKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate API key: %w", err)
	}
	
	// Hash the key
	hash, err := crypto.HashAPIKey(fullKey)
	if err != nil {
		return "", nil, fmt.Errorf("failed to hash API key: %w", err)
	}
	
	// Set defaults
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = []string{ScopeAll}
	}
	
	metadata := req.Metadata
	if metadata == nil {
		metadata = make(map[string]string)
	}
	
	// Serialize JSON fields
	scopesJSON, _ := json.Marshal(scopes)
	metadataJSON, _ := json.Marshal(metadata)
	
	now := time.Now()
	
	// Insert into database
	query := `
		INSERT INTO api_keys (id, name, hash, scopes, rate_limit, enabled, metadata, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)
	`
	
	_, err = s.db.ExecContext(ctx, query,
		keyID,
		req.Name,
		hash,
		string(scopesJSON),
		req.RateLimit,
		string(metadataJSON),
		now,
		req.ExpiresAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return "", nil, ErrKeyExists
		}
		return "", nil, fmt.Errorf("failed to insert API key: %w", err)
	}
	
	key := &APIKey{
		ID:        keyID,
		Name:      req.Name,
		Hash:      hash,
		Scopes:    scopes,
		RateLimit: req.RateLimit,
		Enabled:   true,
		Metadata:  metadata,
		CreatedAt: now,
		ExpiresAt: req.ExpiresAt,
	}
	
	return fullKey, key, nil
}

// Get retrieves an API key by ID.
func (s *SQLiteStore) Get(ctx context.Context, id string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	return s.getByID(ctx, id)
}

// getByID retrieves an API key by ID (internal, no locking).
func (s *SQLiteStore) getByID(ctx context.Context, id string) (*APIKey, error) {
	query := `
		SELECT id, name, hash, scopes, rate_limit, enabled, metadata, created_at, last_used_at, expires_at, usage_count
		FROM api_keys
		WHERE id = ?
	`
	
	var key APIKey
	var scopesJSON, metadataJSON string
	var lastUsedAt, expiresAt sql.NullTime
	
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&key.ID,
		&key.Name,
		&key.Hash,
		&scopesJSON,
		&key.RateLimit,
		&key.Enabled,
		&metadataJSON,
		&key.CreatedAt,
		&lastUsedAt,
		&expiresAt,
		&key.UsageCount,
	)
	if err == sql.ErrNoRows {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query API key: %w", err)
	}
	
	// Parse JSON fields
	json.Unmarshal([]byte(scopesJSON), &key.Scopes)
	json.Unmarshal([]byte(metadataJSON), &key.Metadata)
	
	if lastUsedAt.Valid {
		key.LastUsedAt = &lastUsedAt.Time
	}
	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	
	return &key, nil
}

// GetByHash retrieves an API key by verifying against its hash.
func (s *SQLiteStore) GetByHash(ctx context.Context, fullKey string) (*APIKey, error) {
	// Parse the key to get the ID
	keyID, err := crypto.ParseAPIKey(fullKey)
	if err != nil {
		return nil, ErrInvalidKey
	}
	
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	// Get the key by ID
	key, err := s.getByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	
	// Verify the hash
	if !crypto.VerifyAPIKey(fullKey, key.Hash) {
		return nil, ErrInvalidKey
	}
	
	return key, nil
}

// List returns all API keys.
func (s *SQLiteStore) List(ctx context.Context) ([]*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	query := `
		SELECT id, name, scopes, rate_limit, enabled, metadata, created_at, last_used_at, expires_at, usage_count
		FROM api_keys
		ORDER BY created_at DESC
	`
	
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query API keys: %w", err)
	}
	defer rows.Close()
	
	var keys []*APIKey
	for rows.Next() {
		var key APIKey
		var scopesJSON, metadataJSON string
		var lastUsedAt, expiresAt sql.NullTime
		
		err := rows.Scan(
			&key.ID,
			&key.Name,
			&scopesJSON,
			&key.RateLimit,
			&key.Enabled,
			&metadataJSON,
			&key.CreatedAt,
			&lastUsedAt,
			&expiresAt,
			&key.UsageCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan API key: %w", err)
		}
		
		json.Unmarshal([]byte(scopesJSON), &key.Scopes)
		json.Unmarshal([]byte(metadataJSON), &key.Metadata)
		
		if lastUsedAt.Valid {
			key.LastUsedAt = &lastUsedAt.Time
		}
		if expiresAt.Valid {
			key.ExpiresAt = &expiresAt.Time
		}
		
		keys = append(keys, &key)
	}
	
	return keys, rows.Err()
}

// Update updates an API key.
func (s *SQLiteStore) Update(ctx context.Context, id string, req UpdateKeyRequest) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Build update query dynamically
	var sets []string
	var args []interface{}
	
	if req.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Scopes != nil {
		scopesJSON, _ := json.Marshal(req.Scopes)
		sets = append(sets, "scopes = ?")
		args = append(args, string(scopesJSON))
	}
	if req.RateLimit != nil {
		sets = append(sets, "rate_limit = ?")
		args = append(args, *req.RateLimit)
	}
	if req.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, *req.Enabled)
	}
	if req.Metadata != nil {
		metadataJSON, _ := json.Marshal(req.Metadata)
		sets = append(sets, "metadata = ?")
		args = append(args, string(metadataJSON))
	}
	if req.ExpiresAt != nil {
		sets = append(sets, "expires_at = ?")
		args = append(args, req.ExpiresAt)
	}
	
	if len(sets) == 0 {
		return s.getByID(ctx, id)
	}
	
	args = append(args, id)
	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE id = ?", strings.Join(sets, ", "))
	
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update API key: %w", err)
	}
	
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, ErrKeyNotFound
	}
	
	return s.getByID(ctx, id)
}

// Delete deletes an API key.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	result, err := s.db.ExecContext(ctx, "DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}
	
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrKeyNotFound
	}
	
	return nil
}

// RecordUsage records that a key was used.
func (s *SQLiteStore) RecordUsage(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	query := `
		UPDATE api_keys 
		SET last_used_at = ?, usage_count = usage_count + 1 
		WHERE id = ?
	`
	
	_, err := s.db.ExecContext(ctx, query, time.Now(), id)
	return err
}

// Close closes the store.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
