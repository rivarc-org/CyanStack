package orchestrator

import (
	"testing"
)

func TestDAGResolver_EmptyGraph(t *testing.T) {
	resolver := NewDAGResolver()
	graph := GraphState{Nodes: nil}

	plan, err := resolver.Resolve(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	if len(plan.Tiers) != 0 {
		t.Errorf("expected 0 tiers, got %d", len(plan.Tiers))
	}
}

func TestDAGResolver_SingleNode(t *testing.T) {
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest", Inputs: map[string]string{}},
		},
	}

	plan, err := resolver.Resolve(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tiers) != 1 {
		t.Errorf("expected 1 tier, got %d", len(plan.Tiers))
	}
	if len(plan.Tiers[0].Nodes) != 1 {
		t.Errorf("expected 1 node in tier 0, got %d", len(plan.Tiers[0].Nodes))
	}
	if plan.Tiers[0].Nodes[0].ID != "node-a" {
		t.Errorf("expected node-a, got %s", plan.Tiers[0].Nodes[0].ID)
	}
}

func TestDAGResolver_LinearChain(t *testing.T) {
	// A -> B -> C (linear dependency chain)
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-c", PluginID: "com.test.c", Image: "test:latest", Inputs: map[string]string{"dep": "node-b"}},
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest", Inputs: map[string]string{}},
			{ID: "node-b", PluginID: "com.test.b", Image: "test:latest", Inputs: map[string]string{"dep": "node-a"}},
		},
	}

	plan, err := resolver.Resolve(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(plan.Tiers))
	}

	// Tier 0: node-a (no dependencies)
	if len(plan.Tiers[0].Nodes) != 1 || plan.Tiers[0].Nodes[0].ID != "node-a" {
		t.Errorf("tier 0 should contain only node-a, got %v", plan.Tiers[0].Nodes)
	}

	// Tier 1: node-b (depends on node-a)
	if len(plan.Tiers[1].Nodes) != 1 || plan.Tiers[1].Nodes[0].ID != "node-b" {
		t.Errorf("tier 1 should contain only node-b, got %v", plan.Tiers[1].Nodes)
	}

	// Tier 2: node-c (depends on node-b)
	if len(plan.Tiers[2].Nodes) != 1 || plan.Tiers[2].Nodes[0].ID != "node-c" {
		t.Errorf("tier 2 should contain only node-c, got %v", plan.Tiers[2].Nodes)
	}
}

func TestDAGResolver_IndependentNodes(t *testing.T) {
	// A, B, C all independent - should all be in tier 0
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest", Inputs: map[string]string{}},
			{ID: "node-b", PluginID: "com.test.b", Image: "test:latest", Inputs: map[string]string{}},
			{ID: "node-c", PluginID: "com.test.c", Image: "test:latest", Inputs: map[string]string{}},
		},
	}

	plan, err := resolver.Resolve(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tiers) != 1 {
		t.Fatalf("expected 1 tier, got %d", len(plan.Tiers))
	}
	if len(plan.Tiers[0].Nodes) != 3 {
		t.Errorf("expected 3 nodes in tier 0, got %d", len(plan.Tiers[0].Nodes))
	}
}

func TestDAGResolver_DiamondDependency(t *testing.T) {
	// Diamond: A -> B, A -> C, B -> D, C -> D
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-d", PluginID: "com.test.d", Image: "test:latest", Inputs: map[string]string{"b": "node-b", "c": "node-c"}},
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest", Inputs: map[string]string{}},
			{ID: "node-b", PluginID: "com.test.b", Image: "test:latest", Inputs: map[string]string{"a": "node-a"}},
			{ID: "node-c", PluginID: "com.test.c", Image: "test:latest", Inputs: map[string]string{"a": "node-a"}},
		},
	}

	plan, err := resolver.Resolve(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(plan.Tiers))
	}

	// Tier 0: node-a
	if len(plan.Tiers[0].Nodes) != 1 || plan.Tiers[0].Nodes[0].ID != "node-a" {
		t.Errorf("tier 0 should contain only node-a")
	}

	// Tier 1: node-b and node-c (both depend only on node-a)
	if len(plan.Tiers[1].Nodes) != 2 {
		t.Errorf("tier 1 should contain 2 nodes, got %d", len(plan.Tiers[1].Nodes))
	}
	tier1IDs := make(map[string]bool)
	for _, n := range plan.Tiers[1].Nodes {
		tier1IDs[n.ID] = true
	}
	if !tier1IDs["node-b"] || !tier1IDs["node-c"] {
		t.Errorf("tier 1 should contain node-b and node-c")
	}

	// Tier 2: node-d
	if len(plan.Tiers[2].Nodes) != 1 || plan.Tiers[2].Nodes[0].ID != "node-d" {
		t.Errorf("tier 2 should contain only node-d")
	}
}

