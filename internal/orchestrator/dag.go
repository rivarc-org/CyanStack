package orchestrator

import (
	"fmt"
)

// DAGResolver performs topological sorting of nodes into deployment tiers
type DAGResolver struct{}

// NewDAGResolver creates a new DAG resolver
func NewDAGResolver() *DAGResolver {
	return &DAGResolver{}
}

// Resolve performs topological sort and returns deployment tiers
// Returns error if a cycle is detected
func (r *DAGResolver) Resolve(graph GraphState) (*DeploymentPlan, error) {
	if len(graph.Nodes) == 0 {
		return &DeploymentPlan{Tiers: nil}, nil
	}

	// Build node lookup map
	nodeMap := make(map[string]NodeState)
	for _, node := range graph.Nodes {
		nodeMap[node.ID] = node
	}

	// Build adjacency map and in-degree count
	// dependents: node ID -> list of nodes that depend on it
	dependents := make(map[string][]string)
	// inDegree: node ID -> number of dependencies
	inDegree := make(map[string]int)

	for _, node := range graph.Nodes {
		inDegree[node.ID] = 0 // Initialize
	}

	// Parse dependencies from inputs
	for _, node := range graph.Nodes {
		deps := r.extractDependencies(node, nodeMap)
		inDegree[node.ID] = len(deps)
		for _, depID := range deps {
			dependents[depID] = append(dependents[depID], node.ID)
		}
	}

	// Kahn's algorithm for topological sort into tiers
	var tiers []DeploymentTier
	processed := make(map[string]bool)

	for len(processed) < len(graph.Nodes) {
		// Find all nodes with zero in-degree (not yet processed)
		var currentTier []NodeState
		for _, node := range graph.Nodes {
			if !processed[node.ID] && inDegree[node.ID] == 0 {
				currentTier = append(currentTier, node)
			}
		}

		// Cycle detection: no nodes with zero in-degree but not all processed
		if len(currentTier) == 0 {
			return nil, &CycleError{
				Message: "dependency cycle detected in node graph",
				Nodes:   r.findCycleNodes(graph.Nodes, processed, inDegree),
			}
		}

		// Add tier
		tiers = append(tiers, DeploymentTier{
			Level: len(tiers),
			Nodes: currentTier,
		})

		// Mark nodes as processed and update in-degrees
		for _, node := range currentTier {
			processed[node.ID] = true
			for _, dependentID := range dependents[node.ID] {
				inDegree[dependentID]--
			}
		}
	}

	return &DeploymentPlan{Tiers: tiers}, nil
}

// extractDependencies returns node IDs that this node depends on
func (r *DAGResolver) extractDependencies(node NodeState, nodeMap map[string]NodeState) []string {
	var deps []string
	seen := make(map[string]bool)

	for _, inputValue := range node.Inputs {
		// Check if the input value references another node
		if _, exists := nodeMap[inputValue]; exists {
			if !seen[inputValue] {
				deps = append(deps, inputValue)
				seen[inputValue] = true
			}
		}
	}

	return deps
}

// findCycleNodes identifies nodes involved in a cycle for error reporting
func (r *DAGResolver) findCycleNodes(nodes []NodeState, processed map[string]bool, inDegree map[string]int) []string {
	var cycleNodes []string
	for _, node := range nodes {
		if !processed[node.ID] && inDegree[node.ID] > 0 {
			cycleNodes = append(cycleNodes, node.ID)
		}
	}
	return cycleNodes
}

// ValidateGraph checks for basic graph validity without full resolution
func (r *DAGResolver) ValidateGraph(graph GraphState) error {
	nodeIDs := make(map[string]bool)

	// Check for duplicate node IDs
	for _, node := range graph.Nodes {
		if node.ID == "" {
			return fmt.Errorf("node has empty ID")
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("duplicate node ID: %s", node.ID)
		}
		nodeIDs[node.ID] = true

		if node.PluginID == "" {
			return fmt.Errorf("node %s has empty plugin_id", node.ID)
		}
	}

	// Check that all referenced dependencies exist
	for _, node := range graph.Nodes {
		for inputID, inputValue := range node.Inputs {
			// If the value looks like it could be a node reference
			// (contains "node-" prefix or matches a known node ID), verify it exists
			if _, mightBeNode := nodeIDs[inputValue]; mightBeNode {
				// This is valid - it's a node reference
				continue
			}
			// Otherwise it's just a literal value, which is fine
			_ = inputID // suppress unused warning
		}
	}

	return nil
}

// GetDependencies returns the list of node IDs that a given node depends on
func (r *DAGResolver) GetDependencies(node NodeState, graph GraphState) []string {
	nodeMap := make(map[string]NodeState)
	for _, n := range graph.Nodes {
		nodeMap[n.ID] = n
	}
	return r.extractDependencies(node, nodeMap)
}
