package dto

// EncryptRequest represents a request to encrypt a value
type EncryptRequest struct {
	Value string `json:"value" binding:"required"`
}

// EncryptResponse represents the encrypted value
type EncryptResponse struct {
	Cipher string `json:"cipher"` // SECRET:{base64}
}

// DecryptRequest represents a request to decrypt a value
type DecryptRequest struct {
	Cipher string `json:"cipher" binding:"required"`
}

// DecryptResponse represents the decrypted value
type DecryptResponse struct {
	Value string `json:"value"`
}

// GenerateRequest represents a request to generate a secret
type GenerateRequest struct {
	Length int `json:"length,omitempty"` // Optional, defaults to 32
}

// GenerateResponse represents a generated secret
type GenerateResponse struct {
	Value  string `json:"value"`  // Plain text value
	Cipher string `json:"cipher"` // SECRET:{base64}
}
