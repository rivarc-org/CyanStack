package handlers_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/testutil"
	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCryptoHandler_Encrypt_Success(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/encrypt", handler.Encrypt)

	reqBody := dto.EncryptRequest{
		Value: "my-secret-password",
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/encrypt", reqBody)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.EncryptResponse
	testutil.DecodeResponse(t, w, &resp)

	// Verify cipher starts with SECRET: prefix
	assert.True(t, strings.HasPrefix(resp.Cipher, security.SecretPrefix),
		"Cipher should start with SECRET: prefix, got: %s", resp.Cipher)

	// Verify the cipher is not empty after the prefix
	cipherPart := strings.TrimPrefix(resp.Cipher, security.SecretPrefix)
	assert.NotEmpty(t, cipherPart)
}

func TestCryptoHandler_Encrypt_EmptyValue(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/encrypt", handler.Encrypt)

	// Send empty object (missing required value)
	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/encrypt", map[string]string{})

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeValidation, resp.Code)
}

func TestCryptoHandler_Encrypt_LongValue(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/encrypt", handler.Encrypt)

	// Create a long value (1KB)
	longValue := strings.Repeat("a", 1024)
	reqBody := dto.EncryptRequest{
		Value: longValue,
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/encrypt", reqBody)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.EncryptResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.True(t, strings.HasPrefix(resp.Cipher, security.SecretPrefix))
}

func TestCryptoHandler_Decrypt_Success(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	// First encrypt a value
	originalValue := "my-secret-password"
	encrypted, err := crypto.EncryptForJSON(originalValue)
	require.NoError(t, err)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/decrypt", handler.Decrypt)

	reqBody := dto.DecryptRequest{
		Cipher: encrypted,
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/decrypt", reqBody)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.DecryptResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, originalValue, resp.Value)
}

func TestCryptoHandler_Decrypt_InvalidCipher(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/decrypt", handler.Decrypt)

	reqBody := dto.DecryptRequest{
		Cipher: "SECRET:not-valid-base64!@#$%",
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/decrypt", reqBody)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeCryptoError, resp.Code)
}

func TestCryptoHandler_Decrypt_WrongKey(t *testing.T) {
	// Encrypt with one key (exactly 32 bytes)
	key1 := []byte("12345678901234567890123456789011")
	crypto1, err := security.NewAESCrypto(key1)
	require.NoError(t, err)

	encrypted, err := crypto1.EncryptForJSON("secret")
	require.NoError(t, err)

	// Try to decrypt with a different key (exactly 32 bytes)
	key2 := []byte("12345678901234567890123456789022")
	crypto2, err := security.NewAESCrypto(key2)
	require.NoError(t, err)

	handler := handlers.NewCryptoHandler(crypto2)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/decrypt", handler.Decrypt)

	reqBody := dto.DecryptRequest{
		Cipher: encrypted,
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/decrypt", reqBody)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeCryptoError, resp.Code)
}

func TestCryptoHandler_Decrypt_NonPrefixed(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/decrypt", handler.Decrypt)

	// Send a value without SECRET: prefix (treated as plain text in dev mode)
	reqBody := dto.DecryptRequest{
		Cipher: "plain-text-value",
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/decrypt", reqBody)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.DecryptResponse
	testutil.DecodeResponse(t, w, &resp)
	// Should return value as-is
	assert.Equal(t, "plain-text-value", resp.Value)
}

func TestCryptoHandler_Decrypt_EmptyBody(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/decrypt", handler.Decrypt)

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/decrypt", map[string]string{})

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeValidation, resp.Code)
}

func TestCryptoHandler_Generate_Default(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/generate", handler.Generate)

	// Send empty body for default length
	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/generate", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.GenerateResponse
	testutil.DecodeResponse(t, w, &resp)

	// Default 32 bytes should produce ~43 character base64 string
	assert.NotEmpty(t, resp.Value)
	assert.True(t, strings.HasPrefix(resp.Cipher, security.SecretPrefix))

	// Verify it can be decoded as base64
	_, err := base64.RawURLEncoding.DecodeString(resp.Value)
	assert.NoError(t, err)
}

func TestCryptoHandler_Generate_CustomLength(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/generate", handler.Generate)

	reqBody := dto.GenerateRequest{
		Length: 16,
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/generate", reqBody)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.GenerateResponse
	testutil.DecodeResponse(t, w, &resp)

	// Verify value is valid base64
	decoded, err := base64.RawURLEncoding.DecodeString(resp.Value)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)
}

func TestCryptoHandler_Generate_MaxLength(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/generate", handler.Generate)

	// Request more than max (64)
	reqBody := dto.GenerateRequest{
		Length: 100,
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/generate", reqBody)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.GenerateResponse
	testutil.DecodeResponse(t, w, &resp)

	// Should be capped at 64 bytes
	decoded, err := base64.RawURLEncoding.DecodeString(resp.Value)
	require.NoError(t, err)
	assert.Len(t, decoded, 64, "Generated secret should be capped at 64 bytes")
}

func TestCryptoHandler_Generate_ZeroLength(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/generate", handler.Generate)

	// Request zero length should default to 32
	reqBody := dto.GenerateRequest{
		Length: 0,
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/crypto/generate", reqBody)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.GenerateResponse
	testutil.DecodeResponse(t, w, &resp)

	decoded, err := base64.RawURLEncoding.DecodeString(resp.Value)
	require.NoError(t, err)
	assert.Len(t, decoded, 32, "Zero length should default to 32 bytes")
}

func TestCryptoHandler_Roundtrip(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/encrypt", handler.Encrypt)
	router.POST("/api/v1/crypto/decrypt", handler.Decrypt)

	// Test various values
	testValues := []string{
		"simple",
		"with spaces and special chars !@#$%",
		"unicode: 日本語 🎉",
		"", // Empty string should work too with relaxed validation
	}

	for _, original := range testValues {
		if original == "" {
			continue // Skip empty - requires value in our API
		}

		t.Run("roundtrip_"+original[:min(10, len(original))], func(t *testing.T) {
			// Encrypt
			encryptReq := dto.EncryptRequest{Value: original}
			encryptResp := testutil.MakeRequest(router, "POST", "/api/v1/crypto/encrypt", encryptReq)
			require.Equal(t, http.StatusOK, encryptResp.Code)

			var encrypted dto.EncryptResponse
			testutil.DecodeResponse(t, encryptResp, &encrypted)

			// Decrypt
			decryptReq := dto.DecryptRequest{Cipher: encrypted.Cipher}
			decryptResp := testutil.MakeRequest(router, "POST", "/api/v1/crypto/decrypt", decryptReq)
			require.Equal(t, http.StatusOK, decryptResp.Code)

			var decrypted dto.DecryptResponse
			testutil.DecodeResponse(t, decryptResp, &decrypted)

			assert.Equal(t, original, decrypted.Value)
		})
	}
}

func TestCryptoHandler_Generate_Roundtrip(t *testing.T) {
	crypto := testutil.CreateTestCrypto(t)

	handler := handlers.NewCryptoHandler(crypto)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/crypto/generate", handler.Generate)
	router.POST("/api/v1/crypto/decrypt", handler.Decrypt)

	// Generate a secret
	genResp := testutil.MakeRequest(router, "POST", "/api/v1/crypto/generate", nil)
	require.Equal(t, http.StatusOK, genResp.Code)

	var generated dto.GenerateResponse
	testutil.DecodeResponse(t, genResp, &generated)

	// Decrypt the cipher and verify it matches the plain value
	decryptReq := dto.DecryptRequest{Cipher: generated.Cipher}
	decryptResp := testutil.MakeRequest(router, "POST", "/api/v1/crypto/decrypt", decryptReq)
	require.Equal(t, http.StatusOK, decryptResp.Code)

	var decrypted dto.DecryptResponse
	testutil.DecodeResponse(t, decryptResp, &decrypted)

	assert.Equal(t, generated.Value, decrypted.Value,
		"Decrypted cipher should match the plain value returned by generate")
}

func TestCryptoHandler_SecretPrefixConstant(t *testing.T) {
	// Verify the SECRET: prefix constant matches what we expect
	assert.Equal(t, "SECRET:", security.SecretPrefix)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
