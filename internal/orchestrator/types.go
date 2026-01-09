package orchestrator

import (
	"github.com/cyanstack/cyanstack/internal/config"
)

// GraphState represents the serialized node graph stored in Revision.GraphState
type GraphState struct {
	Nodes []NodeState `json:"nodes"`
}

// NodeState represents a single node in the graph
type NodeState struct {
	ID       string            `json:"id"`        // Unique node identifier
	PluginID string            `json:"plugin_id"` // e.g., "com.cyanstack.postgres"
	Image    string            `json:"image"`     // Docker image (from graph_state, allows version overrides)
	Inputs   map[string]string `json:"inputs"`    // Input ID -> value or node reference
}

// RuntimeContext holds execution state during deployment
type RuntimeContext struct {
	ProjectID   string
	ProjectName string
	RevisionID  string

	// Node outputs indexed by node ID
	// Each node has a map of export key -> resolved value
	NodeOutputs map[string]map[string]string

	// Assigned ports indexed by node ID
	// Key: nodeID, Value: struct with host and container ports
	AssignedPorts map[string]PortAssignment

	// Container IDs indexed by node ID (for cleanup on failure)
	ContainerIDs map[string]string

	// Plugin configs indexed by plugin ID
	PluginConfigs map[string]*config.PluginConfig

	// Actions taken for each node during deployment
	Actions map[string]DeployAction
}

// PortAssignment holds the host and container port for a node
type PortAssignment struct {
	HostPort      int // Port on the host for external access
	ContainerPort int // Port inside the container (from default or same as host)
}

// DeployAction represents the action taken for a node during deployment
type DeployAction string

const (
	ActionCreated   DeployAction = "created"   // Fresh container creation
	ActionUpdated   DeployAction = "updated"   // Container recreated due to config change
	ActionUnchanged DeployAction = "unchanged" // Container already up-to-date
)

// NewRuntimeContext creates an initialized RuntimeContext
func NewRuntimeContext(projectID, projectName, revisionID string) *RuntimeContext {
	return &RuntimeContext{
		ProjectID:     projectID,
		ProjectName:   projectName,
		RevisionID:    revisionID,
		NodeOutputs:   make(map[string]map[string]string),
		AssignedPorts: make(map[string]PortAssignment),
		ContainerIDs:  make(map[string]string),
		PluginConfigs: make(map[string]*config.PluginConfig),
		Actions:       make(map[string]DeployAction),
	}
}

// SetNodeOutput stores a resolved export for a node
func (rc *RuntimeContext) SetNodeOutput(nodeID, key, value string) {
	if rc.NodeOutputs[nodeID] == nil {
		rc.NodeOutputs[nodeID] = make(map[string]string)
	}
	rc.NodeOutputs[nodeID][key] = value
}

// GetNodeOutput retrieves a resolved export from a node
func (rc *RuntimeContext) GetNodeOutput(nodeID, key string) (string, bool) {
	if outputs, ok := rc.NodeOutputs[nodeID]; ok {
		val, exists := outputs[key]
		return val, exists
	}
	return "", false
}

// DeploymentTier represents a group of nodes that can start in parallel
type DeploymentTier struct {
	Level int         // Tier level (0 = no dependencies)
	Nodes []NodeState // Nodes in this tier
}

// DeploymentPlan is the output of DAG resolution
type DeploymentPlan struct {
	Tiers []DeploymentTier
}

// DeployResult contains the result of a deployment
type DeployResult struct {
	Success      bool
	ContainerIDs map[string]string       // nodeID -> containerID
	Actions      map[string]DeployAction // nodeID -> action taken
	Errors       []error
}
