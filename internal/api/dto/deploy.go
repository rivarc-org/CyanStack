package dto

// DeployRequest represents a request to deploy a project
type DeployRequest struct {
	RevisionID string `json:"revision_id" binding:"required"`
}

// DeployResponse represents the response after starting a deployment
type DeployResponse struct {
	Status  string `json:"status"`            // "started", "completed", "failed"
	Message string `json:"message,omitempty"` // Additional context
}

// StopResponse represents the response after stopping a project
type StopResponse struct {
	Status  string `json:"status"` // "stopped"
	Message string `json:"message,omitempty"`
}
