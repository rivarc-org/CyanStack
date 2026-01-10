package dto

import (
	"time"
)

// ProjectResponse represents a project in API responses
type ProjectResponse struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	CurrentRevisionID *string   `json:"current_revision_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// CreateProjectRequest represents a request to create a new project
type CreateProjectRequest struct {
	Name       string         `json:"name" binding:"required"`
	GraphState *GraphStateDTO `json:"graph_state,omitempty"`
}

// CreateProjectResponse represents the response after creating a project
type CreateProjectResponse struct {
	Project  ProjectResponse `json:"project"`
	Revision RevisionSummary `json:"revision"`
}
