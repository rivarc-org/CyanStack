package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Parser handles loading and parsing plugin configurations
type Parser struct{}

// NewParser creates a new config parser
func NewParser() *Parser {
	return &Parser{}
}

// ParseFile parses a plugin configuration from a file path
func (p *Parser) ParseFile(path string) (*PluginConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return p.Parse(data)
}

// Parse parses plugin configuration from YAML bytes
func (p *Parser) Parse(data []byte) (*PluginConfig, error) {
	var config PluginConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	if err := p.validate(&config); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &config, nil
}

// validate ensures required fields are present and valid
func (p *Parser) validate(config *PluginConfig) error {
	if config.ID == "" {
		return fmt.Errorf("id is required")
	}
	if config.Version == "" {
		return fmt.Errorf("version is required")
	}
	if len(config.Lifecycle.StartCommand) == 0 {
		return fmt.Errorf("lifecycle.start_command is required")
	}

	// Validate inputs
	for i, input := range config.Inputs {
		if input.ID == "" {
			return fmt.Errorf("inputs[%d].id is required", i)
		}
		if input.Type == "" {
			return fmt.Errorf("inputs[%d].type is required", i)
		}
		if !isValidInputType(input.Type) {
			return fmt.Errorf("inputs[%d].type must be port, secret, or plugin; got %q", i, input.Type)
		}
		// Plugin inputs must specify what they accept
		if input.Type == "plugin" && input.Accepts == "" {
			return fmt.Errorf("inputs[%d].accepts is required for plugin type inputs", i)
		}
	}

	// Validate category if provided
	if config.Category != "" && !isValidCategory(config.Category) {
		return fmt.Errorf("category must be worker or stateful; got %q", config.Category)
	}

	return nil
}

func isValidInputType(t string) bool {
	switch t {
	case "port", "secret", "plugin":
		return true
	}
	return false
}

func isValidCategory(c PluginCategory) bool {
	switch c {
	case CategoryWorker, CategoryStateful:
		return true
	}
	return false
}
