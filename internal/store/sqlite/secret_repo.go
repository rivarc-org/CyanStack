package sqlite

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Crypto defines the interface for encryption operations
type Crypto interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// SecretRepository handles secret persistence with automatic encryption
type SecretRepository struct {
	db     *gorm.DB
	crypto Crypto
}

// NewSecretRepository creates a new secret repository
// If crypto is nil, secrets will be stored without encryption (not recommended for production)
func NewSecretRepository(db *DB, crypto Crypto) *SecretRepository {
	return &SecretRepository{db: db.DB, crypto: crypto}
}

// Set stores a secret, encrypting the value if crypto is configured
func (r *SecretRepository) Set(ctx context.Context, projectID, pluginID, keyName, value string) error {
	var encryptedValue string

	if r.crypto != nil {
		encrypted, err := r.crypto.Encrypt([]byte(value))
		if err != nil {
			return fmt.Errorf("encrypting secret: %w", err)
		}
		encryptedValue = string(encrypted)
	} else {
		encryptedValue = value
	}

	secret := Secret{
		ProjectID:      projectID,
		PluginID:       pluginID,
		KeyName:        keyName,
		EncryptedValue: encryptedValue,
	}

	// Upsert: insert or update on conflict
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "plugin_id"}, {Name: "key_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"encrypted_value", "updated_at"}),
	}).Create(&secret).Error; err != nil {
		return fmt.Errorf("setting secret: %w", err)
	}

	return nil
}

// Get retrieves and decrypts a secret
func (r *SecretRepository) Get(ctx context.Context, projectID, pluginID, keyName string) (string, error) {
	var secret Secret
	if err := r.db.WithContext(ctx).
		Where("project_id = ? AND plugin_id = ? AND key_name = ?", projectID, pluginID, keyName).
		First(&secret).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", fmt.Errorf("getting secret: %w", err)
	}

	if r.crypto != nil {
		decrypted, err := r.crypto.Decrypt([]byte(secret.EncryptedValue))
		if err != nil {
			return "", fmt.Errorf("decrypting secret: %w", err)
		}
		return string(decrypted), nil
	}

	return secret.EncryptedValue, nil
}

// Delete removes a secret
func (r *SecretRepository) Delete(ctx context.Context, projectID, pluginID, keyName string) error {
	if err := r.db.WithContext(ctx).
		Where("project_id = ? AND plugin_id = ? AND key_name = ?", projectID, pluginID, keyName).
		Delete(&Secret{}).Error; err != nil {
		return fmt.Errorf("deleting secret: %w", err)
	}
	return nil
}

// DeleteByPlugin removes all secrets for a plugin
func (r *SecretRepository) DeleteByPlugin(ctx context.Context, projectID, pluginID string) error {
	if err := r.db.WithContext(ctx).
		Where("project_id = ? AND plugin_id = ?", projectID, pluginID).
		Delete(&Secret{}).Error; err != nil {
		return fmt.Errorf("deleting secrets by plugin: %w", err)
	}
	return nil
}

// DeleteByProject removes all secrets for a project
func (r *SecretRepository) DeleteByProject(ctx context.Context, projectID string) error {
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Delete(&Secret{}).Error; err != nil {
		return fmt.Errorf("deleting secrets by project: %w", err)
	}
	return nil
}

// ListKeys returns all key names for a plugin
func (r *SecretRepository) ListKeys(ctx context.Context, projectID, pluginID string) ([]string, error) {
	var secrets []Secret
	if err := r.db.WithContext(ctx).
		Select("key_name").
		Where("project_id = ? AND plugin_id = ?", projectID, pluginID).
		Find(&secrets).Error; err != nil {
		return nil, fmt.Errorf("listing secret keys: %w", err)
	}

	keys := make([]string, len(secrets))
	for i, s := range secrets {
		keys[i] = s.KeyName
	}
	return keys, nil
}

// ListPlugins returns all plugin IDs that have secrets for a project
func (r *SecretRepository) ListPlugins(ctx context.Context, projectID string) ([]string, error) {
	var plugins []string
	if err := r.db.WithContext(ctx).
		Model(&Secret{}).
		Distinct("plugin_id").
		Where("project_id = ?", projectID).
		Pluck("plugin_id", &plugins).Error; err != nil {
		return nil, fmt.Errorf("listing plugins with secrets: %w", err)
	}
	return plugins, nil
}
