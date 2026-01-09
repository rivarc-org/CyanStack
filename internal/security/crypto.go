package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// AESCrypto provides AES-256-GCM encryption and decryption
type AESCrypto struct {
	gcm cipher.AEAD
}

// NewAESCrypto creates a new AES-GCM crypto instance
// The key must be exactly 32 bytes for AES-256
func NewAESCrypto(key []byte) (*AESCrypto, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes for AES-256, got %d bytes", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	return &AESCrypto{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM
// The nonce is prepended to the ciphertext
// Returns base64-encoded result for safe storage
func (c *AESCrypto) Encrypt(plaintext []byte) ([]byte, error) {
	// Generate a random nonce
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	// Encrypt and prepend nonce to ciphertext
	ciphertext := c.gcm.Seal(nonce, nonce, plaintext, nil)

	// Base64 encode for safe storage
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(ciphertext)))
	base64.StdEncoding.Encode(encoded, ciphertext)

	return encoded, nil
}

// Decrypt decrypts base64-encoded ciphertext using AES-256-GCM
// Expects nonce to be prepended to the ciphertext
func (c *AESCrypto) Decrypt(encoded []byte) ([]byte, error) {
	// Base64 decode
	ciphertext := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Decode(ciphertext, encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding base64: %w", err)
	}
	ciphertext = ciphertext[:n]

	nonceSize := c.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and ciphertext
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt
	plaintext, err := c.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}

	return plaintext, nil
}

// EncryptString is a convenience method for encrypting strings
func (c *AESCrypto) EncryptString(plaintext string) (string, error) {
	encrypted, err := c.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return string(encrypted), nil
}

// DecryptString is a convenience method for decrypting strings
func (c *AESCrypto) DecryptString(ciphertext string) (string, error) {
	decrypted, err := c.Decrypt([]byte(ciphertext))
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}
