package oidc

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	sqlitemodels "github.com/cyanstack/cyanstack/internal/store/sqlite"
)

// setupProviderTestDB creates an in-memory SQLite database for provider testing
func setupProviderTestDB(t *testing.T) *gorm.DB {
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

func TestNewProvider(t *testing.T) {
	db := setupProviderTestDB(t)
	signingKey, err := GenerateSigningKey()
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	storage := NewStorage(db, signingKey)

	provider, err := NewProvider(ProviderConfig{
		Issuer:        "http://localhost:8080",
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if provider == nil {
		t.Fatal("provider should not be nil")
	}
	if provider.Issuer() != "http://localhost:8080" {
		t.Errorf("expected issuer %q, got %q", "http://localhost:8080", provider.Issuer())
	}
}

func TestProvider_Handler(t *testing.T) {
	db := setupProviderTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)

	provider, _ := NewProvider(ProviderConfig{
		Issuer:        "http://localhost:8080",
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})

	handler := provider.Handler()
	if handler == nil {
		t.Fatal("handler should not be nil")
	}
}

func TestProvider_Storage(t *testing.T) {
	db := setupProviderTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)

	provider, _ := NewProvider(ProviderConfig{
		Issuer:        "http://localhost:8080",
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})

	if provider.Storage() != storage {
		t.Error("Storage() should return the same storage instance")
	}
}

func TestProvider_RegisterClient(t *testing.T) {
	db := setupProviderTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)

	provider, _ := NewProvider(ProviderConfig{
		Issuer:        "http://localhost:8080",
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})

	ctx := context.Background()
	creds, err := provider.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})

	if err != nil {
		t.Fatalf("failed to register client: %v", err)
	}
	if creds.ClientID == "" {
		t.Error("expected non-empty client ID")
	}
	if creds.ClientSecret == "" {
		t.Error("expected non-empty client secret")
	}
}

func TestProvider_GetClientByNodeID(t *testing.T) {
	db := setupProviderTestDB(t)
	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)

	provider, _ := NewProvider(ProviderConfig{
		Issuer:        "http://localhost:8080",
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})

	ctx := context.Background()

	// Register client
	creds, _ := provider.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-test",
		ProjectID:    "proj-test",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})

	// Get by node ID
	foundCreds, err := provider.GetClientByNodeID(ctx, "proj-test", "node-test")
	if err != nil {
		t.Fatalf("failed to get client by node ID: %v", err)
	}

	if foundCreds.ClientID != creds.ClientID {
		t.Errorf("expected client ID %q, got %q", creds.ClientID, foundCreds.ClientID)
	}
}

func TestEnsureSigningKey_CreatesNewKey(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	// No key exists yet
	var count int64
	db.Model(&sqlitemodels.OIDCSigningKey{}).Count(&count)
	if count != 0 {
		t.Fatal("expected no signing keys initially")
	}

	// Ensure signing key
	key, err := EnsureSigningKey(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure signing key: %v", err)
	}

	if key == nil {
		t.Fatal("signing key should not be nil")
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

	// Verify key was stored
	db.Model(&sqlitemodels.OIDCSigningKey{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 signing key, got %d", count)
	}
}

func TestEnsureSigningKey_ReusesExistingKey(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	// Create first key
	key1, err := EnsureSigningKey(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure first signing key: %v", err)
	}

	// Ensure again - should return same key
	key2, err := EnsureSigningKey(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure second signing key: %v", err)
	}

	if key1.ID != key2.ID {
		t.Errorf("expected same key ID, got %q and %q", key1.ID, key2.ID)
	}

	// Verify only one key exists
	var count int64
	db.Model(&sqlitemodels.OIDCSigningKey{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 signing key, got %d", count)
	}
}

func TestEnsureDefaultUser_CreatesUser(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	// No users exist yet
	var count int64
	db.Model(&sqlitemodels.OIDCUser{}).Count(&count)
	if count != 0 {
		t.Fatal("expected no users initially")
	}

	// Ensure default user
	err := EnsureDefaultUser(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure default user: %v", err)
	}

	// Verify user was created
	db.Model(&sqlitemodels.OIDCUser{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}

	// Verify user details
	var user sqlitemodels.OIDCUser
	db.First(&user, "email = ?", DefaultUserEmail)

	if user.Email != DefaultUserEmail {
		t.Errorf("expected email %q, got %q", DefaultUserEmail, user.Email)
	}
	if user.PasswordHash == "" {
		t.Error("expected non-empty password hash")
	}
	if user.PasswordHash == DefaultUserPassword {
		t.Error("password should be hashed, not stored in plain text")
	}
}

func TestEnsureDefaultUser_NoOpIfUserExists(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	// Ensure user first time
	err := EnsureDefaultUser(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure default user: %v", err)
	}

	var user1 sqlitemodels.OIDCUser
	db.First(&user1, "email = ?", DefaultUserEmail)

	// Ensure again - should not create new user
	err = EnsureDefaultUser(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure default user second time: %v", err)
	}

	// Verify still only one user
	var count int64
	db.Model(&sqlitemodels.OIDCUser{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}

	var user2 sqlitemodels.OIDCUser
	db.First(&user2, "email = ?", DefaultUserEmail)

	if user1.ID != user2.ID {
		t.Error("should be the same user")
	}
}

func TestValidateCredentials_ValidUser(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	// Create default user
	EnsureDefaultUser(ctx, db)

	// Validate credentials
	user, err := ValidateCredentials(ctx, db, DefaultUserEmail, DefaultUserPassword)
	if err != nil {
		t.Fatalf("failed to validate credentials: %v", err)
	}

	if user == nil {
		t.Fatal("expected non-nil user")
	}
	if user.Email != DefaultUserEmail {
		t.Errorf("expected email %q, got %q", DefaultUserEmail, user.Email)
	}
}

func TestValidateCredentials_InvalidPassword(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	// Create default user
	EnsureDefaultUser(ctx, db)

	// Validate with wrong password
	_, err := ValidateCredentials(ctx, db, DefaultUserEmail, "wrong-password")
	if err == nil {
		t.Error("expected error for wrong password")
	}
}

func TestValidateCredentials_NonExistentUser(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	// Validate non-existent user
	_, err := ValidateCredentials(ctx, db, "nonexistent@test.com", "password")
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestGetUserByID(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	// Create default user
	EnsureDefaultUser(ctx, db)

	// Get user by email first to get ID
	var user sqlitemodels.OIDCUser
	db.First(&user, "email = ?", DefaultUserEmail)

	// Get by ID
	foundUser, err := GetUserByID(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("failed to get user by ID: %v", err)
	}

	if foundUser.ID != user.ID {
		t.Errorf("expected user ID %q, got %q", user.ID, foundUser.ID)
	}
	if foundUser.Email != DefaultUserEmail {
		t.Errorf("expected email %q, got %q", DefaultUserEmail, foundUser.Email)
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	db := setupProviderTestDB(t)
	ctx := context.Background()

	_, err := GetUserByID(ctx, db, "non-existent-id")
	if err == nil {
		t.Error("expected error for non-existent user ID")
	}
}
