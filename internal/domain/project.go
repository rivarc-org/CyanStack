package domain

import "time"

// Project represents a CyanStack project.
type Project struct {
	ID          string
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Active revision ID
	ActiveRevisionID string

	// All revisions
	Revisions []Revision
}

// ProjectStatus represents the overall project state.
type ProjectStatus string

const (
	ProjectStatusStopped  ProjectStatus = "stopped"
	ProjectStatusStarting ProjectStatus = "starting"
	ProjectStatusRunning  ProjectStatus = "running"
	ProjectStatusStopping ProjectStatus = "stopping"
	ProjectStatusError    ProjectStatus = "error"
)
