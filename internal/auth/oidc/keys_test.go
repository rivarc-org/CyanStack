package oidc

import (
	"crypto"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

func TestGenerateSigningKey(t *testing.T) {
	key, err := GenerateSigningKey()
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	if key.ID == "" {
		t.Error("expected non-empty key ID")
	}
	if key.PrivateKey == nil {
		t.Error("expected non-nil private key")
	}
	if key.PublicKey == nil {
		t.Error("expected non-nil public key")
	}

	// Verify key size (should be 2048 bits = 256 bytes)
	if key.PrivateKey.Size() != 256 {
		t.Errorf("expected key size 256 bytes, got %d", key.PrivateKey.Size())
	}
}

func TestGenerateSigningKey_UniqueIDs(t *testing.T) {
	key1, _ := GenerateSigningKey()
	key2, _ := GenerateSigningKey()

	if key1.ID == key2.ID {
		t.Error("expected unique key IDs")
	}
}

func TestSigningKey_ExportPrivatePEM(t *testing.T) {
	key, _ := GenerateSigningKey()

	pem, err := key.ExportPrivatePEM()
	if err != nil {
		t.Fatalf("failed to export private PEM: %v", err)
	}

	if !strings.HasPrefix(pem, "-----BEGIN RSA PRIVATE KEY-----") {
		t.Error("expected PEM to start with RSA PRIVATE KEY header")
	}
	if !strings.HasSuffix(strings.TrimSpace(pem), "-----END RSA PRIVATE KEY-----") {
		t.Error("expected PEM to end with RSA PRIVATE KEY footer")
	}
}

func TestSigningKey_ExportPublicPEM(t *testing.T) {
	key, _ := GenerateSigningKey()

	pem, err := key.ExportPublicPEM()
	if err != nil {
		t.Fatalf("failed to export public PEM: %v", err)
	}

	if !strings.HasPrefix(pem, "-----BEGIN PUBLIC KEY-----") {
		t.Error("expected PEM to start with PUBLIC KEY header")
	}
	if !strings.HasSuffix(strings.TrimSpace(pem), "-----END PUBLIC KEY-----") {
		t.Error("expected PEM to end with PUBLIC KEY footer")
	}
}

func TestImportFromPEM_RoundTrip(t *testing.T) {
	// Generate original key
	original, _ := GenerateSigningKey()

	// Export to PEM
	privatePEM, err := original.ExportPrivatePEM()
	if err != nil {
		t.Fatalf("failed to export private PEM: %v", err)
	}

	publicPEM, err := original.ExportPublicPEM()
	if err != nil {
		t.Fatalf("failed to export public PEM: %v", err)
	}

	// Import back
	imported, err := ImportFromPEM(original.ID, privatePEM, publicPEM)
	if err != nil {
		t.Fatalf("failed to import from PEM: %v", err)
	}

	// Verify imported key matches original
	if imported.ID != original.ID {
		t.Errorf("expected ID %q, got %q", original.ID, imported.ID)
	}

	// Verify keys are mathematically equivalent by comparing modulus
	if original.PrivateKey.N.Cmp(imported.PrivateKey.N) != 0 {
		t.Error("private key modulus does not match")
	}
	if original.PublicKey.N.Cmp(imported.PublicKey.N) != 0 {
		t.Error("public key modulus does not match")
	}
}

func TestImportFromPEM_InvalidPEM(t *testing.T) {
	_, err := ImportFromPEM("key-1", "invalid-pem", "also-invalid")
	if err == nil {
		t.Error("expected error for invalid PEM")
	}
}

func TestImportFromPEM_EmptyPEM(t *testing.T) {
	_, err := ImportFromPEM("key-1", "", "")
	if err == nil {
		t.Error("expected error for empty PEM")
	}
}

func TestSigningKey_JWK(t *testing.T) {
	key, _ := GenerateSigningKey()

	jwk := key.JWK()

	if jwk.KeyID != key.ID {
		t.Errorf("expected key ID %q, got %q", key.ID, jwk.KeyID)
	}
	if jwk.Algorithm != string(jose.RS256) {
		t.Errorf("expected algorithm RS256, got %q", jwk.Algorithm)
	}
	if jwk.Use != "sig" {
		t.Errorf("expected use 'sig', got %q", jwk.Use)
	}
	if jwk.Key == nil {
		t.Error("expected non-nil key in JWK")
	}

	// Verify it's the public key
	if !jwk.IsPublic() {
		t.Error("JWK should be public")
	}
}

func TestSigningKey_SigningJWK(t *testing.T) {
	key, _ := GenerateSigningKey()

	jwk := key.SigningJWK()

	if jwk.KeyID != key.ID {
		t.Errorf("expected key ID %q, got %q", key.ID, jwk.KeyID)
	}
	if jwk.Algorithm != string(jose.RS256) {
		t.Errorf("expected algorithm RS256, got %q", jwk.Algorithm)
	}
	if jwk.Use != "sig" {
		t.Errorf("expected use 'sig', got %q", jwk.Use)
	}

	// Verify it's a private key (can sign)
	if jwk.IsPublic() {
		t.Error("SigningJWK should return private key")
	}
}

func TestSigningKey_JWK_Serialization(t *testing.T) {
	key, _ := GenerateSigningKey()

	jwk := key.JWK()

	// Verify JWK can be serialized (JSON marshaling)
	thumbprint, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		t.Fatalf("failed to compute thumbprint: %v", err)
	}

	if len(thumbprint) == 0 {
		t.Error("expected non-empty thumbprint")
	}
}
