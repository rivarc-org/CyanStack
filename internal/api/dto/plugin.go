package dto

// PluginSummary represents a plugin in list responses
type PluginSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Category string `json:"category"` // "worker", "stateful"
}

// PluginDetailResponse represents a full plugin with its configuration
type PluginDetailResponse struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Version  string      `json:"version"`
	Category string      `json:"category"`
	Image    string      `json:"image"`
	Inputs   []InputDTO  `json:"inputs"`
	Exports  []ExportDTO `json:"exports"`
}

// InputDTO represents an input definition for a plugin
type InputDTO struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	InjectAs string `json:"inject_as,omitempty"`
	Default  string `json:"default,omitempty"`
	Generate bool   `json:"generate,omitempty"`
	Accepts  string `json:"accepts,omitempty"` // For plugin references
}

// ExportDTO represents an export definition for a plugin
type ExportDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"` // Template like "{{HOST}}:{{PORT}}"
}
