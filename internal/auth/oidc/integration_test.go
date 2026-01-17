package oidc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	sqlitemodels "github.com/cyanstack/cyanstack/internal/store/sqlite"
)

// setupIntegrationTestDB creates an in-memory SQLite database for integration testing
func setupIntegrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Run migrations
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

func TestIntegration_FullOIDCFlow(t *testing.T) {
	db := setupIntegrationTestDB(t)
	ctx := context.Background()

	// 1. Setup OIDC provider
	signingKey, err := EnsureSigningKey(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure signing key: %v", err)
	}

	// Create test server to get issuer URL
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Use a test server
	ts := httptest.NewServer(r)
	defer ts.Close()
	issuerURL := ts.URL

	storage := NewStorage(db, signingKey)
	provider, err := NewProvider(ProviderConfig{
		Issuer:        issuerURL,
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// 2. Ensure default user exists
	if err := EnsureDefaultUser(ctx, db); err != nil {
		t.Fatalf("failed to ensure default user: %v", err)
	}

	// 3. Register a client
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

	t.Logf("Registered client: ID=%s, Secret=%s", creds.ClientID, creds.ClientSecret)

	// 4. Verify client credentials
	err = storage.AuthorizeClientIDSecret(ctx, creds.ClientID, creds.ClientSecret)
	if err != nil {
		t.Fatalf("failed to authorize client credentials: %v", err)
	}

	// 5. Create an authorization request
	client, err := storage.GetClientByClientID(ctx, creds.ClientID)
	if err != nil {
		t.Fatalf("failed to get client: %v", err)
	}

	if client.GetID() != creds.ClientID {
		t.Errorf("client ID mismatch")
	}

	// Verify client properties
	if len(client.RedirectURIs()) != 1 {
		t.Errorf("expected 1 redirect URI")
	}
	if len(client.GrantTypes()) == 0 {
		t.Error("expected at least one grant type")
	}

	// 6. Verify user credentials
	user, err := ValidateCredentials(ctx, db, DefaultUserEmail, DefaultUserPassword)
	if err != nil {
		t.Fatalf("failed to validate credentials: %v", err)
	}

	t.Logf("Validated user: ID=%s, Email=%s", user.ID, user.Email)

	// 7. Verify signing key
	key, err := storage.SigningKey(ctx)
	if err != nil {
		t.Fatalf("failed to get signing key: %v", err)
	}

	if key.ID() != signingKey.ID {
		t.Errorf("signing key ID mismatch")
	}

	// 8. Verify key set
	keys, err := storage.KeySet(ctx)
	if err != nil {
		t.Fatalf("failed to get key set: %v", err)
	}

	if len(keys) != 1 {
		t.Errorf("expected 1 key in key set")
	}
}

func TestIntegration_ClientSecretValidation(t *testing.T) {
	db := setupIntegrationTestDB(t)
	ctx := context.Background()

	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)

	// Register client
	creds, _ := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})

	// Test various secret scenarios
	tests := []struct {
		name      string
		clientID  string
		secret    string
		expectErr bool
	}{
		{"valid credentials", creds.ClientID, creds.ClientSecret, false},
		{"wrong secret", creds.ClientID, "wrong-secret", true},
		{"empty secret", creds.ClientID, "", true},
		{"non-existent client", "non-existent", creds.ClientSecret, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.AuthorizeClientIDSecret(ctx, tt.clientID, tt.secret)
			if (err != nil) != tt.expectErr {
				t.Errorf("expected error=%v, got err=%v", tt.expectErr, err)
			}
		})
	}
}

func TestIntegration_SessionManagement(t *testing.T) {
	db := setupIntegrationTestDB(t)
	ctx := context.Background()

	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)

	// Create user
	EnsureDefaultUser(ctx, db)
	user, _ := ValidateCredentials(ctx, db, DefaultUserEmail, DefaultUserPassword)

	// Create session
	sessionID := "test-session-123"
	expiresAt := time.Now().Add(24 * time.Hour)

	err := storage.CreateSession(ctx, sessionID, user.ID, expiresAt)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Retrieve session
	session, err := storage.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if session.UserID != user.ID {
		t.Errorf("session user ID mismatch")
	}

	// Delete session
	err = storage.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	// Verify deletion
	_, err = storage.GetSession(ctx, sessionID)
	if err == nil {
		t.Error("expected error for deleted session")
	}
}

func TestIntegration_MultipleClients(t *testing.T) {
	db := setupIntegrationTestDB(t)
	ctx := context.Background()

	signingKey, _ := GenerateSigningKey()
	storage := NewStorage(db, signingKey)

	// Register multiple clients for same project
	clients := make([]*ClientCredentials, 3)
	for i := 0; i < 3; i++ {
		creds, err := storage.RegisterClient(ctx, ClientRegistration{
			Name:         "Test App",
			PluginID:     "com.test.app",
			NodeID:       "node-" + string(rune('A'+i)),
			ProjectID:    "proj-1",
			RedirectURIs: []string{"http://localhost:3000/callback"},
		})
		if err != nil {
			t.Fatalf("failed to register client %d: %v", i, err)
		}
		clients[i] = creds
	}

	// Verify all clients have unique IDs and secrets
	idSet := make(map[string]bool)
	secretSet := make(map[string]bool)
	for _, c := range clients {
		if idSet[c.ClientID] {
			t.Error("duplicate client ID")
		}
		idSet[c.ClientID] = true

		if secretSet[c.ClientSecret] {
			t.Error("duplicate client secret")
		}
		secretSet[c.ClientSecret] = true
	}

	// Verify each client's credentials work
	for _, c := range clients {
		err := storage.AuthorizeClientIDSecret(ctx, c.ClientID, c.ClientSecret)
		if err != nil {
			t.Errorf("failed to authorize client %s: %v", c.ClientID, err)
		}
	}
}

