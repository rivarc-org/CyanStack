package config

import (
	"fmt"
	"time"
)

// PluginCategory defines the type of plugin
type PluginCategory string

const (
	CategoryWorker   PluginCategory = "worker"
	CategoryStateful PluginCategory = "stateful"
)

// PluginConfig represents the complete plugin configuration
type PluginConfig struct {
	ID        string             `yaml:"id"`
	Version   string             `yaml:"version"`
	Name      string             `yaml:"name"`
	Category  PluginCategory     `yaml:"category"`
	Image     string             `yaml:"image"`              // Docker image for this plugin
	Auth      bool               `yaml:"auth,omitempty"`     // Enable OIDC authentication for this plugin
	Inputs    []InputDefinition  `yaml:"inputs"`
	Lifecycle Lifecycle          `yaml:"lifecycle"`
	Exports   []ExportDefinition `yaml:"exports,omitempty"`
}

// InputDefinition defines an input requirement for a plugin
type InputDefinition struct {
	ID         string            `yaml:"id"`
	Type       string            `yaml:"type"`                  // "port", "secret", "plugin"
	InjectAs   string            `yaml:"inject_as"`
	Generated  bool              `yaml:"generate,omitempty"`
	Default    string            `yaml:"default,omitempty"`     // Default value (e.g., "5432" for fixed ports)
	Accepts    string            `yaml:"accepts,omitempty"`     // For plugin type inputs
	EnvMapping map[string]string `yaml:"env_mapping,omitempty"` // Environment variable mapping
}

// Lifecycle defines the container lifecycle configuration
type Lifecycle struct {
	StartCommand []string    `yaml:"start_command"`
	HealthCheck  HealthCheck `yaml:"health_check"`
	PostInstall  *HookConfig `yaml:"post_install,omitempty"`
	Migration    *HookConfig `yaml:"migration,omitempty"`
}

// HealthCheck configuration for container health monitoring
type HealthCheck struct {
	Path string `yaml:"path"`
	Port string `yaml:"port,omitempty"` // Optional, defaults to assigned port
}

// HookConfig for lifecycle hooks like post_install, migration
type HookConfig struct {
	Command []string `yaml:"command"`
	Timeout Duration `yaml:"timeout"`
}

// ExportDefinition for plugin exports
type ExportDefinition struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

// Duration wraps time.Duration for YAML parsing of strings like "300s"
type Duration struct {
	time.Duration
}

// UnmarshalYAML implements yaml.Unmarshaler for Duration
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = duration
	return nil
}

// MarshalYAML implements yaml.Marshaler for Duration
func (d Duration) MarshalYAML() (interface{}, error) {
	return d.String(), nil
}
