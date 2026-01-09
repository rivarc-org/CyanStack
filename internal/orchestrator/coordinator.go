package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/cyanstack/cyanstack/internal/drivers/local"
)

// RevisionStore defines the interface for revision retrieval
type RevisionStore interface {
	GetByID(ctx context.Context, id string) (*RevisionData, error)
}

// RevisionData represents the data needed from a revision
type RevisionData struct {
	ID         string
	ProjectID  string
	GraphState string // JSON
}

// ProjectStore defines the interface for project retrieval
type ProjectStore interface {
	GetByID(ctx context.Context, id string) (*ProjectData, error)
}

// ProjectData represents the data needed from a project
type ProjectData struct {
	ID   string
	Name string
}

// Coordinator orchestrates the deployment of a revision
type Coordinator struct {
	driver         interfaces.RuntimeDriver
	networkManager *local.NetworkManager
	traefikGen     *local.TraefikGenerator

	dagResolver *DAGResolver
	envResolver *EnvResolver

	revisionStore RevisionStore
	projectStore  ProjectStore

	startupTimeout time.Duration
}

// CoordinatorConfig configures the Coordinator
type CoordinatorConfig struct {
	Driver         interfaces.RuntimeDriver
	NetworkManager *local.NetworkManager
	TraefikGen     *local.TraefikGenerator
	DAGResolver    *DAGResolver
	EnvResolver    *EnvResolver
	RevisionStore  RevisionStore
	ProjectStore   ProjectStore
	StartupTimeout time.Duration
}

// NewCoordinator creates a new deployment coordinator
func NewCoordinator(cfg CoordinatorConfig) *Coordinator {
	if cfg.StartupTimeout == 0 {
		cfg.StartupTimeout = 2 * time.Minute
	}

	return &Coordinator{
		driver:         cfg.Driver,
		networkManager: cfg.NetworkManager,
		traefikGen:     cfg.TraefikGen,
		dagResolver:    cfg.DAGResolver,
		envResolver:    cfg.EnvResolver,
		revisionStore:  cfg.RevisionStore,
		projectStore:   cfg.ProjectStore,
		startupTimeout: cfg.StartupTimeout,
	}
}

// Deploy executes a full deployment of a revision
func (c *Coordinator) Deploy(ctx context.Context, revisionID string) (*DeployResult, error) {
	// 1. Load revision from database
	revision, err := c.revisionStore.GetByID(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("loading revision: %w", err)
	}
	if revision == nil {
		return nil, fmt.Errorf("revision %s not found", revisionID)
	}

	// 2. Load project for name
	project, err := c.projectStore.GetByID(ctx, revision.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("loading project: %w", err)
	}
	if project == nil {
		return nil, fmt.Errorf("project %s not found", revision.ProjectID)
	}

	// 3. Parse graph state
	var graph GraphState
	if err := json.Unmarshal([]byte(revision.GraphState), &graph); err != nil {
		return nil, fmt.Errorf("parsing graph state: %w", err)
	}

	// 4. Validate graph
	if err := c.dagResolver.ValidateGraph(graph); err != nil {
		return nil, fmt.Errorf("invalid graph: %w", err)
	}

	// 5. Resolve DAG into deployment tiers
	plan, err := c.dagResolver.Resolve(graph)
	if err != nil {
		return nil, fmt.Errorf("resolving DAG: %w", err)
	}

	// Handle empty graph
	if len(plan.Tiers) == 0 {
		return &DeployResult{Success: true, ContainerIDs: make(map[string]string)}, nil
	}

	// 6. Initialize runtime context
	runtimeCtx := NewRuntimeContext(project.ID, project.Name, revision.ID)

	// 7. Ensure project network exists
	projectNetwork, err := c.networkManager.EnsureProjectNetwork(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("ensuring project network: %w", err)
	}

	// Also ensure gateway network exists for exposed services
	if err := c.networkManager.EnsureGatewayNetwork(ctx); err != nil {
		return nil, fmt.Errorf("ensuring gateway network: %w", err)
	}

	// 8. Deploy tier by tier
	var startedNodes []string

	for _, tier := range plan.Tiers {
		if err := c.deployTier(ctx, tier, runtimeCtx, projectNetwork, &startedNodes); err != nil {
			// Return error with context about what was started
			if depErr, ok := err.(*DeploymentError); ok {
				depErr.StartedNodes = startedNodes
				return &DeployResult{
					Success:      false,
					ContainerIDs: runtimeCtx.ContainerIDs,
					Errors:       []error{depErr},
				}, depErr
			}
			return nil, err
		}
	}

	return &DeployResult{
		Success:      true,
		ContainerIDs: runtimeCtx.ContainerIDs,
	}, nil
}