func TestIntegration_OIDCDiscoveryDocument(t *testing.T) {
	db := setupIntegrationTestDB(t)
	ctx := context.Background()

	signingKey, _ := EnsureSigningKey(ctx, db)
	storage := NewStorage(db, signingKey)

	// Create test server
	gin.SetMode(gin.TestMode)

	ts := httptest.NewServer(nil)
	defer ts.Close()
	issuerURL := ts.URL

	provider, err := NewProvider(ProviderConfig{
		Issuer:        issuerURL,
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Mount provider handler
	mux := http.NewServeMux()
	mux.Handle("/", provider.Handler())

	// Replace test server handler
	ts.Config.Handler = mux

	// Fetch discovery document
	resp, err := http.Get(issuerURL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("failed to fetch discovery document: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Parse discovery document
	var discovery map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		t.Fatalf("failed to parse discovery document: %v", err)
	}

	// Verify required fields
	requiredFields := []string{
		"issuer",
		"authorization_endpoint",
		"token_endpoint",
		"jwks_uri",
		"response_types_supported",
		"subject_types_supported",
		"id_token_signing_alg_values_supported",
	}

	for _, field := range requiredFields {
		if _, ok := discovery[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// Verify issuer matches
	if discovery["issuer"] != issuerURL {
		t.Errorf("expected issuer %q, got %q", issuerURL, discovery["issuer"])
	}
}

func TestIntegration_JWKSEndpoint(t *testing.T) {
	db := setupIntegrationTestDB(t)
	ctx := context.Background()

	signingKey, _ := EnsureSigningKey(ctx, db)
	storage := NewStorage(db, signingKey)

	gin.SetMode(gin.TestMode)

	ts := httptest.NewServer(nil)
	defer ts.Close()
	issuerURL := ts.URL

	provider, err := NewProvider(ProviderConfig{
		Issuer:        issuerURL,
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", provider.Handler())
	ts.Config.Handler = mux

	// Fetch JWKS (default endpoint is /keys in zitadel/oidc)
	resp, err := http.Get(issuerURL + "/keys")
	if err != nil {
		t.Fatalf("failed to fetch JWKS: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Parse JWKS
	var jwks struct {
		Keys []map[string]interface{} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		t.Fatalf("failed to parse JWKS: %v", err)
	}

	if len(jwks.Keys) == 0 {
		t.Fatal("expected at least one key in JWKS")
	}

	// Verify key properties
	key := jwks.Keys[0]
	if key["kty"] != "RSA" {
		t.Errorf("expected key type RSA, got %v", key["kty"])
	}
	if key["use"] != "sig" {
		t.Errorf("expected key use sig, got %v", key["use"])
	}
	if key["alg"] != "RS256" {
		t.Errorf("expected algorithm RS256, got %v", key["alg"])
	}
	if key["kid"] != signingKey.ID {
		t.Errorf("expected key ID %q, got %v", signingKey.ID, key["kid"])
	}
}

func TestIntegration_AuthorizeEndpointRedirect(t *testing.T) {
	db := setupIntegrationTestDB(t)
	ctx := context.Background()

	signingKey, _ := EnsureSigningKey(ctx, db)
	storage := NewStorage(db, signingKey)

	gin.SetMode(gin.TestMode)

	ts := httptest.NewServer(nil)
	defer ts.Close()
	issuerURL := ts.URL

	provider, err := NewProvider(ProviderConfig{
		Issuer:        issuerURL,
		DB:            db,
		Storage:       storage,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", provider.Handler())
	ts.Config.Handler = mux

	// Register a client
	creds, _ := storage.RegisterClient(ctx, ClientRegistration{
		Name:         "Test App",
		PluginID:     "com.test.app",
		NodeID:       "node-1",
		ProjectID:    "proj-1",
		RedirectURIs: []string{"http://localhost:3000/callback"},
	})

	// Build authorize URL
	authorizeURL, _ := url.Parse(issuerURL + "/authorize")
	q := authorizeURL.Query()
	q.Set("client_id", creds.ClientID)
	q.Set("redirect_uri", "http://localhost:3000/callback")
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email")
	q.Set("state", "test-state")
	authorizeURL.RawQuery = q.Encode()

	// Create client that doesn't follow redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(authorizeURL.String())
	if err != nil {
		t.Fatalf("failed to request authorize endpoint: %v", err)
	}
	defer resp.Body.Close()

	// Should redirect to login
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected redirect status, got %d: %s", resp.StatusCode, string(body))
	}

	location := resp.Header.Get("Location")
	if !strings.Contains(location, "/login") {
		t.Errorf("expected redirect to login, got %s", location)
	}
}

func TestIntegration_KeyPersistence(t *testing.T) {
	db := setupIntegrationTestDB(t)
	ctx := context.Background()

	// Ensure signing key - first time
	key1, err := EnsureSigningKey(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure signing key: %v", err)
	}

	// Ensure signing key again - should return same key
	key2, err := EnsureSigningKey(ctx, db)
	if err != nil {
		t.Fatalf("failed to ensure signing key second time: %v", err)
	}

	if key1.ID != key2.ID {
		t.Error("key ID should be persisted")
	}

	// Verify keys are mathematically equivalent
	if key1.PrivateKey.N.Cmp(key2.PrivateKey.N) != 0 {
		t.Error("private keys should match")
	}
}
