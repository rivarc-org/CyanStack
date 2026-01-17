package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupOIDCTestDB creates an in-memory SQLite database for OIDC repository testing
func setupOIDCTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Run migrations for OIDC tables
	err = db.AutoMigrate(
		&OIDCUser{},
		&OIDCClient{},
		&OIDCSession{},
		&OIDCSigningKey{},
		&OIDCAuthRequest{},
		&OIDCAuthCode{},
		&OIDCToken{},
		&OIDCRefreshToken{},
		&Project{},
	)
	if err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return db
}

func TestOIDCRepository_UserOperations(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create user
	user := &OIDCUser{
		ID:           uuid.New().String(),
		Email:        "test@example.com",
		PasswordHash: "hashed-password",
	}

	err := repo.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Get by email
	foundUser, err := repo.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user by email: %v", err)
	}
	if foundUser.ID != user.ID {
		t.Errorf("expected user ID %q, got %q", user.ID, foundUser.ID)
	}

	// Get by ID
	foundUser2, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("failed to get user by ID: %v", err)
	}
	if foundUser2.Email != user.Email {
		t.Errorf("expected email %q, got %q", user.Email, foundUser2.Email)
	}

	// Count users
	count, err := repo.CountUsers(ctx)
	if err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}
}

func TestOIDCRepository_GetUserByEmail_NotFound(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	_, err := repo.GetUserByEmail(ctx, "nonexistent@example.com")
	if err == nil {
		t.Error("expected error for non-existent email")
	}
}

func TestOIDCRepository_ClientOperations(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create client
	client := &OIDCClient{
		ID:            uuid.New().String(),
		SecretHash:    "hashed-secret",
		Name:          "Test App",
		PluginID:      "com.test.app",
		NodeID:        "node-1",
		ProjectID:     "proj-1",
		RedirectURIs:  `["http://localhost:3000/callback"]`,
		GrantTypes:    `["authorization_code"]`,
		ResponseTypes: `["code"]`,
		Scopes:        `["openid", "profile", "email"]`,
	}

	err := repo.CreateClient(ctx, client)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Get by ID
	foundClient, err := repo.GetClientByID(ctx, client.ID)
	if err != nil {
		t.Fatalf("failed to get client by ID: %v", err)
	}
	if foundClient.Name != client.Name {
		t.Errorf("expected name %q, got %q", client.Name, foundClient.Name)
	}

	// Get by node ID
	foundClient2, err := repo.GetClientByNodeID(ctx, "proj-1", "node-1")
	if err != nil {
		t.Fatalf("failed to get client by node ID: %v", err)
	}
	if foundClient2.ID != client.ID {
		t.Errorf("expected client ID %q, got %q", client.ID, foundClient2.ID)
	}
}

func TestOIDCRepository_ListClientsByProject(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create clients for project 1
	for i := 0; i < 3; i++ {
		client := &OIDCClient{
			ID:            uuid.New().String(),
			SecretHash:    "hashed-secret",
			Name:          "Test App",
			PluginID:      "com.test.app",
			NodeID:        uuid.New().String(),
			ProjectID:     "proj-1",
			RedirectURIs:  `["http://localhost:3000/callback"]`,
			GrantTypes:    `["authorization_code"]`,
			ResponseTypes: `["code"]`,
			Scopes:        `["openid"]`,
		}
		repo.CreateClient(ctx, client)
	}

	// Create client for project 2
	client := &OIDCClient{
		ID:            uuid.New().String(),
		SecretHash:    "hashed-secret",
		Name:          "Test App 2",
		PluginID:      "com.test.app",
		NodeID:        uuid.New().String(),
		ProjectID:     "proj-2",
		RedirectURIs:  `["http://localhost:3000/callback"]`,
		GrantTypes:    `["authorization_code"]`,
		ResponseTypes: `["code"]`,
		Scopes:        `["openid"]`,
	}
	repo.CreateClient(ctx, client)

	// List project 1 clients
	clients, err := repo.ListClientsByProject(ctx, "proj-1")
	if err != nil {
		t.Fatalf("failed to list clients: %v", err)
	}
	if len(clients) != 3 {
		t.Errorf("expected 3 clients, got %d", len(clients))
	}

	// List project 2 clients
	clients2, err := repo.ListClientsByProject(ctx, "proj-2")
	if err != nil {
		t.Fatalf("failed to list clients: %v", err)
	}
	if len(clients2) != 1 {
		t.Errorf("expected 1 client, got %d", len(clients2))
	}
}

