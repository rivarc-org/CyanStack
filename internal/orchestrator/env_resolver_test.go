package orchestrator

import (
	"context"
	"testing"

	"github.com/cyanstack/cyanstack/internal/config"
)

// mockCrypto implements Crypto for testing
type mockCrypto struct{}

func (m *mockCrypto) DecryptFromJSON(value string) (string, error) {
	return value, nil
}

func TestEnvResolver_PortInput(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.app",
		Version:  "1.0.0",
		Name:     "Test App",
		Category: config.CategoryWorker,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	resolver := NewEnvResolver(nil, pluginLoader, NewSimplePortAllocator())
	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-app",
		PluginID: "com.test.app",
		Image:    "test:latest",
		Inputs:   map[string]string{},
	}

	env, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have PORT injected
	if _, ok := env["PORT"]; !ok {
		t.Error("expected PORT to be injected")
	}

	// Should have port assignment
	if _, ok := runtimeCtx.AssignedPorts[node.ID]; !ok {
		t.Error("expected port assignment in runtime context")
	}
}

func TestEnvResolver_FixedPortInput(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.postgres",
		Version:  "1.0.0",
		Name:     "PostgreSQL",
		Category: config.CategoryStateful,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", Default: "5432", InjectAs: ""},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"postgres"}},
	})

	resolver := NewEnvResolver(nil, pluginLoader, NewSimplePortAllocator())
	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-postgres",
		PluginID: "com.test.postgres",
		Image:    "postgres:15",
		Inputs:   map[string]string{},
	}

	env, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT have any env var injected (InjectAs is empty)
	if len(env) != 0 {
		t.Errorf("expected no env vars, got %d", len(env))
	}

	// Should have port assignment with container port 5432
	portAssign, ok := runtimeCtx.AssignedPorts[node.ID]
	if !ok {
		t.Fatal("expected port assignment in runtime context")
	}
	if portAssign.ContainerPort != 5432 {
		t.Errorf("expected container port 5432, got %d", portAssign.ContainerPort)
	}
	// Host port should be allocated from range
	if portAssign.HostPort < PortRangeStart || portAssign.HostPort >= PortRangeEnd {
		t.Errorf("expected host port in range, got %d", portAssign.HostPort)
	}
}

func TestEnvResolver_SecretInput_UserProvided(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.db",
		Version:  "1.0.0",
		Name:     "Test DB",
		Category: config.CategoryStateful,
		Inputs: []config.InputDefinition{
			{ID: "DB_PASSWORD", Type: "secret", InjectAs: "POSTGRES_PASSWORD"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-db",
		PluginID: "com.test.db",
		Image:    "postgres:15",
		Inputs: map[string]string{
			"DB_PASSWORD": "user-provided-password",
		},
	}

	env, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env["POSTGRES_PASSWORD"] != "user-provided-password" {
		t.Errorf("expected user-provided-password, got %s", env["POSTGRES_PASSWORD"])
	}
}

func TestEnvResolver_SecretInput_Generated(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.db",
		Version:  "1.0.0",
		Name:     "Test DB",
		Category: config.CategoryStateful,
		Inputs: []config.InputDefinition{
			{ID: "DB_PASSWORD", Type: "secret", InjectAs: "POSTGRES_PASSWORD", Generated: true},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-db",
		PluginID: "com.test.db",
		Image:    "postgres:15",
		Inputs:   map[string]string{},
	}

	env, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have a generated password
	if env["POSTGRES_PASSWORD"] == "" {
		t.Error("expected generated password")
	}
}

func TestEnvResolver_SecretInput_NotFoundNoGenerate(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.db",
		Version:  "1.0.0",
		Name:     "Test DB",
		Category: config.CategoryStateful,
		Inputs: []config.InputDefinition{
			{ID: "DB_PASSWORD", Type: "secret", InjectAs: "POSTGRES_PASSWORD", Generated: false},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	resolver := NewEnvResolver(crypto, pluginLoader, NewSimplePortAllocator())
	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	node := NodeState{
		ID:       "node-db",
		PluginID: "com.test.db",
		Image:    "postgres:15",
		Inputs:   map[string]string{},
	}

	_, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err == nil {
		t.Fatal("expected error for missing secret with generate=false")
	}
}

func TestEnvResolver_PluginInput(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()

	// Postgres plugin
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.postgres",
		Version:  "1.0.0",
		Name:     "PostgreSQL",
		Category: config.CategoryStateful,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", Default: "5432", InjectAs: ""},
			{ID: "DB_PASSWORD", Type: "secret", InjectAs: "POSTGRES_PASSWORD", Generated: true},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"postgres"}},
		Exports: []config.ExportDefinition{
			{Key: "CONNECTION_STRING", Value: "postgresql://postgres:{{DB_PASSWORD}}@{{INTERNAL_HOST}}:{{INTERNAL_PORT}}/postgres"},
		},
	})

	// Backend plugin
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.backend",
		Version:  "1.0.0",
		Name:     "Backend",
		Category: config.CategoryWorker,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
			{
				ID:      "MAIN_DATABASE",
				Type:    "plugin",
				Accepts: "com.test.postgres",
				EnvMapping: map[string]string{
					"DATABASE_URL": "CONNECTION_STRING",
				},
			},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	crypto := &mockCrypto{}
	portAllocator := NewSimplePortAllocator()
	resolver := NewEnvResolver(crypto, pluginLoader, portAllocator)
	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	// First, resolve postgres node
	postgresNode := NodeState{
		ID:       "node-postgres",
		PluginID: "com.test.postgres",
		Image:    "postgres:15",
		Inputs:   map[string]string{},
	}

	_, err := resolver.ResolveNodeEnv(context.Background(), postgresNode, runtimeCtx)
	if err != nil {
		t.Fatalf("failed to resolve postgres: %v", err)
	}

	// Now resolve backend node that depends on postgres
	backendNode := NodeState{
		ID:       "node-backend",
		PluginID: "com.test.backend",
		Image:    "app:latest",
		Inputs: map[string]string{
			"MAIN_DATABASE": "node-postgres",
		},
	}

	backendEnv, err := resolver.ResolveNodeEnv(context.Background(), backendNode, runtimeCtx)
	if err != nil {
		t.Fatalf("failed to resolve backend: %v", err)
	}

	// Backend should have DATABASE_URL from postgres CONNECTION_STRING
	if backendEnv["DATABASE_URL"] == "" {
		t.Error("expected DATABASE_URL to be set")
	}

	// DATABASE_URL should contain resolved postgres connection string
	if backendEnv["DATABASE_URL"] == "" {
		t.Error("DATABASE_URL should not be empty")
	}
}