// DeployDirect deploys a graph directly without database lookup
// Useful for testing
func (c *Coordinator) DeployDirect(
	ctx context.Context,
	projectID, projectName, revisionID string,
	graph GraphState,
) (*DeployResult, error) {
	// Validate graph
	if err := c.dagResolver.ValidateGraph(graph); err != nil {
		return nil, fmt.Errorf("invalid graph: %w", err)
	}

	// Resolve DAG into deployment tiers
	plan, err := c.dagResolver.Resolve(graph)
	if err != nil {
		return nil, fmt.Errorf("resolving DAG: %w", err)
	}

	// Handle empty graph
	if len(plan.Tiers) == 0 {
		return &DeployResult{Success: true, ContainerIDs: make(map[string]string)}, nil
	}

	// Initialize runtime context
	runtimeCtx := NewRuntimeContext(projectID, projectName, revisionID)

	// Ensure project network exists
	projectNetwork, err := c.networkManager.EnsureProjectNetwork(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("ensuring project network: %w", err)
	}

	// Also ensure gateway network exists for exposed services
	if err := c.networkManager.EnsureGatewayNetwork(ctx); err != nil {
		return nil, fmt.Errorf("ensuring gateway network: %w", err)
	}

	// Deploy tier by tier
	var startedNodes []string

	for _, tier := range plan.Tiers {
		if err := c.deployTier(ctx, tier, runtimeCtx, projectNetwork, &startedNodes); err != nil {
			if depErr, ok := err.(*DeploymentError); ok {
				depErr.StartedNodes = startedNodes
				return &DeployResult{
					Success:      false,
					ContainerIDs: runtimeCtx.ContainerIDs,
					Errors:       []error{depErr},
				}, depErr
			}
			return nil, err
		}
	}

	return &DeployResult{
		Success:      true,
		ContainerIDs: runtimeCtx.ContainerIDs,
	}, nil
}

// deployTier deploys all nodes in a tier concurrently
func (c *Coordinator) deployTier(
	ctx context.Context,
	tier DeploymentTier,
	runtimeCtx *RuntimeContext,
	projectNetwork string,
	startedNodes *[]string,
) error {
	if len(tier.Nodes) == 0 {
		return nil
	}

	// Single node: deploy synchronously
	if len(tier.Nodes) == 1 {
		if err := c.deployNode(ctx, tier.Nodes[0], tier.Level, runtimeCtx, projectNetwork); err != nil {
			return err
		}
		*startedNodes = append(*startedNodes, tier.Nodes[0].ID)
		return nil
	}

	// Multiple nodes: deploy concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, len(tier.Nodes))
	var mu sync.Mutex

	for _, node := range tier.Nodes {
		wg.Add(1)
		go func(n NodeState) {
			defer wg.Done()
			if err := c.deployNode(ctx, n, tier.Level, runtimeCtx, projectNetwork); err != nil {
				errChan <- err
				return
			}
			mu.Lock()
			*startedNodes = append(*startedNodes, n.ID)
			mu.Unlock()
		}(node)
	}

	wg.Wait()
	close(errChan)

	// Check for errors (fail fast on first error)
	for err := range errChan {
		return err
	}

	return nil
}

// deployNode deploys a single node
func (c *Coordinator) deployNode(
	ctx context.Context,
	node NodeState,
	tierLevel int,
	runtimeCtx *RuntimeContext,
	projectNetwork string,
) error {
	// 1. Resolve environment variables
	env, err := c.envResolver.ResolveNodeEnv(ctx, node, runtimeCtx)
	if err != nil {
		return &DeploymentError{
			Phase:     "resolve",
			TierLevel: tierLevel,
			NodeID:    node.ID,
			Cause:     err,
		}
	}

	// 2. Build container configuration
	containerConfig := c.buildContainerConfig(node, env, runtimeCtx, projectNetwork)

	// 3. Start container
	containerID, err := c.driver.Run(ctx, containerConfig)
	if err != nil {
		return &DeploymentError{
			Phase:     "start",
			TierLevel: tierLevel,
			NodeID:    node.ID,
			Cause:     err,
		}
	}

	runtimeCtx.ContainerIDs[node.ID] = containerID

	// 4. Wait for container to be running (Phase 3: no deep health checks)
	// Just verify the container started successfully
	startCtx, cancel := context.WithTimeout(ctx, c.startupTimeout)
	defer cancel()

	if err := c.waitForRunning(startCtx, containerID); err != nil {
		return &DeploymentError{
			Phase:     "healthcheck",
			TierLevel: tierLevel,
			NodeID:    node.ID,
			Cause:     err,
		}
	}

	// 5. Run lifecycle hooks (migration) if defined
	if err := c.runLifecycleHooks(ctx, node, containerID, runtimeCtx); err != nil {
		return &DeploymentError{
			Phase:     "hook",
			TierLevel: tierLevel,
			NodeID:    node.ID,
			Cause:     err,
		}
	}

	return nil
}

// waitForRunning waits for a container to reach running status
func (c *Coordinator) waitForRunning(ctx context.Context, containerID string) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for container to start: %w", ctx.Err())
		case <-ticker.C:
			state, err := c.driver.Inspect(ctx, containerID)
			if err != nil {
				return fmt.Errorf("inspecting container: %w", err)
			}

			switch state.Status {
			case interfaces.StatusRunning:
				return nil
			case interfaces.StatusExited, interfaces.StatusDead:
				return fmt.Errorf("container exited with status: %s (exit code: %d)", state.Status, state.ExitCode)
			}
			// Continue waiting for created, restarting states
		}
	}
}