func TestOIDCRepository_DeleteClientsByProject(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create clients
	for i := 0; i < 2; i++ {
		client := &OIDCClient{
			ID:            uuid.New().String(),
			SecretHash:    "hashed-secret",
			Name:          "Test App",
			PluginID:      "com.test.app",
			NodeID:        uuid.New().String(),
			ProjectID:     "proj-to-delete",
			RedirectURIs:  `["http://localhost:3000/callback"]`,
			GrantTypes:    `["authorization_code"]`,
			ResponseTypes: `["code"]`,
			Scopes:        `["openid"]`,
		}
		repo.CreateClient(ctx, client)
	}

	// Delete all clients for project
	err := repo.DeleteClientsByProject(ctx, "proj-to-delete")
	if err != nil {
		t.Fatalf("failed to delete clients: %v", err)
	}

	// Verify deletion
	clients, _ := repo.ListClientsByProject(ctx, "proj-to-delete")
	if len(clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(clients))
	}
}

func TestOIDCRepository_DeleteClientByNodeID(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create client
	client := &OIDCClient{
		ID:            uuid.New().String(),
		SecretHash:    "hashed-secret",
		Name:          "Test App",
		PluginID:      "com.test.app",
		NodeID:        "node-to-delete",
		ProjectID:     "proj-1",
		RedirectURIs:  `["http://localhost:3000/callback"]`,
		GrantTypes:    `["authorization_code"]`,
		ResponseTypes: `["code"]`,
		Scopes:        `["openid"]`,
	}
	repo.CreateClient(ctx, client)

	// Delete by node ID
	err := repo.DeleteClientByNodeID(ctx, "proj-1", "node-to-delete")
	if err != nil {
		t.Fatalf("failed to delete client: %v", err)
	}

	// Verify deletion
	_, err = repo.GetClientByNodeID(ctx, "proj-1", "node-to-delete")
	if err == nil {
		t.Error("expected error for deleted client")
	}
}

