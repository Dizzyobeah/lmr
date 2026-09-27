// Package crypto provides cryptographic utilities for the Local Model Router.
package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	// APIKeyPrefix is the prefix for all API keys.
	APIKeyPrefix = "lmr_"
	
	// APIKeyLength is the length of the random part of the API key (base62 encoded).
	APIKeyLength = 32
	
	// BcryptCost is the bcrypt cost factor for hashing API keys.
	BcryptCost = 12
)

var (
	// ErrInvalidAPIKey indicates the API key format is invalid.
	ErrInvalidAPIKey = errors.New("invalid API key format")
	
	// base62Alphabet is used for generating URL-safe API keys.
	base62Alphabet = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
)

// GenerateAPIKey generates a new API key in the format lmr_XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX.
// Returns the full API key (to give to user) and the key ID (first 8 chars after prefix, for lookups).
func GenerateAPIKey() (fullKey string, keyID string, err error) {
	// Generate random bytes
	randomBytes := make([]byte, APIKeyLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	
	// Encode to base62
	randomPart := make([]byte, APIKeyLength)
	for i := 0; i < APIKeyLength; i++ {
		randomPart[i] = base62Alphabet[int(randomBytes[i])%len(base62Alphabet)]
	}
	
	fullKey = APIKeyPrefix + string(randomPart)
	keyID = string(randomPart[:8])
	
	return fullKey, keyID, nil
}

// HashAPIKey creates a bcrypt hash of an API key for secure storage.
func HashAPIKey(apiKey string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(apiKey), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash API key: %w", err)
	}
	return string(hash), nil
}

// VerifyAPIKey compares an API key against a bcrypt hash.
// Returns true if they match, false otherwise.
func VerifyAPIKey(apiKey, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(apiKey))
	return err == nil
}

// ParseAPIKey validates and parses an API key.
// Returns the key ID (first 8 chars after prefix) and the full key if valid.
func ParseAPIKey(apiKey string) (keyID string, err error) {
	if !strings.HasPrefix(apiKey, APIKeyPrefix) {
		return "", ErrInvalidAPIKey
	}
	
	randomPart := strings.TrimPrefix(apiKey, APIKeyPrefix)
	if len(randomPart) != APIKeyLength {
		return "", ErrInvalidAPIKey
	}
	
	// Validate characters are in base62 alphabet
	for _, c := range randomPart {
		if !isBase62(byte(c)) {
			return "", ErrInvalidAPIKey
		}
	}
	
	return randomPart[:8], nil
}

// isBase62 checks if a byte is in the base62 alphabet.
func isBase62(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// SecureCompare performs a constant-time comparison of two strings.
// This prevents timing attacks when comparing sensitive values.
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// GenerateSecureToken generates a cryptographically secure random token.
// The token is base64 URL-encoded and has the specified number of random bytes.
func GenerateSecureToken(numBytes int) (string, error) {
	bytes := make([]byte, numBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// HashPassword hashes a password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword compares a password against a bcrypt hash.
func VerifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
