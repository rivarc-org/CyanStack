package orchestrator

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/cyanstack/cyanstack/internal/config"
)

// PluginLoader defines the interface for loading plugin configurations
type PluginLoader interface {
	Load(pluginID string) (*config.PluginConfig, error)
}

// FilePluginLoader loads plugin configurations from YAML files
type FilePluginLoader struct {
	basePath string
	parser   *config.Parser
	cache    map[string]*config.PluginConfig
	mu       sync.RWMutex
}

// NewFilePluginLoader creates a new file-based plugin loader
// basePath should point to a directory containing plugin YAML files
// Files should be named {plugin_id}.yaml (e.g., com.cyanstack.postgres.yaml)
func NewFilePluginLoader(basePath string) *FilePluginLoader {
	return &FilePluginLoader{
		basePath: basePath,
		parser:   config.NewParser(),
		cache:    make(map[string]*config.PluginConfig),
	}
}

// Load loads a plugin configuration by plugin ID
// It looks for a file named {pluginID}.yaml in the base path
func (l *FilePluginLoader) Load(pluginID string) (*config.PluginConfig, error) {
	// Check cache first
	l.mu.RLock()
	if cfg, ok := l.cache[pluginID]; ok {
		l.mu.RUnlock()
		return cfg, nil
	}
	l.mu.RUnlock()

	// Load from file
	filename := pluginID + ".yaml"
	filePath := filepath.Join(l.basePath, filename)

	cfg, err := l.parser.ParseFile(filePath)
	if err != nil {
		return nil, &PluginNotFoundError{PluginID: pluginID}
	}

	// Verify the plugin ID matches
	if cfg.ID != pluginID {
		return nil, fmt.Errorf("plugin ID mismatch: file contains %s but expected %s", cfg.ID, pluginID)
	}

	// Cache the result
	l.mu.Lock()
	l.cache[pluginID] = cfg
	l.mu.Unlock()

	return cfg, nil
}

// ClearCache clears the plugin cache
func (l *FilePluginLoader) ClearCache() {
	l.mu.Lock()
	l.cache = make(map[string]*config.PluginConfig)
	l.mu.Unlock()
}

// InMemoryPluginLoader is a plugin loader that uses an in-memory map
// Useful for testing
type InMemoryPluginLoader struct {
	plugins map[string]*config.PluginConfig
	mu      sync.RWMutex
}

// NewInMemoryPluginLoader creates a new in-memory plugin loader
func NewInMemoryPluginLoader() *InMemoryPluginLoader {
	return &InMemoryPluginLoader{
		plugins: make(map[string]*config.PluginConfig),
	}
}

// Register adds a plugin configuration to the loader
func (l *InMemoryPluginLoader) Register(cfg *config.PluginConfig) {
	l.mu.Lock()
	l.plugins[cfg.ID] = cfg
	l.mu.Unlock()
}

// Load retrieves a plugin configuration by ID
func (l *InMemoryPluginLoader) Load(pluginID string) (*config.PluginConfig, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	cfg, ok := l.plugins[pluginID]
	if !ok {
		return nil, &PluginNotFoundError{PluginID: pluginID}
	}
	return cfg, nil
}
