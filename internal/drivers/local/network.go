package local

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	// GatewayNetworkName is the shared network for Traefik and exposed containers
	GatewayNetworkName = "cyan-gateway"
	// ProjectNetworkPrefix is the prefix for project-specific networks
	ProjectNetworkPrefix = "cyan-proj-"
)

// NetworkManager manages Docker networks for CyanStack
type NetworkManager struct {
	client *client.Client
}

// NewNetworkManager creates a new network manager
func NewNetworkManager(cli *client.Client) *NetworkManager {
	return &NetworkManager{client: cli}
}

// EnsureGatewayNetwork creates the cyan-gateway network if it doesn't exist
func (m *NetworkManager) EnsureGatewayNetwork(ctx context.Context) error {
	return m.ensureNetwork(ctx, GatewayNetworkName, map[string]string{
		"cyanstack.managed": "true",
		"cyanstack.type":    "gateway",
	})
}

// EnsureProjectNetwork creates a project-specific network if it doesn't exist
// Returns the network name (cyan-proj-{projectID})
func (m *NetworkManager) EnsureProjectNetwork(ctx context.Context, projectID string) (string, error) {
	name := ProjectNetworkPrefix + projectID
	err := m.ensureNetwork(ctx, name, map[string]string{
		"cyanstack.managed": "true",
		"cyanstack.type":    "project",
		"cyanstack.project": projectID,
	})
	return name, err
}

// RemoveProjectNetwork removes a project-specific network
func (m *NetworkManager) RemoveProjectNetwork(ctx context.Context, projectID string) error {
	name := ProjectNetworkPrefix + projectID
	if err := m.client.NetworkRemove(ctx, name); err != nil {
		return fmt.Errorf("removing network %s: %w", name, err)
	}
	return nil
}

// GetNetworksForContainer returns the networks a container should join
// If hasExports is true, the container joins both gateway and project networks
// Otherwise, it only joins the project network
func (m *NetworkManager) GetNetworksForContainer(projectID string, hasExports bool) []string {
	projectNet := ProjectNetworkPrefix + projectID
	if hasExports {
		return []string{GatewayNetworkName, projectNet}
	}
	return []string{projectNet}
}

// ListProjectNetworks returns all project networks
func (m *NetworkManager) ListProjectNetworks(ctx context.Context) ([]network.Summary, error) {
	networks, err := m.client.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}

	var projectNetworks []network.Summary
	for _, n := range networks {
		if n.Labels["cyanstack.type"] == "project" {
			projectNetworks = append(projectNetworks, n)
		}
	}
	return projectNetworks, nil
}

// NetworkExists checks if a network exists
func (m *NetworkManager) NetworkExists(ctx context.Context, name string) (bool, error) {
	_, err := m.client.NetworkInspect(ctx, name, network.InspectOptions{})
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspecting network: %w", err)
	}
	return true, nil
}

// ensureNetwork creates a network if it doesn't exist
func (m *NetworkManager) ensureNetwork(ctx context.Context, name string, labels map[string]string) error {
	// Check if network already exists
	_, err := m.client.NetworkInspect(ctx, name, network.InspectOptions{})
	if err == nil {
		return nil // Network already exists
	}
	if !client.IsErrNotFound(err) {
		return fmt.Errorf("inspecting network: %w", err)
	}

	// Create the network
	_, err = m.client.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("creating network %s: %w", name, err)
	}

	return nil
}

// CleanupOrphanedNetworks removes project networks that have no connected containers
func (m *NetworkManager) CleanupOrphanedNetworks(ctx context.Context) error {
	networks, err := m.ListProjectNetworks(ctx)
	if err != nil {
		return err
	}

	for _, n := range networks {
		// Inspect to get connected containers
		info, err := m.client.NetworkInspect(ctx, n.ID, network.InspectOptions{})
		if err != nil {
			continue
		}

		// Remove if no containers are connected
		if len(info.Containers) == 0 {
			if err := m.client.NetworkRemove(ctx, n.ID); err != nil {
				// Log but don't fail on cleanup errors
				continue
			}
		}
	}

	return nil
}
