package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
		return &DeployResult{
			Success:      true,
			ContainerIDs: make(map[string]string),
			Actions:      make(map[string]DeployAction),
		}, nil
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
		Actions:      runtimeCtx.Actions,
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
		return &DeployResult{
			Success:      true,
			ContainerIDs: make(map[string]string),
			Actions:      make(map[string]DeployAction),
		}, nil
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
		Actions:      runtimeCtx.Actions,
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

// deployNode deploys a single node with smart update support
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

	// 3. Compute config hash for idempotency
	pluginConfig := runtimeCtx.PluginConfigs[node.PluginID]
	var command []string
	if pluginConfig != nil {
		command = pluginConfig.Lifecycle.StartCommand
	}
	configHash := ComputeConfigHash(node.Image, env, command)
	containerConfig.Labels[ConfigHashLabel] = configHash

	// 4. Check for existing container
	existing, err := c.driver.GetContainer(ctx, containerConfig.Name)

	var action DeployAction
	var containerID string
	var runPostInstall bool

	if errors.Is(err, interfaces.ErrContainerNotFound) {
		// Case A: Fresh container creation
		action = ActionCreated
		runPostInstall = true

		containerID, err = c.driver.Run(ctx, containerConfig)
		if err != nil {
			return &DeploymentError{
				Phase:     "start",
				TierLevel: tierLevel,
				NodeID:    node.ID,
				Cause:     err,
			}
		}
	} else if err != nil {
		return &DeploymentError{
			Phase:     "inspect",
			TierLevel: tierLevel,
			NodeID:    node.ID,
			Cause:     err,
		}
	} else {
		// Container exists - check config hash
		existingHash := existing.Labels[ConfigHashLabel]

		if existingHash == configHash {
			// Case B: Unchanged - container is up to date
			action = ActionUnchanged
			containerID = existing.ID
			runtimeCtx.ContainerIDs[node.ID] = containerID
			runtimeCtx.Actions[node.ID] = action

			// Populate port info from existing container for dependent nodes
			c.updateRuntimePortsFromContainer(runtimeCtx, node.ID, existing)

			return nil
		}

		// Case C: Update needed - config has changed
		action = ActionUpdated
		runPostInstall = false // No post_install on updates, only migration

		// Stop and remove old container
		if stopErr := c.driver.Stop(ctx, existing.ID); stopErr != nil {
			// Log but continue - container might already be stopped
		}
		if removeErr := c.driver.Remove(ctx, existing.ID); removeErr != nil {
			// Log but continue
		}

		containerID, err = c.driver.Run(ctx, containerConfig)
		if err != nil {
			return &DeploymentError{
				Phase:     "start",
				TierLevel: tierLevel,
				NodeID:    node.ID,
				Cause:     err,
			}
		}
	}

	runtimeCtx.ContainerIDs[node.ID] = containerID
	runtimeCtx.Actions[node.ID] = action

	// 5. Wait for container to be running
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

	// 6. Wait for service health check (TCP/HTTP on assigned port)
	if portAssign, ok := runtimeCtx.AssignedPorts[node.ID]; ok && portAssign.ContainerPort > 0 {
		healthConfig := interfaces.HealthCheckConfig{
			ContainerPort: portAssign.ContainerPort,
			Path:          "", // Default to TCP check
			Timeout:       60 * time.Second,
		}
		// If plugin specifies an HTTP health check path, use it
		if pluginConfig != nil && pluginConfig.Lifecycle.HealthCheck.Path != "" {
			healthConfig.Path = pluginConfig.Lifecycle.HealthCheck.Path
		}
		if err := c.driver.WaitForService(startCtx, containerID, healthConfig); err != nil {
			return &DeploymentError{
				Phase:     "healthcheck",
				TierLevel: tierLevel,
				NodeID:    node.ID,
				Cause:     err,
			}
		}
	}

	// 7. Run lifecycle hooks (post_install and/or migration)
	if err := c.runLifecycleHooks(ctx, node, containerID, runtimeCtx, runPostInstall); err != nil {
		return &DeploymentError{
			Phase:     "hook",
			TierLevel: tierLevel,
			NodeID:    node.ID,
			Cause:     err,
		}
	}

	return nil
}

// updateRuntimePortsFromContainer populates RuntimeContext ports from a running container
func (c *Coordinator) updateRuntimePortsFromContainer(runtimeCtx *RuntimeContext, nodeID string, container *interfaces.ContainerState) {
	for portKey, hostPort := range container.PortBindings {
		// portKey is like "5432/tcp"
		port, _ := strconv.Atoi(strings.Split(portKey, "/")[0])
		hostPortInt, _ := strconv.Atoi(hostPort)
		runtimeCtx.AssignedPorts[nodeID] = PortAssignment{
			HostPort:      hostPortInt,
			ContainerPort: port,
		}
		break // Only need the first port
	}
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

	// Port mapping: containerPort -> "" for ephemeral host port binding
	ports := make(map[string]string)
	if portAssign, ok := runtimeCtx.AssignedPorts[node.ID]; ok && portAssign.ContainerPort > 0 {
		containerPort := fmt.Sprintf("%d", portAssign.ContainerPort)
		ports[containerPort] = "" // Empty string signals ephemeral port allocation
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

// runLifecycleHooks executes post_install and migration hooks
func (c *Coordinator) runLifecycleHooks(
	ctx context.Context,
	node NodeState,
	containerID string,
	runtimeCtx *RuntimeContext,
	runPostInstall bool,
) error {
	pluginConfig := runtimeCtx.PluginConfigs[node.PluginID]
	if pluginConfig == nil {
		return nil
	}

	// Run post_install only on fresh creation
	if runPostInstall && pluginConfig.Lifecycle.PostInstall != nil {
		timeout := pluginConfig.Lifecycle.PostInstall.Timeout.Duration
		if timeout == 0 {
			timeout = 2 * time.Minute
		}

		hookCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var output bytes.Buffer
		exitCode, err := c.driver.Exec(hookCtx, containerID, pluginConfig.Lifecycle.PostInstall.Command, &output)
		if err != nil {
			return fmt.Errorf("running post_install: %w", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("post_install exited with code %d: %s", exitCode, output.String())
		}
	}

	// Run migration on both create and update
	if pluginConfig.Lifecycle.Migration != nil {
		timeout := pluginConfig.Lifecycle.Migration.Timeout.Duration
		if timeout == 0 {
			timeout = 2 * time.Minute
		}

		hookCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var output bytes.Buffer
		exitCode, err := c.driver.Exec(hookCtx, containerID, pluginConfig.Lifecycle.Migration.Command, &output)
		if err != nil {
			return fmt.Errorf("running migration: %w", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("migration exited with code %d: %s", exitCode, output.String())
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
