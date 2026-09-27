// Package crypto provides cryptographic utilities for the Local Model Router.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrInvalidKeyLength indicates the encryption key has an invalid length.
	ErrInvalidKeyLength = errors.New("encryption key must be 32 bytes (64 hex characters)")
	
	// ErrDecryptionFailed indicates decryption failed (wrong key or corrupted data).
	ErrDecryptionFailed = errors.New("decryption failed: invalid ciphertext or wrong key")
	
	// ErrInvalidCiphertext indicates the ciphertext is too short or malformed.
	ErrInvalidCiphertext = errors.New("invalid ciphertext: too short or malformed")
)

// Encryptor provides AES-256-GCM encryption/decryption.
type Encryptor struct {
	aead cipher.AEAD
}

// NewEncryptor creates a new Encryptor with the given hex-encoded key.
// The key must be 64 hex characters (32 bytes).
func NewEncryptor(hexKey string) (*Encryptor, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex key: %w", err)
	}
	
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}
	
	return NewEncryptorFromBytes(key)
}

// NewEncryptorFromBytes creates a new Encryptor with the given raw key bytes.
// The key must be exactly 32 bytes for AES-256.
func NewEncryptorFromBytes(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}
	
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	
	return &Encryptor{aead: aead}, nil
}

// Encrypt encrypts plaintext and returns hex-encoded ciphertext.
// The ciphertext includes a random nonce prepended to the encrypted data.
func (e *Encryptor) Encrypt(plaintext []byte) (string, error) {
	// Generate random nonce
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	
	// Encrypt and prepend nonce
	ciphertext := e.aead.Seal(nonce, nonce, plaintext, nil)
	
	return hex.EncodeToString(ciphertext), nil
}

// EncryptString encrypts a string and returns hex-encoded ciphertext.
func (e *Encryptor) EncryptString(plaintext string) (string, error) {
	return e.Encrypt([]byte(plaintext))
}

// Decrypt decrypts hex-encoded ciphertext and returns plaintext.
func (e *Encryptor) Decrypt(hexCiphertext string) ([]byte, error) {
	ciphertext, err := hex.DecodeString(hexCiphertext)
	if err != nil {
		return nil, fmt.Errorf("invalid hex ciphertext: %w", err)
	}
	
	nonceSize := e.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidCiphertext
	}
	
	nonce, ciphertextData := ciphertext[:nonceSize], ciphertext[nonceSize:]
	
	plaintext, err := e.aead.Open(nil, nonce, ciphertextData, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	
	return plaintext, nil
}

// DecryptString decrypts hex-encoded ciphertext and returns a string.
func (e *Encryptor) DecryptString(hexCiphertext string) (string, error) {
	plaintext, err := e.Decrypt(hexCiphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// GenerateKey generates a new random 32-byte encryption key and returns it hex-encoded.
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}
	return hex.EncodeToString(key), nil
}
