package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/gin-gonic/gin"
)

// CryptoHandler handles cryptography-related API requests
type CryptoHandler struct {
	crypto *security.AESCrypto
}

// NewCryptoHandler creates a new CryptoHandler
func NewCryptoHandler(crypto *security.AESCrypto) *CryptoHandler {
	return &CryptoHandler{
		crypto: crypto,
	}
}

// Encrypt encrypts a value and returns it with the SECRET: prefix
func (h *CryptoHandler) Encrypt(c *gin.Context) {
	var req dto.EncryptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(
			"Invalid request body",
			dto.ErrCodeValidation,
		))
		return
	}

	cipher, err := h.crypto.EncryptForJSON(req.Value)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to encrypt value",
			dto.ErrCodeCryptoError,
		))
		return
	}

	c.JSON(http.StatusOK, dto.EncryptResponse{
		Cipher: cipher,
	})
}

// Decrypt decrypts a SECRET:-prefixed value
func (h *CryptoHandler) Decrypt(c *gin.Context) {
	var req dto.DecryptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(
			"Invalid request body",
			dto.ErrCodeValidation,
		))
		return
	}

	value, err := h.crypto.DecryptFromJSON(req.Cipher)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(
			"Failed to decrypt value - invalid cipher or wrong key",
			dto.ErrCodeCryptoError,
		))
		return
	}

	c.JSON(http.StatusOK, dto.DecryptResponse{
		Value: value,
	})
}

// Generate generates a cryptographically strong random secret
func (h *CryptoHandler) Generate(c *gin.Context) {
	var req dto.GenerateRequest
	// Allow empty body with default length
	_ = c.ShouldBindJSON(&req)

	// Default to 32 bytes if not specified
	length := req.Length
	if length <= 0 {
		length = 32
	}

	// Cap at 64 bytes for security and practicality
	if length > 64 {
		length = 64
	}

	// Generate random bytes
	randomBytes := make([]byte, length)
	if _, err := rand.Read(randomBytes); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to generate random bytes",
			dto.ErrCodeInternal,
		))
		return
	}

	// Encode as URL-safe base64 for use in environment variables
	plainValue := base64.RawURLEncoding.EncodeToString(randomBytes)

	// Encrypt the generated value
	cipher, err := h.crypto.EncryptForJSON(plainValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to encrypt generated secret",
			dto.ErrCodeCryptoError,
		))
		return
	}

	c.JSON(http.StatusOK, dto.GenerateResponse{
		Value:  plainValue,
		Cipher: cipher,
	})
}
