package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/config"
	"github.com/gin-gonic/gin"
)

// LibraryHandler handles plugin library API requests
type LibraryHandler struct {
	pluginPath string
	parser     *config.Parser
}

// NewLibraryHandler creates a new LibraryHandler
func NewLibraryHandler(pluginPath string) *LibraryHandler {
	return &LibraryHandler{
		pluginPath: pluginPath,
		parser:     config.NewParser(),
	}
}

// List returns all available plugins
func (h *LibraryHandler) List(c *gin.Context) {
	// Scan plugin directory for YAML files
	entries, err := os.ReadDir(h.pluginPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to read plugin directory",
			dto.ErrCodeInternal,
		))
		return
	}

	plugins := make([]dto.PluginSummary, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		// Parse the plugin config
		path := filepath.Join(h.pluginPath, name)
		pluginConfig, err := h.parser.ParseFile(path)
		if err != nil {
			// Skip invalid plugins
			continue
		}

		plugins = append(plugins, dto.PluginSummary{
			ID:       pluginConfig.ID,
			Name:     pluginConfig.Name,
			Version:  pluginConfig.Version,
			Category: string(pluginConfig.Category),
		})
	}

	c.JSON(http.StatusOK, plugins)
}

// Get returns details for a specific plugin
func (h *LibraryHandler) Get(c *gin.Context) {
	pluginID := c.Param("plugin_id")

	// Try to find the plugin file
	// Plugin files are named as {plugin_id}.yaml
	possibleNames := []string{
		pluginID + ".yaml",
		pluginID + ".yml",
	}

	var pluginConfig *config.PluginConfig
	var found bool

	for _, name := range possibleNames {
		path := filepath.Join(h.pluginPath, name)
		cfg, err := h.parser.ParseFile(path)
		if err == nil && cfg.ID == pluginID {
			pluginConfig = cfg
			found = true
			break
		}
	}

	// If not found by filename, scan all files
	if !found {
		entries, err := os.ReadDir(h.pluginPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
				"Failed to read plugin directory",
				dto.ErrCodeInternal,
			))
			return
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}

			path := filepath.Join(h.pluginPath, name)
			cfg, err := h.parser.ParseFile(path)
			if err != nil {
				continue
			}

			if cfg.ID == pluginID {
				pluginConfig = cfg
				found = true
				break
			}
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(
			"Plugin not found",
			dto.ErrCodeNotFound,
		))
		return
	}

	// Convert to DTO
	inputs := make([]dto.InputDTO, len(pluginConfig.Inputs))
	for i, input := range pluginConfig.Inputs {
		inputs[i] = dto.InputDTO{
			ID:       input.ID,
			Type:     input.Type,
			InjectAs: input.InjectAs,
			Default:  input.Default,
			Generate: input.Generated,
			Accepts:  input.Accepts,
		}
	}

	exports := make([]dto.ExportDTO, len(pluginConfig.Exports))
	for i, export := range pluginConfig.Exports {
		exports[i] = dto.ExportDTO{
			Key:   export.Key,
			Value: export.Value,
		}
	}

	response := dto.PluginDetailResponse{
		ID:       pluginConfig.ID,
		Name:     pluginConfig.Name,
		Version:  pluginConfig.Version,
		Category: string(pluginConfig.Category),
		Inputs:   inputs,
		Exports:  exports,
	}

	c.JSON(http.StatusOK, response)
}
