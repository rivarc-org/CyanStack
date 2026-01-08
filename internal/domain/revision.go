package domain

import "time"

// Revision represents a deployment configuration snapshot.
type Revision struct {
	ID        string
	ProjectID string
	Number    int
	CreatedAt time.Time

	// Plugin configurations for this revision
	Nodes []Node

	// Deployment status
	Status RevisionStatus
}

// RevisionStatus represents the state of a revision.
type RevisionStatus string

const (
	RevisionStatusPending    RevisionStatus = "pending"
	RevisionStatusDeployed   RevisionStatus = "deployed"
	RevisionStatusRolledBack RevisionStatus = "rolled_back"
)
