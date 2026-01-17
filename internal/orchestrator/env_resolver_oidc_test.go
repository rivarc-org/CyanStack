package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/cyanstack/cyanstack/internal/config"
)

// mockOIDCRegistrar implements OIDCRegistrar for testing
type mockOIDCRegistrar struct {
	registeredClients map[string]*OIDCCredentials
	lastRegistration  OIDCClientRegistration
	shouldFail        bool
}

func newMockOIDCRegistrar() *mockOIDCRegistrar {
	return &mockOIDCRegistrar{
		registeredClients: make(map[string]*OIDCCredentials),
	}
}

func (m *mockOIDCRegistrar) RegisterClient(ctx context.Context, reg OIDCClientRegistration) (*OIDCCredentials, error) {
	if m.shouldFail {
		return nil, errors.New("mock registration failure")
	}

	m.lastRegistration = reg
	clientKey := reg.ProjectID + ":" + reg.NodeID
	creds := &OIDCCredentials{
		ClientID:     "test-client-id-" + reg.NodeID,
		ClientSecret: "test-client-secret-" + reg.NodeID,
	}
	m.registeredClients[clientKey] = creds
	return creds, nil
}

func (m *mockOIDCRegistrar) GetClientByNodeID(ctx context.Context, projectID, nodeID string) (*OIDCCredentials, error) {
	clientKey := projectID + ":" + nodeID
	if creds, ok := m.registeredClients[clientKey]; ok {
		return creds, nil
	}
	return nil, errors.New("client not found")
}

func TestEnvResolver_OIDCCredentialInjection(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()

	// Plugin with auth: true
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.authapp",
		Version:  "1.0.0",
		Name:     "Auth-Enabled App",
		Category: config.CategoryWorker,
		Auth:     true, // Enable OIDC
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	oidcRegistrar := newMockOIDCRegistrar()
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	resolver.SetOIDCRegistrar(oidcRegistrar, "http://localhost:8080")

	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-auth-app",
		PluginID: "com.test.authapp",
		Inputs:   map[string]string{},
	}

	env, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify OIDC environment variables are injected
	if env["OIDC_CLIENT_ID"] == "" {
		t.Error("expected OIDC_CLIENT_ID to be set")
	}
	if env["OIDC_CLIENT_SECRET"] == "" {
		t.Error("expected OIDC_CLIENT_SECRET to be set")
	}
	if env["OIDC_ISSUER_URL"] == "" {
		t.Error("expected OIDC_ISSUER_URL to be set")
	}
	if env["OIDC_DISCOVERY_URL"] == "" {
		t.Error("expected OIDC_DISCOVERY_URL to be set")
	}
	if env["OIDC_TOKEN_URL"] == "" {
		t.Error("expected OIDC_TOKEN_URL to be set")
	}
	if env["OIDC_USERINFO_URL"] == "" {
		t.Error("expected OIDC_USERINFO_URL to be set")
	}
	if env["OIDC_JWKS_URL"] == "" {
		t.Error("expected OIDC_JWKS_URL to be set")
	}
	if env["OIDC_AUTHORIZE_URL"] == "" {
		t.Error("expected OIDC_AUTHORIZE_URL to be set")
	}

	// Verify the correct client ID pattern
	expectedClientID := "test-client-id-node-auth-app"
	if env["OIDC_CLIENT_ID"] != expectedClientID {
		t.Errorf("expected client ID %q, got %q", expectedClientID, env["OIDC_CLIENT_ID"])
	}
}

func TestEnvResolver_OIDCNotInjectedWhenAuthFalse(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()

	// Plugin with auth: false (default)
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.noauthapp",
		Version:  "1.0.0",
		Name:     "No-Auth App",
		Category: config.CategoryWorker,
		Auth:     false,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	oidcRegistrar := newMockOIDCRegistrar()
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	resolver.SetOIDCRegistrar(oidcRegistrar, "http://localhost:8080")

	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-no-auth",
		PluginID: "com.test.noauthapp",
		Inputs:   map[string]string{},
	}

	env, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify OIDC environment variables are NOT injected
	if _, ok := env["OIDC_CLIENT_ID"]; ok {
		t.Error("OIDC_CLIENT_ID should not be set when auth is false")
	}
	if _, ok := env["OIDC_CLIENT_SECRET"]; ok {
		t.Error("OIDC_CLIENT_SECRET should not be set when auth is false")
	}
}

func TestEnvResolver_OIDCNotInjectedWhenNoRegistrar(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()

	// Plugin with auth: true but no registrar set
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.authapp",
		Version:  "1.0.0",
		Name:     "Auth-Enabled App",
		Category: config.CategoryWorker,
		Auth:     true,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	// No OIDC registrar set

	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-auth-app",
		PluginID: "com.test.authapp",
		Inputs:   map[string]string{},
	}

	env, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify OIDC environment variables are NOT injected
	if _, ok := env["OIDC_CLIENT_ID"]; ok {
		t.Error("OIDC_CLIENT_ID should not be set when no registrar is configured")
	}
}

