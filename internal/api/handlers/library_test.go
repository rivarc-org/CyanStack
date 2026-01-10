package handlers_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPluginYAML = `id: com.test.postgres
name: PostgreSQL
version: "15.0"
category: stateful
inputs:
  - id: INTERNAL_PORT
    type: port
    default: "5432"
  - id: DB_PASSWORD
    type: secret
    inject_as: POSTGRES_PASSWORD
    generate: true
exports:
  - key: CONNECTION_STRING
    value: "postgresql://postgres:{{DB_PASSWORD}}@{{INTERNAL_HOST}}:{{INTERNAL_PORT}}/postgres"
lifecycle:
  start_command: ["postgres"]
  health_check:
    path: ""
    timeout: 30s
`

const testBackendPluginYAML = `id: com.test.backend
name: Backend Service
version: "1.0.0"
category: worker
inputs:
  - id: INTERNAL_PORT
    type: port
    inject_as: PORT
  - id: DATABASE_URL
    type: plugin
    accepts: com.test.postgres
    env_mapping:
      DATABASE_URL: CONNECTION_STRING
lifecycle:
  start_command: ["npm", "start"]
`

func TestLibraryHandler_List_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Write test plugin files
	err := os.WriteFile(filepath.Join(tmpDir, "com.test.postgres.yaml"), []byte(testPluginYAML), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "com.test.backend.yaml"), []byte(testBackendPluginYAML), 0644)
	require.NoError(t, err)

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/library", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var plugins []dto.PluginSummary
	testutil.DecodeResponse(t, w, &plugins)

	assert.Len(t, plugins, 2)

	// Find postgres plugin
	var foundPostgres, foundBackend bool
	for _, p := range plugins {
		if p.ID == "com.test.postgres" {
			foundPostgres = true
			assert.Equal(t, "PostgreSQL", p.Name)
			assert.Equal(t, "15.0", p.Version)
			assert.Equal(t, "stateful", p.Category)
		}
		if p.ID == "com.test.backend" {
			foundBackend = true
			assert.Equal(t, "Backend Service", p.Name)
			assert.Equal(t, "worker", p.Category)
		}
	}

	assert.True(t, foundPostgres, "Should find postgres plugin")
	assert.True(t, foundBackend, "Should find backend plugin")
}

func TestLibraryHandler_List_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/library", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var plugins []dto.PluginSummary
	testutil.DecodeResponse(t, w, &plugins)
	assert.Len(t, plugins, 0)
}

func TestLibraryHandler_List_SkipsInvalidPlugins(t *testing.T) {
	tmpDir := t.TempDir()

	// Write valid plugin
	err := os.WriteFile(filepath.Join(tmpDir, "valid.yaml"), []byte(testPluginYAML), 0644)
	require.NoError(t, err)

	// Write invalid YAML
	err = os.WriteFile(filepath.Join(tmpDir, "invalid.yaml"), []byte("not: valid: yaml: {{"), 0644)
	require.NoError(t, err)

	// Write non-YAML file
	err = os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("not a plugin"), 0644)
	require.NoError(t, err)

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/library", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var plugins []dto.PluginSummary
	testutil.DecodeResponse(t, w, &plugins)

	// Should only contain the valid plugin
	assert.Len(t, plugins, 1)
	assert.Equal(t, "com.test.postgres", plugins[0].ID)
}

func TestLibraryHandler_List_SkipsDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory
	err := os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	require.NoError(t, err)

	// Write valid plugin
	err = os.WriteFile(filepath.Join(tmpDir, "plugin.yaml"), []byte(testPluginYAML), 0644)
	require.NoError(t, err)

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/library", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var plugins []dto.PluginSummary
	testutil.DecodeResponse(t, w, &plugins)
	assert.Len(t, plugins, 1)
}