func TestOIDCRepository_SessionOperations(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create user first (for foreign key)
	user := &OIDCUser{
		ID:           uuid.New().String(),
		Email:        "test@example.com",
		PasswordHash: "hashed",
	}
	repo.CreateUser(ctx, user)

	// Create session
	session := &OIDCSession{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	err := repo.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Get session
	foundSession, err := repo.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if foundSession.UserID != user.ID {
		t.Errorf("expected user ID %q, got %q", user.ID, foundSession.UserID)
	}

	// Delete session
	err = repo.DeleteSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	// Verify deletion
	_, err = repo.GetSession(ctx, session.ID)
	if err == nil {
		t.Error("expected error for deleted session")
	}
}

func TestOIDCRepository_DeleteExpiredSessions(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create user
	user := &OIDCUser{
		ID:           uuid.New().String(),
		Email:        "test@example.com",
		PasswordHash: "hashed",
	}
	repo.CreateUser(ctx, user)

	// Create expired session
	expiredSession := &OIDCSession{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	repo.CreateSession(ctx, expiredSession)

	// Create valid session
	validSession := &OIDCSession{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	repo.CreateSession(ctx, validSession)

	// Delete expired sessions
	err := repo.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("failed to delete expired sessions: %v", err)
	}

	// Verify expired session is deleted
	_, err = repo.GetSession(ctx, expiredSession.ID)
	if err == nil {
		t.Error("expected error for deleted expired session")
	}

	// Verify valid session still exists
	_, err = repo.GetSession(ctx, validSession.ID)
	if err != nil {
		t.Errorf("valid session should still exist: %v", err)
	}
}

func TestOIDCRepository_SigningKeyOperations(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create signing key
	key := &OIDCSigningKey{
		ID:            uuid.New().String(),
		PrivateKeyPEM: "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----",
		PublicKeyPEM:  "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----",
		Algorithm:     "RS256",
		IsActive:      true,
	}

	err := repo.CreateSigningKey(ctx, key)
	if err != nil {
		t.Fatalf("failed to create signing key: %v", err)
	}

	// Get active signing key
	foundKey, err := repo.GetActiveSigningKey(ctx)
	if err != nil {
		t.Fatalf("failed to get active signing key: %v", err)
	}
	if foundKey.ID != key.ID {
		t.Errorf("expected key ID %q, got %q", key.ID, foundKey.ID)
	}
}

func TestOIDCRepository_GetActiveSigningKey_NotFound(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	_, err := repo.GetActiveSigningKey(ctx)
	if err == nil {
		t.Error("expected error for no active signing key")
	}
}

func TestOIDCRepository_AuthRequestOperations(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create auth request
	req := &OIDCAuthRequest{
		ID:           uuid.New().String(),
		ClientID:     "client-123",
		RedirectURI:  "http://localhost:3000/callback",
		Scopes:       `["openid"]`,
		State:        "state-123",
		ResponseType: "code",
		Done:         false,
	}

	err := repo.CreateAuthRequest(ctx, req)
	if err != nil {
		t.Fatalf("failed to create auth request: %v", err)
	}

	// Get auth request
	foundReq, err := repo.GetAuthRequest(ctx, req.ID)
	if err != nil {
		t.Fatalf("failed to get auth request: %v", err)
	}
	if foundReq.State != "state-123" {
		t.Errorf("expected state %q, got %q", "state-123", foundReq.State)
	}

	// Update auth request
	userID := "user-456"
	err = repo.UpdateAuthRequest(ctx, req.ID, map[string]interface{}{
		"user_id": userID,
		"done":    true,
	})
	if err != nil {
		t.Fatalf("failed to update auth request: %v", err)
	}

	// Verify update
	updatedReq, _ := repo.GetAuthRequest(ctx, req.ID)
	if updatedReq.UserID == nil || *updatedReq.UserID != userID {
		t.Error("user ID should be updated")
	}
	if !updatedReq.Done {
		t.Error("auth request should be done")
	}

	// Delete auth request
	err = repo.DeleteAuthRequest(ctx, req.ID)
	if err != nil {
		t.Fatalf("failed to delete auth request: %v", err)
	}

	// Verify deletion
	_, err = repo.GetAuthRequest(ctx, req.ID)
	if err == nil {
		t.Error("expected error for deleted auth request")
	}
}

func TestOIDCRepository_CleanupExpiredTokens(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)
	ctx := context.Background()

	// Create expired auth code
	expiredCode := &OIDCAuthCode{
		ID:            uuid.New().String(),
		AuthRequestID: uuid.New().String(),
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
	}
	db.Create(expiredCode)

	// Create valid auth code
	validCode := &OIDCAuthCode{
		ID:            uuid.New().String(),
		AuthRequestID: uuid.New().String(),
		ExpiresAt:     time.Now().Add(1 * time.Hour),
	}
	db.Create(validCode)

	// Create expired token
	expiredToken := &OIDCToken{
		ID:            uuid.New().String(),
		ApplicationID: "app-1",
		Subject:       "user-1",
		Audience:      `["app-1"]`,
		Scopes:        `["openid"]`,
		AuthTime:      time.Now(),
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
	}
	db.Create(expiredToken)

	// Create valid token
	validToken := &OIDCToken{
		ID:            uuid.New().String(),
		ApplicationID: "app-1",
		Subject:       "user-1",
		Audience:      `["app-1"]`,
		Scopes:        `["openid"]`,
		AuthTime:      time.Now(),
		ExpiresAt:     time.Now().Add(1 * time.Hour),
	}
	db.Create(validToken)

	// Cleanup expired tokens
	err := repo.CleanupExpiredTokens(ctx)
	if err != nil {
		t.Fatalf("failed to cleanup expired tokens: %v", err)
	}

	// Verify expired items deleted
	var codeCount int64
	db.Model(&OIDCAuthCode{}).Where("id = ?", expiredCode.ID).Count(&codeCount)
	if codeCount != 0 {
		t.Error("expired auth code should be deleted")
	}

	var tokenCount int64
	db.Model(&OIDCToken{}).Where("id = ?", expiredToken.ID).Count(&tokenCount)
	if tokenCount != 0 {
		t.Error("expired token should be deleted")
	}

	// Verify valid items still exist
	db.Model(&OIDCAuthCode{}).Where("id = ?", validCode.ID).Count(&codeCount)
	if codeCount != 1 {
		t.Error("valid auth code should still exist")
	}

	db.Model(&OIDCToken{}).Where("id = ?", validToken.ID).Count(&tokenCount)
	if tokenCount != 1 {
		t.Error("valid token should still exist")
	}
}

func TestOIDCRepository_DB(t *testing.T) {
	db := setupOIDCTestDB(t)
	repo := NewOIDCRepository(db)

	if repo.DB() != db {
		t.Error("DB() should return the same database instance")
	}
}