func TestEnvResolver_OIDCRegistrationDetails(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()

	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.authapp",
		Version:  "1.0.0",
		Name:     "Auth-Enabled App",
		Category: config.CategoryWorker,
		Auth:     true,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	oidcRegistrar := newMockOIDCRegistrar()
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	resolver.SetOIDCRegistrar(oidcRegistrar, "http://localhost:8080")

	runtimeCtx := NewRuntimeContext("proj-test", "myproject", "rev-1")

	node := NodeState{
		ID:       "my-auth-node",
		PluginID: "com.test.authapp",
		Inputs:   map[string]string{},
	}

	_, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify registration details
	reg := oidcRegistrar.lastRegistration

	if reg.Name != "Auth-Enabled App" {
		t.Errorf("expected name %q, got %q", "Auth-Enabled App", reg.Name)
	}
	if reg.PluginID != "com.test.authapp" {
		t.Errorf("expected plugin ID %q, got %q", "com.test.authapp", reg.PluginID)
	}
	if reg.NodeID != "my-auth-node" {
		t.Errorf("expected node ID %q, got %q", "my-auth-node", reg.NodeID)
	}
	if reg.ProjectID != "proj-test" {
		t.Errorf("expected project ID %q, got %q", "proj-test", reg.ProjectID)
	}

	// Verify redirect URI format
	expectedRedirectURI := "http://my-auth-node.myproject.localhost/auth/callback"
	if len(reg.RedirectURIs) != 1 || reg.RedirectURIs[0] != expectedRedirectURI {
		t.Errorf("expected redirect URI %q, got %v", expectedRedirectURI, reg.RedirectURIs)
	}
}

func TestEnvResolver_OIDCInternalURLs(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()

	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.authapp",
		Version:  "1.0.0",
		Name:     "Auth-Enabled App",
		Category: config.CategoryWorker,
		Auth:     true,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	oidcRegistrar := newMockOIDCRegistrar()
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	resolver.SetOIDCRegistrar(oidcRegistrar, "http://localhost:8080")

	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-auth-app",
		PluginID: "com.test.authapp",
		Inputs:   map[string]string{},
	}

	env, _ := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)

	// Verify internal URLs use host.docker.internal
	expectedIssuer := "http://host.docker.internal:8080"
	if env["OIDC_ISSUER_URL"] != expectedIssuer {
		t.Errorf("expected issuer URL %q, got %q", expectedIssuer, env["OIDC_ISSUER_URL"])
	}

	expectedDiscovery := expectedIssuer + "/.well-known/openid-configuration"
	if env["OIDC_DISCOVERY_URL"] != expectedDiscovery {
		t.Errorf("expected discovery URL %q, got %q", expectedDiscovery, env["OIDC_DISCOVERY_URL"])
	}

	expectedToken := expectedIssuer + "/token"
	if env["OIDC_TOKEN_URL"] != expectedToken {
		t.Errorf("expected token URL %q, got %q", expectedToken, env["OIDC_TOKEN_URL"])
	}

	expectedUserinfo := expectedIssuer + "/userinfo"
	if env["OIDC_USERINFO_URL"] != expectedUserinfo {
		t.Errorf("expected userinfo URL %q, got %q", expectedUserinfo, env["OIDC_USERINFO_URL"])
	}

	expectedJwks := expectedIssuer + "/jwks"
	if env["OIDC_JWKS_URL"] != expectedJwks {
		t.Errorf("expected JWKS URL %q, got %q", expectedJwks, env["OIDC_JWKS_URL"])
	}

	// Browser-facing URL should use the issuer URL
	expectedAuthorize := "http://localhost:8080/authorize"
	if env["OIDC_AUTHORIZE_URL"] != expectedAuthorize {
		t.Errorf("expected authorize URL %q, got %q", expectedAuthorize, env["OIDC_AUTHORIZE_URL"])
	}
}

func TestEnvResolver_OIDCRegistrationFailure(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()

	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.authapp",
		Version:  "1.0.0",
		Name:     "Auth-Enabled App",
		Category: config.CategoryWorker,
		Auth:     true,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	oidcRegistrar := newMockOIDCRegistrar()
	oidcRegistrar.shouldFail = true
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	resolver.SetOIDCRegistrar(oidcRegistrar, "http://localhost:8080")

	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-auth-app",
		PluginID: "com.test.authapp",
		Inputs:   map[string]string{},
	}

	_, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err == nil {
		t.Fatal("expected error when OIDC registration fails")
	}
}
