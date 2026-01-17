package oidc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	sqlitemodels "github.com/cyanstack/cyanstack/internal/store/sqlite"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Run migrations for OIDC tables
	err = db.AutoMigrate(
		&sqlitemodels.OIDCUser{},
		&sqlitemodels.OIDCClient{},
		&sqlitemodels.OIDCSession{},
		&sqlitemodels.OIDCSigningKey{},
		&sqlitemodels.OIDCAuthRequest{},
		&sqlitemodels.OIDCAuthCode{},
		&sqlitemodels.OIDCToken{},
		&sqlitemodels.OIDCRefreshToken{},
		&sqlitemodels.Project{},
	)
	if err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return db
}

func TestStorage_RegisterClient(t *testing.T) {
	db := setupTestDB(t)
	signingKey, err := GenerateSigningKey()
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Register a client
	reg := ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	}

	creds, err := storage.RegisterClient(ctx, reg)
	if err != nil {
		t.Fatalf("failed to register client: %v", err)
	}

	if creds.ClientID == "" {
		t.Error("expected non-empty client ID")
	}
	if creds.ClientSecret == "" {
		t.Error("expected non-empty client secret")
	}
	if len(creds.ClientSecret) != 32 {
		t.Errorf("expected client secret length 32, got %d", len(creds.ClientSecret))
	}

	// Verify client was stored with bcrypt-hashed secret
	var storedClient sqlitemodels.OIDCClient
	if err := db.First(&storedClient, "id = ?", creds.ClientID).Error; err != nil {
		t.Fatalf("failed to retrieve stored client: %v", err)
	}

	if storedClient.Name != reg.Name {
		t.Errorf("expected name %q, got %q", reg.Name, storedClient.Name)
	}
	if storedClient.NodeID != reg.NodeID {
		t.Errorf("expected node ID %q, got %q", reg.NodeID, storedClient.NodeID)
	}
	if storedClient.ProjectID != reg.ProjectID {
		t.Errorf("expected project ID %q, got %q", reg.ProjectID, storedClient.ProjectID)
	}

	// Verify secret hash is not the plain secret
	if storedClient.SecretHash == creds.ClientSecret {
		t.Error("secret should be hashed, not stored in plain text")
	}
}

func TestStorage_GetClientByClientID(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Register a client first
	creds, err := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})
	if err != nil {
		t.Fatalf("failed to register client: %v", err)
	}

	// Get the client
	client, err := storage.GetClientByClientID(ctx, creds.ClientID)
	if err != nil {
		t.Fatalf("failed to get client: %v", err)
	}

	if client.GetID() != creds.ClientID {
		t.Errorf("expected client ID %q, got %q", creds.ClientID, client.GetID())
	}

	// Verify redirect URIs
	redirectURIs := client.RedirectURIs()
	if len(redirectURIs) != 1 || redirectURIs[0] != "http://localhost:3000/callback" {
		t.Errorf("unexpected redirect URIs: %v", redirectURIs)
	}
}

func TestStorage_GetClientByNodeID(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Register a client
	creds, err := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-test",
		ProjectID:    "proj-test",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})
	if err != nil {
		t.Fatalf("failed to register client: %v", err)
	}

	// Get by node ID
	foundCreds, err := storage.GetClientByNodeID(ctx, "proj-test", "node-test")
	if err != nil {
		t.Fatalf("failed to get client by node ID: %v", err)
	}

	if foundCreds.ClientID != creds.ClientID {
		t.Errorf("expected client ID %q, got %q", creds.ClientID, foundCreds.ClientID)
	}
}

func TestStorage_AuthorizeClientIDSecret(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Register a client
	creds, err := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})
	if err != nil {
		t.Fatalf("failed to register client: %v", err)
	}

	// Valid credentials
	err = storage.AuthorizeClientIDSecret(ctx, creds.ClientID, creds.ClientSecret)
	if err != nil {
		t.Errorf("expected valid credentials to pass: %v", err)
	}

	// Invalid secret
	err = storage.AuthorizeClientIDSecret(ctx, creds.ClientID, "wrong-secret")
	if err == nil {
		t.Error("expected error for wrong secret")
	}

	// Invalid client ID
	err = storage.AuthorizeClientIDSecret(ctx, "non-existent-id", creds.ClientSecret)
	if err == nil {
		t.Error("expected error for non-existent client")
	}
}

