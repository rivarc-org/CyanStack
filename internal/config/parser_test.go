package config_test

import (
	"testing"
	"time"

	"github.com/cyanstack/cyanstack/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_ParseFile_MedusaConfig(t *testing.T) {
	parser := config.NewParser()

	cfg, err := parser.ParseFile("../../testdata/medusa-config.yaml")
	require.NoError(t, err)

	// Basic fields
	assert.Equal(t, "com.medusajs.backend", cfg.ID)
	assert.Equal(t, "2.1.0", cfg.Version)
	assert.Equal(t, "Medusa E-Commerce Backend", cfg.Name)
	assert.Equal(t, config.CategoryWorker, cfg.Category)

	// Inputs
	require.Len(t, cfg.Inputs, 3)

	// Port input
	assert.Equal(t, "INTERNAL_PORT", cfg.Inputs[0].ID)
	assert.Equal(t, "port", cfg.Inputs[0].Type)
	assert.Equal(t, "PORT", cfg.Inputs[0].InjectAs)
	assert.False(t, cfg.Inputs[0].Generated)

	// Secret input with generation
	assert.Equal(t, "JWT_SECRET", cfg.Inputs[1].ID)
	assert.Equal(t, "secret", cfg.Inputs[1].Type)
	assert.Equal(t, "JWT_SECRET", cfg.Inputs[1].InjectAs)
	assert.True(t, cfg.Inputs[1].Generated)

	// Plugin dependency input
	assert.Equal(t, "MAIN_DATABASE", cfg.Inputs[2].ID)
	assert.Equal(t, "plugin", cfg.Inputs[2].Type)
	assert.Equal(t, "com.cyanstack.postgres", cfg.Inputs[2].Accepts)
	assert.Equal(t, "URL", cfg.Inputs[2].EnvMapping["CONTAINER_URL"])
	assert.Equal(t, "CONNECTION_STRING", cfg.Inputs[2].EnvMapping["DATABASE_URL"])

	// Lifecycle
	assert.Equal(t, []string{"medusa", "start"}, cfg.Lifecycle.StartCommand)
	assert.Equal(t, "/health", cfg.Lifecycle.HealthCheck.Path)

	// Hooks
	require.NotNil(t, cfg.Lifecycle.PostInstall)
	assert.Equal(t, []string{"medusa", "seed", "--seed-file=data/seed.json"}, cfg.Lifecycle.PostInstall.Command)
	assert.Equal(t, 300*time.Second, cfg.Lifecycle.PostInstall.Timeout.Duration)

	require.NotNil(t, cfg.Lifecycle.Migration)
	assert.Equal(t, []string{"medusa", "migrations", "run"}, cfg.Lifecycle.Migration.Command)
	assert.Equal(t, 120*time.Second, cfg.Lifecycle.Migration.Timeout.Duration)

	// Exports
	require.Len(t, cfg.Exports, 1)
	assert.Equal(t, "CUSTOM_EXPORT", cfg.Exports[0].Key)
	assert.Equal(t, "EXPORT", cfg.Exports[0].Value)
}

func TestParser_ParseFile_PostgresConfig(t *testing.T) {
	parser := config.NewParser()

	cfg, err := parser.ParseFile("../../testdata/postgres-config.yaml")
	require.NoError(t, err)

	assert.Equal(t, "com.cyanstack.postgres", cfg.ID)
	assert.Equal(t, "15.0", cfg.Version)
	assert.Equal(t, "PostgreSQL Database", cfg.Name)
	assert.Equal(t, config.CategoryStateful, cfg.Category)

	require.Len(t, cfg.Inputs, 2)
	assert.Equal(t, "port", cfg.Inputs[0].Type)
	assert.Equal(t, "secret", cfg.Inputs[1].Type)
	assert.True(t, cfg.Inputs[1].Generated)

	assert.Equal(t, []string{"docker-entrypoint.sh", "postgres"}, cfg.Lifecycle.StartCommand)
	assert.Equal(t, "5432", cfg.Lifecycle.HealthCheck.Port)

	require.Len(t, cfg.Exports, 2)
}

func TestParser_Parse_ValidationErrors(t *testing.T) {
	parser := config.NewParser()

	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "missing id",
			yaml:    "version: '1.0'\nlifecycle:\n  start_command: ['run']",
			wantErr: "id is required",
		},
		{
			name:    "missing version",
			yaml:    "id: test\nlifecycle:\n  start_command: ['run']",
			wantErr: "version is required",
		},
		{
			name:    "missing start_command",
			yaml:    "id: test\nversion: '1.0'",
			wantErr: "lifecycle.start_command is required",
		},
		{
			name:    "invalid input type",
			yaml:    "id: test\nversion: '1.0'\nlifecycle:\n  start_command: ['run']\ninputs:\n  - id: TEST\n    type: invalid\n    inject_as: TEST",
			wantErr: "inputs[0].type must be port, secret, or plugin",
		},
		{
			name:    "missing input id",
			yaml:    "id: test\nversion: '1.0'\nlifecycle:\n  start_command: ['run']\ninputs:\n  - type: port\n    inject_as: TEST",
			wantErr: "inputs[0].id is required",
		},
		{
			name:    "missing input type",
			yaml:    "id: test\nversion: '1.0'\nlifecycle:\n  start_command: ['run']\ninputs:\n  - id: TEST\n    inject_as: TEST",
			wantErr: "inputs[0].type is required",
		},
		{
			name:    "plugin input missing accepts",
			yaml:    "id: test\nversion: '1.0'\nlifecycle:\n  start_command: ['run']\ninputs:\n  - id: DB\n    type: plugin\n    inject_as: DB",
			wantErr: "inputs[0].accepts is required for plugin type inputs",
		},
		{
			name:    "invalid category",
			yaml:    "id: test\nversion: '1.0'\ncategory: invalid\nlifecycle:\n  start_command: ['run']",
			wantErr: "category must be worker or stateful",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.Parse([]byte(tt.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParser_ParseFile_NotFound(t *testing.T) {
	parser := config.NewParser()

	_, err := parser.ParseFile("/nonexistent/path.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

func TestParser_Parse_InvalidYAML(t *testing.T) {
	parser := config.NewParser()

	_, err := parser.Parse([]byte("not: valid: yaml: {{"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing YAML")
}

func TestParser_Parse_MinimalValidConfig(t *testing.T) {
	parser := config.NewParser()

	yaml := `
id: minimal
version: "1.0"
lifecycle:
  start_command: ["run"]
`
	cfg, err := parser.Parse([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, "minimal", cfg.ID)
	assert.Equal(t, "1.0", cfg.Version)
	assert.Equal(t, []string{"run"}, cfg.Lifecycle.StartCommand)
	assert.Empty(t, cfg.Inputs)
	assert.Empty(t, cfg.Exports)
}