// buildContainerConfig creates the container configuration for a node
func (c *Coordinator) buildContainerConfig(
	node NodeState,
	env map[string]string,
	runtimeCtx *RuntimeContext,
	projectNetwork string,
) interfaces.ContainerConfig {

	// Container name: cyan-{projectID}-{nodeID}
	containerName := fmt.Sprintf("cyan-%s-%s", runtimeCtx.ProjectID, node.ID)

	// Port mapping
	ports := make(map[string]string)
	if portAssign, ok := runtimeCtx.AssignedPorts[node.ID]; ok && portAssign.HostPort > 0 {
		// Map host port to container port
		hostPort := fmt.Sprintf("%d", portAssign.HostPort)
		containerPort := fmt.Sprintf("%d", portAssign.ContainerPort)
		ports[hostPort] = containerPort
	}

	// Labels for management
	labels := map[string]string{
		"cyanstack.managed":  "true",
		"cyanstack.project":  runtimeCtx.ProjectID,
		"cyanstack.revision": runtimeCtx.RevisionID,
		"cyanstack.node":     node.ID,
		"cyanstack.plugin":   node.PluginID,
	}

	// Check if this node has exports (should be exposed via Traefik)
	pluginConfig := runtimeCtx.PluginConfigs[node.PluginID]
	hasExports := pluginConfig != nil && len(pluginConfig.Exports) > 0

	// Add Traefik labels if this node has exports
	if hasExports && c.traefikGen != nil {
		containerPort := 8080 // Default
		if portAssign, ok := runtimeCtx.AssignedPorts[node.ID]; ok {
			containerPort = portAssign.ContainerPort
		}

		traefikConfig := local.TraefikConfig{
			ServiceName: fmt.Sprintf("%s-%s", runtimeCtx.ProjectName, node.ID),
			Host:        fmt.Sprintf("%s.%s.localhost", node.ID, runtimeCtx.ProjectName),
			Port:        containerPort,
			TLS:         false,
		}
		traefikLabels := c.traefikGen.GenerateLabels(traefikConfig)
		labels = c.traefikGen.MergeLabels(labels, traefikLabels)
	}

	// Determine networks
	networks := []string{projectNetwork}
	if hasExports {
		// Also join gateway network for Traefik routing
		networks = append(networks, local.GatewayNetworkName)
	}

	// Network aliases for DNS resolution
	networkAliases := map[string][]string{
		projectNetwork: {
			node.ID,                        // Primary: node ID
			shortPluginName(node.PluginID), // Secondary: short plugin name
		},
	}

	// Get start command from plugin config
	var command []string
	if pluginConfig != nil {
		command = pluginConfig.Lifecycle.StartCommand
	}

	return interfaces.ContainerConfig{
		Image:          node.Image,
		Name:           containerName,
		Env:            env,
		Ports:          ports,
		Labels:         labels,
		Command:        command,
		Networks:       networks,
		NetworkAliases: networkAliases,
		RestartPolicy:  "unless-stopped",
	}
}

// runLifecycleHooks executes migration hooks
func (c *Coordinator) runLifecycleHooks(
	ctx context.Context,
	node NodeState,
	containerID string,
	runtimeCtx *RuntimeContext,
) error {
	pluginConfig := runtimeCtx.PluginConfigs[node.PluginID]
	if pluginConfig == nil {
		return nil
	}

	// Run migration if defined
	if pluginConfig.Lifecycle.Migration != nil {
		timeout := pluginConfig.Lifecycle.Migration.Timeout.Duration
		if timeout == 0 {
			timeout = 2 * time.Minute
		}

		hookCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		exitCode, err := c.driver.Exec(hookCtx, containerID, pluginConfig.Lifecycle.Migration.Command)
		if err != nil {
			return fmt.Errorf("running migration: %w", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("migration exited with code %d", exitCode)
		}
	}

	return nil
}

// shortPluginName extracts a short name from plugin ID
// e.g., "com.cyanstack.postgres" -> "postgres"
func shortPluginName(pluginID string) string {
	parts := strings.Split(pluginID, ".")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return pluginID
}

// Stop stops all containers for a project
func (c *Coordinator) StopProject(ctx context.Context, projectID string) error {
	containers, err := c.driver.List(ctx)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	var lastErr error
	for _, container := range containers {
		// Check if this container belongs to the project
		// Container names are formatted as cyan-{projectID}-{nodeID}
		expectedPrefix := fmt.Sprintf("/cyan-%s-", projectID)
		if strings.HasPrefix(container.Name, expectedPrefix) {
			if err := c.driver.Stop(ctx, container.ID); err != nil {
				lastErr = err
				continue
			}
			if err := c.driver.Remove(ctx, container.ID); err != nil {
				lastErr = err
			}
		}
	}

	return lastErr
}
