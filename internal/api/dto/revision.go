package dto

import (
	"time"
)

// RevisionSummary represents a lightweight revision for list responses
// Does NOT include graph_state to save bandwidth
type RevisionSummary struct {
	ID            string    `json:"id"`
	VersionNumber int       `json:"version_number"`
	CreatedAt     time.Time `json:"created_at"`
}

// RevisionDetailResponse represents a full revision including graph state
type RevisionDetailResponse struct {
	ID            string        `json:"id"`
	ProjectID     string        `json:"project_id"`
	VersionNumber int           `json:"version_number"`
	GraphState    GraphStateDTO `json:"graph_state"`
	CreatedAt     time.Time     `json:"created_at"`
}

// GraphStateDTO represents the graph state for a revision
type GraphStateDTO struct {
	Nodes []NodeStateDTO `json:"nodes"`
}

// NodeStateDTO represents a single node in the graph
type NodeStateDTO struct {
	ID       string            `json:"id"`
	PluginID string            `json:"plugin_id"`
	Image    string            `json:"image"`
	Inputs   map[string]string `json:"inputs"`
}

// CreateRevisionRequest represents a request to create a new revision
type CreateRevisionRequest struct {
	GraphState GraphStateDTO `json:"graph_state" binding:"required"`
}
