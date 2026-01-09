package security

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// KeySize is the size of the AES-256 key in bytes
	KeySize = 32
	// EnvMasterKey is the environment variable name for the master key
	EnvMasterKey = "CYAN_MASTER_KEY"
	// DefaultKeyFile is the default key file name
	DefaultKeyFile = "key"
	// DefaultConfigDir is the default config directory
	DefaultConfigDir = ".cyanstack"
)

// KeyStore manages the master encryption key
type KeyStore struct {
	keyPath string
}

// NewKeyStore creates a new key store
// If keyPath is empty, uses the default location ~/.cyanstack/key
func NewKeyStore(keyPath string) *KeyStore {
	if keyPath == "" {
		homeDir, _ := os.UserHomeDir()
		keyPath = filepath.Join(homeDir, DefaultConfigDir, DefaultKeyFile)
	}
	return &KeyStore{keyPath: keyPath}
}

// GetKey retrieves the master key from environment or file
// If the key doesn't exist, it will be generated and stored
func (k *KeyStore) GetKey() ([]byte, error) {
	// First, check environment variable
	if envKey := os.Getenv(EnvMasterKey); envKey != "" {
		key, err := hex.DecodeString(envKey)
		if err != nil {
			return nil, fmt.Errorf("invalid CYAN_MASTER_KEY: must be hex-encoded: %w", err)
		}
		if len(key) != KeySize {
			return nil, fmt.Errorf("invalid CYAN_MASTER_KEY: must be %d bytes (64 hex chars), got %d bytes", KeySize, len(key))
		}
		return key, nil
	}

	// Check if key file exists
	if data, err := os.ReadFile(k.keyPath); err == nil {
		key, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("invalid key file: must be hex-encoded: %w", err)
		}
		if len(key) != KeySize {
			return nil, fmt.Errorf("invalid key file: must be %d bytes, got %d bytes", KeySize, len(key))
		}
		return key, nil
	}

	// Generate and store new key
	key, err := k.generateAndStoreKey()
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}

	return key, nil
}

// generateAndStoreKey generates a new random key and stores it in the key file
func (k *KeyStore) generateAndStoreKey() ([]byte, error) {
	// Generate random key
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating random key: %w", err)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(k.keyPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating key directory: %w", err)
	}

	// Write key file with restricted permissions (0600 = owner read/write only)
	hexKey := hex.EncodeToString(key)
	if err := os.WriteFile(k.keyPath, []byte(hexKey), 0600); err != nil {
		return nil, fmt.Errorf("writing key file: %w", err)
	}

	return key, nil
}

// KeyPath returns the path to the key file
func (k *KeyStore) KeyPath() string {
	return k.keyPath
}

// KeyExists checks if a key exists (either in env or file)
func (k *KeyStore) KeyExists() bool {
	if os.Getenv(EnvMasterKey) != "" {
		return true
	}
	_, err := os.Stat(k.keyPath)
	return err == nil
}

// NewCrypto creates a new AESCrypto instance using the key from this keystore
func (k *KeyStore) NewCrypto() (*AESCrypto, error) {
	key, err := k.GetKey()
	if err != nil {
		return nil, err
	}
	return NewAESCrypto(key)
}

// DefaultCrypto creates a new AESCrypto instance using the default keystore
func DefaultCrypto() (*AESCrypto, error) {
	ks := NewKeyStore("")
	return ks.NewCrypto()
}
