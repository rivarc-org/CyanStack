package events

import (
	"encoding/json"
	"time"
)

// EventType represents the type of deployment event
type EventType string

const (
	EventDeployStarted   EventType = "deploy_started"
	EventNodeStarting    EventType = "node_starting"
	EventNodeHealthCheck EventType = "node_health_check"
	EventNodeReady       EventType = "node_ready"
	EventNodeFailed      EventType = "node_failed"
	EventDeployCompleted EventType = "deploy_completed"
	EventDeployFailed    EventType = "deploy_failed"
)

// Event represents a deployment event
type Event struct {
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	ProjectID string          `json:"project_id"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// NodeProgressData contains data for node progress events
type NodeProgressData struct {
	NodeID   string `json:"node_id"`
	PluginID string `json:"plugin_id,omitempty"`
	Phase    string `json:"phase"`
	Message  string `json:"message,omitempty"`
}

// DeployResultData contains data for deployment completion events
type DeployResultData struct {
	Success      bool              `json:"success"`
	ContainerIDs map[string]string `json:"container_ids,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// NewEvent creates a new event with the given type and project ID
func NewEvent(eventType EventType, projectID string, data any) Event {
	event := Event{
		Type:      eventType,
		Timestamp: time.Now(),
		ProjectID: projectID,
	}

	if data != nil {
		if jsonData, err := json.Marshal(data); err == nil {
			event.Data = jsonData
		}
	}

	return event
}
