package oidc

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/cyanstack/cyanstack/internal/store/sqlite"
)

const (
	// DefaultUserEmail is the hardcoded test user email
	DefaultUserEmail = "test@test.com"
	// DefaultUserPassword is the hardcoded test user password
	DefaultUserPassword = "test"

	// TestCLIClientID is the client ID for the CLI test command
	TestCLIClientID = "test-cli-client"
	// TestCLIClientSecret is the client secret for the CLI test command
	TestCLIClientSecret = "test-cli-secret-do-not-use-in-production"
)

// EnsureDefaultUser creates the hardcoded test user if it doesn't exist
func EnsureDefaultUser(ctx context.Context, db *gorm.DB) error {
	var count int64
	if err := db.WithContext(ctx).Model(&sqlite.OIDCUser{}).Count(&count).Error; err != nil {
		return fmt.Errorf("counting users: %w", err)
	}

	if count > 0 {
		return nil // User already exists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(DefaultUserPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	user := &sqlite.OIDCUser{
		ID:           uuid.New().String(),
		Email:        DefaultUserEmail,
		PasswordHash: string(hash),
	}

	if err := db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("creating user: %w", err)
	}

	return nil
}

const (
	// TestCLIProjectID is the project ID used for CLI test client
	TestCLIProjectID = "00000000-0000-0000-0000-000000000001"
)

// EnsureTestCLIClient creates a test OIDC client for CLI testing if it doesn't exist
func EnsureTestCLIClient(ctx context.Context, db *gorm.DB) error {
	var client sqlite.OIDCClient
	err := db.WithContext(ctx).First(&client, "id = ?", TestCLIClientID).Error
	if err == nil {
		return nil // Client already exists
	}

	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("checking for test client: %w", err)
	}

	// Ensure test project exists (required for foreign key)
	var project sqlite.Project
	if err := db.WithContext(ctx).First(&project, "id = ?", TestCLIProjectID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			project = sqlite.Project{
				ID:   TestCLIProjectID,
				Name: "CLI Test Project",
			}
			if err := db.WithContext(ctx).Create(&project).Error; err != nil {
				return fmt.Errorf("creating test project: %w", err)
			}
		} else {
			return fmt.Errorf("checking for test project: %w", err)
		}
	}

	// Hash the secret
	hash, err := bcrypt.GenerateFromPassword([]byte(TestCLIClientSecret), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing secret: %w", err)
	}

	// Create the test client with flexible redirect URIs for testing
	client = sqlite.OIDCClient{
		ID:            TestCLIClientID,
		SecretHash:    string(hash),
		Name:          "CLI Test Client",
		PluginID:      "cli-test",
		NodeID:        "cli-test",
		ProjectID:     TestCLIProjectID,
		RedirectURIs:  `["http://localhost:9999/callback", "http://localhost:3000/callback", "http://127.0.0.1:9999/callback"]`,
		GrantTypes:    `["authorization_code", "refresh_token"]`,
		ResponseTypes: `["code"]`,
		Scopes:        `["openid", "profile", "email", "offline_access"]`,
	}

	if err := db.WithContext(ctx).Create(&client).Error; err != nil {
		return fmt.Errorf("creating test client: %w", err)
	}

	return nil
}

// ValidateCredentials checks email/password and returns the user if valid
func ValidateCredentials(ctx context.Context, db *gorm.DB, email, password string) (*sqlite.OIDCUser, error) {
	var user sqlite.OIDCUser
	if err := db.WithContext(ctx).First(&user, "email = ?", email).Error; err != nil {
		return nil, fmt.Errorf("user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	return &user, nil
}

// GetUserByID retrieves a user by ID
func GetUserByID(ctx context.Context, db *gorm.DB, id string) (*sqlite.OIDCUser, error) {
	var user sqlite.OIDCUser
	if err := db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}