func TestLibraryHandler_Get_Success(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "com.test.postgres.yaml"), []byte(testPluginYAML), 0644)
	require.NoError(t, err)

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library/:plugin_id", handler.Get)

	w := testutil.MakeRequest(router, "GET", "/api/v1/library/com.test.postgres", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var plugin dto.PluginDetailResponse
	testutil.DecodeResponse(t, w, &plugin)

	assert.Equal(t, "com.test.postgres", plugin.ID)
	assert.Equal(t, "PostgreSQL", plugin.Name)
	assert.Equal(t, "15.0", plugin.Version)
	assert.Equal(t, "stateful", plugin.Category)

	// Verify inputs
	assert.Len(t, plugin.Inputs, 2)

	var foundPortInput, foundPasswordInput bool
	for _, input := range plugin.Inputs {
		if input.ID == "INTERNAL_PORT" {
			foundPortInput = true
			assert.Equal(t, "port", input.Type)
			assert.Equal(t, "5432", input.Default)
		}
		if input.ID == "DB_PASSWORD" {
			foundPasswordInput = true
			assert.Equal(t, "secret", input.Type)
			assert.Equal(t, "POSTGRES_PASSWORD", input.InjectAs)
			assert.True(t, input.Generate)
		}
	}
	assert.True(t, foundPortInput, "Should find INTERNAL_PORT input")
	assert.True(t, foundPasswordInput, "Should find DB_PASSWORD input")

	// Verify exports
	assert.Len(t, plugin.Exports, 1)
	assert.Equal(t, "CONNECTION_STRING", plugin.Exports[0].Key)
	assert.Contains(t, plugin.Exports[0].Value, "postgresql://")
}

func TestLibraryHandler_Get_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library/:plugin_id", handler.Get)

	w := testutil.MakeRequest(router, "GET", "/api/v1/library/nonexistent.plugin", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeNotFound, resp.Code)
}

func TestLibraryHandler_Get_ByDirectoryScan(t *testing.T) {
	tmpDir := t.TempDir()

	// Write plugin with a different filename than the plugin ID
	err := os.WriteFile(filepath.Join(tmpDir, "postgres-plugin.yaml"), []byte(testPluginYAML), 0644)
	require.NoError(t, err)

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library/:plugin_id", handler.Get)

	// Should find plugin by scanning all files
	w := testutil.MakeRequest(router, "GET", "/api/v1/library/com.test.postgres", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var plugin dto.PluginDetailResponse
	testutil.DecodeResponse(t, w, &plugin)
	assert.Equal(t, "com.test.postgres", plugin.ID)
}

func TestLibraryHandler_Get_YmlExtension(t *testing.T) {
	tmpDir := t.TempDir()

	// Write plugin with .yml extension
	err := os.WriteFile(filepath.Join(tmpDir, "com.test.postgres.yml"), []byte(testPluginYAML), 0644)
	require.NoError(t, err)

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library/:plugin_id", handler.Get)

	w := testutil.MakeRequest(router, "GET", "/api/v1/library/com.test.postgres", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var plugin dto.PluginDetailResponse
	testutil.DecodeResponse(t, w, &plugin)
	assert.Equal(t, "com.test.postgres", plugin.ID)
}

func TestLibraryHandler_Get_PluginInputDetails(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "com.test.backend.yaml"), []byte(testBackendPluginYAML), 0644)
	require.NoError(t, err)

	handler := handlers.NewLibraryHandler(tmpDir)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/library/:plugin_id", handler.Get)

	w := testutil.MakeRequest(router, "GET", "/api/v1/library/com.test.backend", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var plugin dto.PluginDetailResponse
	testutil.DecodeResponse(t, w, &plugin)

	// Find plugin input
	var foundPluginInput bool
	for _, input := range plugin.Inputs {
		if input.ID == "DATABASE_URL" {
			foundPluginInput = true
			assert.Equal(t, "plugin", input.Type)
			assert.Equal(t, "com.test.postgres", input.Accepts)
		}
	}
	assert.True(t, foundPluginInput, "Should find DATABASE_URL plugin input")
}
