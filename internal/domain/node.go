package domain

import (
	"fmt"

	"github.com/cyanstack/cyanstack/internal/config"
	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
)

// Node represents a running plugin instance.
type Node struct {
	ID         string
	ProjectID  string
	RevisionID string

	// Plugin configuration
	PluginConfig *config.PluginConfig

	// Docker image to use
	Image string

	// Runtime state
	ContainerID string
	Status      NodeStatus

	// Assigned resources
	AssignedPort int
	DataPath     string

	// Environment variables (resolved)
	ResolvedEnv map[string]string
}

// NodeStatus represents the lifecycle state of a node.
type NodeStatus string

const (
	NodeStatusPending   NodeStatus = "pending"
	NodeStatusPulling   NodeStatus = "pulling"
	NodeStatusStarting  NodeStatus = "starting"
	NodeStatusRunning   NodeStatus = "running"
	NodeStatusHealthy   NodeStatus = "healthy"
	NodeStatusUnhealthy NodeStatus = "unhealthy"
	NodeStatusStopping  NodeStatus = "stopping"
	NodeStatusStopped   NodeStatus = "stopped"
	NodeStatusFailed    NodeStatus = "failed"
)

// ToContainerConfig converts a Node to ContainerConfig for the driver.
func (n *Node) ToContainerConfig() interfaces.ContainerConfig {
	ports := make(map[string]string)
	if n.AssignedPort > 0 {
		// Map assigned host port to container port 8080 (default)
		ports[fmt.Sprintf("%d", n.AssignedPort)] = "8080"
	}

	volumes := make(map[string]string)
	if n.DataPath != "" {
		volumes[n.DataPath] = "/data"
	}

	labels := map[string]string{
		"cyanstack.managed": "true",
		"cyanstack.project": n.ProjectID,
		"cyanstack.node":    n.ID,
	}
	if n.RevisionID != "" {
		labels["cyanstack.revision"] = n.RevisionID
	}
	if n.PluginConfig != nil {
		labels["cyanstack.plugin"] = n.PluginConfig.ID
	}

	var command []string
	if n.PluginConfig != nil {
		command = n.PluginConfig.Lifecycle.StartCommand
	}

	return interfaces.ContainerConfig{
		Image:   n.Image,
		Name:    n.ID,
		Env:     n.ResolvedEnv,
		Ports:   ports,
		Volumes: volumes,
		Labels:  labels,
		Command: command,
	}
}