func TestStorage_AuthRequestLifecycle(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Register a client first
	creds, _ := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})

	// Create auth request
	authReq := &oidc.AuthRequest{
		ClientID:     creds.ClientID,
		RedirectURI:  "http://localhost:3000/callback",
		Scopes:       []string{"openid", "profile", "email"},
		State:        "test-state-123",
		Nonce:        "test-nonce-456",
		ResponseType: "code",
	}

	createdReq, err := storage.CreateAuthRequest(ctx, authReq, "")
	if err != nil {
		t.Fatalf("failed to create auth request: %v", err)
	}

	requestID := createdReq.GetID()
	if requestID == "" {
		t.Error("expected non-empty request ID")
	}

	// Retrieve auth request
	retrievedReq, err := storage.AuthRequestByID(ctx, requestID)
	if err != nil {
		t.Fatalf("failed to retrieve auth request: %v", err)
	}

	if retrievedReq.GetClientID() != creds.ClientID {
		t.Errorf("expected client ID %q, got %q", creds.ClientID, retrievedReq.GetClientID())
	}
	if retrievedReq.GetState() != "test-state-123" {
		t.Errorf("expected state %q, got %q", "test-state-123", retrievedReq.GetState())
	}
	if retrievedReq.Done() {
		t.Error("auth request should not be done yet")
	}

	// Complete auth request
	userID := "user-123"
	err = storage.CompleteAuthRequest(ctx, requestID, userID)
	if err != nil {
		t.Fatalf("failed to complete auth request: %v", err)
	}

	// Verify completion
	completedReq, err := storage.AuthRequestByID(ctx, requestID)
	if err != nil {
		t.Fatalf("failed to retrieve completed auth request: %v", err)
	}

	if !completedReq.Done() {
		t.Error("auth request should be done")
	}
	if completedReq.GetSubject() != userID {
		t.Errorf("expected subject %q, got %q", userID, completedReq.GetSubject())
	}

	// Delete auth request
	err = storage.DeleteAuthRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("failed to delete auth request: %v", err)
	}

	// Verify deletion
	_, err = storage.AuthRequestByID(ctx, requestID)
	if err == nil {
		t.Error("expected error when retrieving deleted auth request")
	}
}

func TestStorage_AuthCode(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Register client and create auth request
	creds, _ := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})

	authReq := &oidc.AuthRequest{
		ClientID:     creds.ClientID,
		RedirectURI:  "http://localhost:3000/callback",
		Scopes:       []string{"openid"},
		ResponseType: "code",
	}

	createdReq, _ := storage.CreateAuthRequest(ctx, authReq, "")
	storage.CompleteAuthRequest(ctx, createdReq.GetID(), "user-123")

	// Save auth code
	code := "test-auth-code-xyz"
	err := storage.SaveAuthCode(ctx, createdReq.GetID(), code)
	if err != nil {
		t.Fatalf("failed to save auth code: %v", err)
	}

	// Retrieve auth request by code
	retrievedReq, err := storage.AuthRequestByCode(ctx, code)
	if err != nil {
		t.Fatalf("failed to retrieve auth request by code: %v", err)
	}

	if retrievedReq.GetID() != createdReq.GetID() {
		t.Errorf("expected request ID %q, got %q", createdReq.GetID(), retrievedReq.GetID())
	}
}

func TestStorage_SessionManagement(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Create session
	sessionID := "session-123"
	userID := "user-456"
	expiresAt := time.Now().Add(24 * time.Hour)

	err := storage.CreateSession(ctx, sessionID, userID, expiresAt)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Retrieve session
	session, err := storage.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if session.UserID != userID {
		t.Errorf("expected user ID %q, got %q", userID, session.UserID)
	}

	// Delete session
	err = storage.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	// Verify deletion
	_, err = storage.GetSession(ctx, sessionID)
	if err == nil {
		t.Error("expected error when retrieving deleted session")
	}
}

func TestStorage_ExpiredSession(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Create expired session
	sessionID := "expired-session"
	userID := "user-123"
	expiresAt := time.Now().Add(-1 * time.Hour) // Already expired

	err := storage.CreateSession(ctx, sessionID, userID, expiresAt)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Try to retrieve - should fail
	_, err = storage.GetSession(ctx, sessionID)
	if err == nil {
		t.Error("expected error for expired session")
	}
}

