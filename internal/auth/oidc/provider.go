package oidc

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/text/language"
	"gorm.io/gorm"

	"github.com/cyanstack/cyanstack/internal/store/sqlite"
)

// ProviderConfig configures the OIDC provider
type ProviderConfig struct {
	Issuer        string
	DB            *gorm.DB
	Storage       *Storage
	AllowInsecure bool // Allow HTTP for development/testing
}

// Provider wraps the zitadel/oidc provider
type Provider struct {
	op      op.OpenIDProvider
	storage *Storage
	issuer  string
}

// NewProvider creates a new OIDC provider
func NewProvider(cfg ProviderConfig) (*Provider, error) {
	// Create provider configuration
	providerConfig := &op.Config{
		CryptoKey:                generateCryptoKey(),
		DefaultLogoutRedirectURI: cfg.Issuer,
		CodeMethodS256:           true,
		AuthMethodPost:           true,
		GrantTypeRefreshToken:    true,
		RequestObjectSupported:   false,
		SupportedUILocales:       []language.Tag{language.English},
	}

	// Build provider options
	var opts []op.Option
	if cfg.AllowInsecure {
		opts = append(opts, op.WithAllowInsecure())
	}

	// Create the provider
	// The issuer is set via the third argument as a static issuer function
	provider, err := op.NewProvider(providerConfig, cfg.Storage, op.StaticIssuer(cfg.Issuer), opts...)
	if err != nil {
		return nil, fmt.Errorf("creating OIDC provider: %w", err)
	}

	return &Provider{
		op:      provider,
		storage: cfg.Storage,
		issuer:  cfg.Issuer,
	}, nil
}

// Handler returns an http.Handler for the OIDC endpoints
func (p *Provider) Handler() http.Handler {
	return p.op
}

// Storage returns the underlying storage
func (p *Provider) Storage() *Storage {
	return p.storage
}

// Issuer returns the issuer URL
func (p *Provider) Issuer() string {
	return p.issuer
}

// RegisterClient dynamically registers a new OIDC client
func (p *Provider) RegisterClient(ctx context.Context, reg ClientRegistration) (*ClientCredentials, error) {
	return p.storage.RegisterClient(ctx, reg)
}

// GetClientByNodeID retrieves a client by project and node ID
func (p *Provider) GetClientByNodeID(ctx context.Context, projectID, nodeID string) (*ClientCredentials, error) {
	return p.storage.GetClientByNodeID(ctx, projectID, nodeID)
}

// CompleteAuthRequest marks an authorization request as authenticated
func (p *Provider) CompleteAuthRequest(ctx context.Context, authRequestID, userID string) error {
	return p.storage.CompleteAuthRequest(ctx, authRequestID, userID)
}

// CreateSession creates a new session for a user
func (p *Provider) CreateSession(ctx context.Context, sessionID, userID string, expiresAt time.Time) error {
	return p.storage.CreateSession(ctx, sessionID, userID, expiresAt)
}

// GetSession retrieves a session by ID
func (p *Provider) GetSession(ctx context.Context, sessionID string) (*sqlite.OIDCSession, error) {
	return p.storage.GetSession(ctx, sessionID)
}

// DeleteSession removes a session
func (p *Provider) DeleteSession(ctx context.Context, sessionID string) error {
	return p.storage.DeleteSession(ctx, sessionID)
}

// generateCryptoKey generates a 32-byte key for cookie encryption
func generateCryptoKey() [32]byte {
	// In production, this should be persisted
	// For now, we use a static key for development
	var key [32]byte
	copy(key[:], "cyanstack-oidc-provider-key-dev!")
	return key
}

// EnsureSigningKey ensures a signing key exists in the database, creating one if needed
func EnsureSigningKey(ctx context.Context, db *gorm.DB) (*SigningKey, error) {
	var keyModel sqlite.OIDCSigningKey
	err := db.WithContext(ctx).First(&keyModel, "is_active = ?", true).Error

	if err == nil {
		// Key exists, import it
		return ImportFromPEM(keyModel.ID, keyModel.PrivateKeyPEM, keyModel.PublicKeyPEM)
	}

	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("checking for signing key: %w", err)
	}

	// Generate new key
	key, err := GenerateSigningKey()
	if err != nil {
		return nil, fmt.Errorf("generating signing key: %w", err)
	}

	privatePEM, err := key.ExportPrivatePEM()
	if err != nil {
		return nil, fmt.Errorf("exporting private key: %w", err)
	}

	publicPEM, err := key.ExportPublicPEM()
	if err != nil {
		return nil, fmt.Errorf("exporting public key: %w", err)
	}

	// Store in database
	keyModel = sqlite.OIDCSigningKey{
		ID:            key.ID,
		PrivateKeyPEM: privatePEM,
		PublicKeyPEM:  publicPEM,
		Algorithm:     "RS256",
		IsActive:      true,
	}

	if err := db.WithContext(ctx).Create(&keyModel).Error; err != nil {
		return nil, fmt.Errorf("storing signing key: %w", err)
	}

	return key, nil
}
