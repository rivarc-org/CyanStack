package orchestrator

import (
	"fmt"
	"strings"
)

// CycleError indicates a dependency cycle was detected
type CycleError struct {
	Message string
	Nodes   []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("%s: involved nodes: %s", e.Message, strings.Join(e.Nodes, ", "))
}

// DeploymentError wraps errors that occur during deployment
type DeploymentError struct {
	Phase        string   // "resolve", "start", "healthcheck", "hook"
	TierLevel    int
	NodeID       string
	Cause        error
	StartedNodes []string // Nodes that were successfully started
}

func (e *DeploymentError) Error() string {
	return fmt.Sprintf("deployment failed at phase=%s tier=%d node=%s: %v",
		e.Phase, e.TierLevel, e.NodeID, e.Cause)
}

func (e *DeploymentError) Unwrap() error {
	return e.Cause
}

// EnvResolutionError indicates environment resolution failed
type EnvResolutionError struct {
	NodeID    string
	InputID   string
	InputType string
	Cause     error
}

func (e *EnvResolutionError) Error() string {
	return fmt.Sprintf("failed to resolve %s input %s for node %s: %v",
		e.InputType, e.InputID, e.NodeID, e.Cause)
}

func (e *EnvResolutionError) Unwrap() error {
	return e.Cause
}

// PluginNotFoundError indicates a plugin configuration was not found
type PluginNotFoundError struct {
	PluginID string
}

func (e *PluginNotFoundError) Error() string {
	return fmt.Sprintf("plugin not found: %s", e.PluginID)
}

// DependencyNotFoundError indicates a dependency node was not found
type DependencyNotFoundError struct {
	NodeID       string
	DependencyID string
}

func (e *DependencyNotFoundError) Error() string {
	return fmt.Sprintf("node %s references unknown dependency: %s", e.NodeID, e.DependencyID)
}

// ExportNotFoundError indicates a required export was not found
type ExportNotFoundError struct {
	NodeID    string
	ExportKey string
}

func (e *ExportNotFoundError) Error() string {
	return fmt.Sprintf("node %s does not export: %s", e.NodeID, e.ExportKey)
}