func TestDAGResolver_CycleDetection(t *testing.T) {
	// A -> B -> A (cycle)
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest", Inputs: map[string]string{"dep": "node-b"}},
			{ID: "node-b", PluginID: "com.test.b", Image: "test:latest", Inputs: map[string]string{"dep": "node-a"}},
		},
	}

	_, err := resolver.Resolve(graph)
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}

	cycleErr, ok := err.(*CycleError)
	if !ok {
		t.Fatalf("expected CycleError, got %T", err)
	}
	if len(cycleErr.Nodes) != 2 {
		t.Errorf("expected 2 nodes in cycle, got %d", len(cycleErr.Nodes))
	}
}

func TestDAGResolver_ThreeNodeCycle(t *testing.T) {
	// A -> B -> C -> A (3-node cycle)
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest", Inputs: map[string]string{"dep": "node-c"}},
			{ID: "node-b", PluginID: "com.test.b", Image: "test:latest", Inputs: map[string]string{"dep": "node-a"}},
			{ID: "node-c", PluginID: "com.test.c", Image: "test:latest", Inputs: map[string]string{"dep": "node-b"}},
		},
	}

	_, err := resolver.Resolve(graph)
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}

	_, ok := err.(*CycleError)
	if !ok {
		t.Fatalf("expected CycleError, got %T", err)
	}
}

func TestDAGResolver_MixedDependencies(t *testing.T) {
	// A (no deps), B depends on A, C (no deps), D depends on B and C
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest", Inputs: map[string]string{}},
			{ID: "node-b", PluginID: "com.test.b", Image: "test:latest", Inputs: map[string]string{"dep": "node-a"}},
			{ID: "node-c", PluginID: "com.test.c", Image: "test:latest", Inputs: map[string]string{}},
			{ID: "node-d", PluginID: "com.test.d", Image: "test:latest", Inputs: map[string]string{"b": "node-b", "c": "node-c"}},
		},
	}

	plan, err := resolver.Resolve(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(plan.Tiers))
	}

	// Tier 0: node-a and node-c
	if len(plan.Tiers[0].Nodes) != 2 {
		t.Errorf("tier 0 should have 2 nodes, got %d", len(plan.Tiers[0].Nodes))
	}

	// Tier 1: node-b
	if len(plan.Tiers[1].Nodes) != 1 || plan.Tiers[1].Nodes[0].ID != "node-b" {
		t.Errorf("tier 1 should contain only node-b")
	}

	// Tier 2: node-d
	if len(plan.Tiers[2].Nodes) != 1 || plan.Tiers[2].Nodes[0].ID != "node-d" {
		t.Errorf("tier 2 should contain only node-d")
	}
}

func TestDAGResolver_ValidateGraph_DuplicateID(t *testing.T) {
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest"},
			{ID: "node-a", PluginID: "com.test.b", Image: "test:latest"}, // Duplicate!
		},
	}

	err := resolver.ValidateGraph(graph)
	if err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}
}

func TestDAGResolver_ValidateGraph_EmptyID(t *testing.T) {
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "", PluginID: "com.test.a", Image: "test:latest"},
		},
	}

	err := resolver.ValidateGraph(graph)
	if err == nil {
		t.Fatal("expected error for empty ID, got nil")
	}
}

func TestDAGResolver_ValidateGraph_EmptyPluginID(t *testing.T) {
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "", Image: "test:latest"},
		},
	}

	err := resolver.ValidateGraph(graph)
	if err == nil {
		t.Fatal("expected error for empty plugin_id, got nil")
	}
}

func TestDAGResolver_ValidateGraph_EmptyImage(t *testing.T) {
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "com.test.a", Image: ""},
		},
	}

	err := resolver.ValidateGraph(graph)
	if err == nil {
		t.Fatal("expected error for empty image, got nil")
	}
}

func TestDAGResolver_LiteralInputValues(t *testing.T) {
	// Inputs that are literal values (not node references) should not create dependencies
	resolver := NewDAGResolver()
	graph := GraphState{
		Nodes: []NodeState{
			{ID: "node-a", PluginID: "com.test.a", Image: "test:latest", Inputs: map[string]string{
				"password": "secretpassword", // literal value
				"port":     "5432",           // literal value
			}},
		},
	}

	plan, err := resolver.Resolve(graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tiers) != 1 {
		t.Errorf("expected 1 tier, got %d", len(plan.Tiers))
	}
}
