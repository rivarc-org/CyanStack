package local

import (
	"fmt"

	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
)

const (
	// TraefikContainerName is the name of the Traefik container
	TraefikContainerName = "cyan-traefik"
	// TraefikImage is the Docker image for Traefik
	TraefikImage = "traefik:v3"
)

// TraefikConfig defines routing configuration for a service
type TraefikConfig struct {
	ServiceName string // Unique service identifier (e.g., "my-project-backend")
	Host        string // Host rule (e.g., "backend.my-project.localhost")
	Port        int    // Container port to route to
	PathPrefix  string // Optional path prefix (e.g., "/api")
	TLS         bool   // Enable HTTPS
}

// TraefikGenerator generates Traefik configuration
type TraefikGenerator struct{}

// NewTraefikGenerator creates a new Traefik generator
func NewTraefikGenerator() *TraefikGenerator {
	return &TraefikGenerator{}
}

// GenerateLabels creates Docker labels for Traefik routing
func (g *TraefikGenerator) GenerateLabels(config TraefikConfig) map[string]string {
	labels := map[string]string{
		"traefik.enable": "true",
	}

	// Router configuration
	routerName := config.ServiceName
	entrypoint := "web"
	if config.TLS {
		entrypoint = "websecure"
	}

	// Basic host rule
	rule := fmt.Sprintf("Host(`%s`)", config.Host)

	// Add path prefix if specified
	if config.PathPrefix != "" {
		rule = fmt.Sprintf("Host(`%s`) && PathPrefix(`%s`)", config.Host, config.PathPrefix)
	}

	labels[fmt.Sprintf("traefik.http.routers.%s.rule", routerName)] = rule
	labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", routerName)] = entrypoint

	// Service port configuration
	labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName)] = fmt.Sprintf("%d", config.Port)

	// TLS configuration
	if config.TLS {
		labels[fmt.Sprintf("traefik.http.routers.%s.tls", routerName)] = "true"
	}

	return labels
}

// GenerateHostname creates a hostname for a service in a project
// Format: {serviceName}.{projectName}.localhost
func (g *TraefikGenerator) GenerateHostname(projectName, serviceName string) string {
	return fmt.Sprintf("%s.%s.localhost", serviceName, projectName)
}

// GetTraefikContainerConfig returns the container configuration for running Traefik
func (g *TraefikGenerator) GetTraefikContainerConfig() interfaces.ContainerConfig {
	return interfaces.ContainerConfig{
		Image: TraefikImage,
		Name:  TraefikContainerName,
		Command: []string{
			"--api.insecure=true",
			"--providers.docker=true",
			"--providers.docker.exposedbydefault=false",
			"--providers.docker.network=" + GatewayNetworkName,
			"--entrypoints.web.address=:80",
			"--entrypoints.websecure.address=:443",
		},
		Ports: map[string]string{
			"80":   "80",   // HTTP
			"443":  "443",  // HTTPS
			"8080": "8080", // Traefik dashboard
		},
		Volumes: map[string]string{
			"/var/run/docker.sock": "/var/run/docker.sock",
		},
		Labels: map[string]string{
			"cyanstack.managed": "true",
			"cyanstack.type":    "traefik",
		},
		Networks:      []string{GatewayNetworkName},
		RestartPolicy: "unless-stopped",
	}
}

// MergeLabels merges Traefik labels with existing container labels
func (g *TraefikGenerator) MergeLabels(existing, traefikLabels map[string]string) map[string]string {
	if existing == nil {
		existing = make(map[string]string)
	}
	for k, v := range traefikLabels {
		existing[k] = v
	}
	return existing
}