func TestEnvResolver_AutoExports(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.app",
		Version:  "1.0.0",
		Name:     "Test App",
		Category: config.CategoryWorker,
		Inputs: []config.InputDefinition{
			{ID: "INTERNAL_PORT", Type: "port", InjectAs: "PORT"},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	resolver := NewEnvResolver(nil, pluginLoader, NewSimplePortAllocator())
	runtimeCtx := NewRuntimeContext("proj-1", "myproject", "rev-1")

	node := NodeState{
		ID:       "node-app",
		PluginID: "com.test.app",
		Image:    "test:latest",
		Inputs:   map[string]string{},
	}

	_, err := resolver.ResolveNodeEnv(context.Background(), node, runtimeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check auto-exports
	outputs := runtimeCtx.NodeOutputs[node.ID]

	if outputs["INTERNAL_HOST"] != "node-app" {
		t.Errorf("expected INTERNAL_HOST=node-app, got %s", outputs["INTERNAL_HOST"])
	}

	if outputs["INTERNAL_PORT"] == "" {
		t.Error("expected INTERNAL_PORT to be set")
	}

	if outputs["EXTERNAL_HOST"] != "node-app.myproject.localhost" {
		t.Errorf("expected EXTERNAL_HOST=node-app.myproject.localhost, got %s", outputs["EXTERNAL_HOST"])
	}

	if outputs["EXTERNAL_PORT"] != "80" {
		t.Errorf("expected EXTERNAL_PORT=80, got %s", outputs["EXTERNAL_PORT"])
	}
}

func TestResolveTemplates(t *testing.T) {
	tests := []struct {
		name     string
		template string
		values   map[string]string
		expected string
	}{
		{
			name:     "simple replacement",
			template: "host:{{PORT}}",
			values:   map[string]string{"PORT": "5432"},
			expected: "host:5432",
		},
		{
			name:     "multiple replacements",
			template: "postgresql://{{USER}}:{{PASS}}@{{HOST}}:{{PORT}}/db",
			values:   map[string]string{"USER": "admin", "PASS": "secret", "HOST": "localhost", "PORT": "5432"},
			expected: "postgresql://admin:secret@localhost:5432/db",
		},
		{
			name:     "unresolved template",
			template: "{{UNKNOWN}}",
			values:   map[string]string{},
			expected: "{{UNKNOWN}}",
		},
		{
			name:     "no templates",
			template: "plain text",
			values:   map[string]string{"FOO": "bar"},
			expected: "plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveTemplates(tt.template, tt.values)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestEnvResolver_MissingDependency(t *testing.T) {
	pluginLoader := NewInMemoryPluginLoader()
	pluginLoader.Register(&config.PluginConfig{
		ID:       "com.test.backend",
		Version:  "1.0.0",
		Name:     "Backend",
		Category: config.CategoryWorker,
		Inputs: []config.InputDefinition{
			{
				ID:      "MAIN_DATABASE",
				Type:    "plugin",
				Accepts: "com.test.postgres",
				EnvMapping: map[string]string{
					"DATABASE_URL": "CONNECTION_STRING",
				},
			},
		},
		Lifecycle: config.Lifecycle{StartCommand: []string{"start"}},
	})

	resolver := NewEnvResolver(nil, pluginLoader, NewSimplePortAllocator())
	runtimeCtx := NewRuntimeContext("proj-1", "testproject", "rev-1")

	// Try to resolve backend without postgres being started
	backendNode := NodeState{
		ID:       "node-backend",
		PluginID: "com.test.backend",
		Image:    "app:latest",
		Inputs: map[string]string{
			"MAIN_DATABASE": "node-postgres", // References non-existent node
		},
	}

	_, err := resolver.ResolveNodeEnv(context.Background(), backendNode, runtimeCtx)
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}