func TestStorage_TokenCreation(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Register client and create auth request
	creds, _ := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})

	authReq := &oidc.AuthRequest{
		ClientID:     creds.ClientID,
		RedirectURI:  "http://localhost:3000/callback",
		Scopes:       []string{"openid"},
		ResponseType: "code",
	}

	createdReq, _ := storage.CreateAuthRequest(ctx, authReq, "")
	storage.CompleteAuthRequest(ctx, createdReq.GetID(), "user-123")

	// Create access token
	tokenReq, _ := storage.AuthRequestByID(ctx, createdReq.GetID())

	tokenID, expiration, err := storage.CreateAccessToken(ctx, tokenReq)
	if err != nil {
		t.Fatalf("failed to create access token: %v", err)
	}

	if tokenID == "" {
		t.Error("expected non-empty token ID")
	}
	if expiration.Before(time.Now()) {
		t.Error("token should not be expired")
	}

	// Verify token is stored
	var token sqlitemodels.OIDCToken
	if err := db.First(&token, "id = ?", tokenID).Error; err != nil {
		t.Fatalf("failed to retrieve stored token: %v", err)
	}

	if token.Subject != "user-123" {
		t.Errorf("expected subject %q, got %q", "user-123", token.Subject)
	}
}

func TestStorage_AccessAndRefreshTokens(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Register client and create auth request
	creds, _ := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})

	authReq := &oidc.AuthRequest{
		ClientID:     creds.ClientID,
		RedirectURI:  "http://localhost:3000/callback",
		Scopes:       []string{"openid", "offline_access"},
		ResponseType: "code",
	}

	createdReq, _ := storage.CreateAuthRequest(ctx, authReq, "")
	storage.CompleteAuthRequest(ctx, createdReq.GetID(), "user-123")

	tokenReq, _ := storage.AuthRequestByID(ctx, createdReq.GetID())

	// Create access and refresh tokens
	accessTokenID, refreshTokenID, expiration, err := storage.CreateAccessAndRefreshTokens(ctx, tokenReq, "")
	if err != nil {
		t.Fatalf("failed to create tokens: %v", err)
	}

	if accessTokenID == "" {
		t.Error("expected non-empty access token ID")
	}
	if refreshTokenID == "" {
		t.Error("expected non-empty refresh token ID")
	}
	if expiration.Before(time.Now()) {
		t.Error("tokens should not be expired")
	}

	// Retrieve by refresh token
	refreshReq, err := storage.TokenRequestByRefreshToken(ctx, refreshTokenID)
	if err != nil {
		t.Fatalf("failed to get token request by refresh token: %v", err)
	}

	if refreshReq.GetSubject() != "user-123" {
		t.Errorf("expected subject %q, got %q", "user-123", refreshReq.GetSubject())
	}
}

func TestStorage_SigningKey(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Get signing key
	key, err := storage.SigningKey(ctx)
	if err != nil {
		t.Fatalf("failed to get signing key: %v", err)
	}

	if key.ID() != signingKey.ID {
		t.Errorf("expected key ID %q, got %q", signingKey.ID, key.ID())
	}
}

func TestStorage_KeySet(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Get key set
	keys, err := storage.KeySet(ctx)
	if err != nil {
		t.Fatalf("failed to get key set: %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	if keys[0].ID() != signingKey.ID {
		t.Errorf("expected key ID %q, got %q", signingKey.ID, keys[0].ID())
	}
}

func TestStorage_RevokeToken(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	// Create a token directly
	scopes, _ := json.Marshal([]string{"openid"})
	audience, _ := json.Marshal([]string{"test-client"})
	token := &sqlitemodels.OIDCToken{
		ID:            "token-to-revoke",
		ApplicationID: "test-client",
		Subject:       "user-123",
		Audience:      string(audience),
		Scopes:        string(scopes),
		AuthTime:      time.Now(),
		ExpiresAt:     time.Now().Add(time.Hour),
	}
	db.Create(token)

	// Revoke it
	oidcErr := storage.RevokeToken(ctx, "token-to-revoke", "user-123", "test-client")
	if oidcErr != nil {
		t.Fatalf("failed to revoke token: %v", oidcErr)
	}

	// Verify deletion
	var count int64
	db.Model(&sqlitemodels.OIDCToken{}).Where("id = ?", "token-to-revoke").Count(&count)
	if count != 0 {
		t.Error("token should have been deleted")
	}
}

func TestStorage_Health(t *testing.T) {
	db := setupTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)
	ctx := context.Background()

	err := storage.Health(ctx)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
}
