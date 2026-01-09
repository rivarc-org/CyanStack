package interfaces

import "time"

// ContainerConfig defines the configuration for running a container
type ContainerConfig struct {
	Image   string
	Name    string
	Env     map[string]string
	Ports   map[string]string // container:host mapping, e.g. "80" -> "8080" or "5432" -> "" (ephemeral)
	Volumes map[string]string // host:container mapping, e.g. "/host/path" -> "/container/path"
	Labels  map[string]string

	// Extended configuration
	Command       []string // Override entrypoint command
	WorkDir       string   // Working directory inside container
	RestartPolicy string   // "no", "always", "on-failure", "unless-stopped"

	// Networking configuration
	Network        string              // Deprecated: Use Networks instead. Kept for backward compatibility.
	Networks       []string            // Networks to attach to (can attach to multiple)
	NetworkAliases map[string][]string // Network name -> aliases mapping for DNS resolution
}

// ResourceRequirements defines resource constraints for scheduling
type ResourceRequirements struct {
	CPUCores float64 // Number of CPU cores needed
	MemoryMB int64   // Memory in megabytes
	DiskMB   int64   // Disk space in megabytes
	GPUCount int     // Number of GPUs needed (future)
}

// ContainerState represents the current state of a container
type ContainerState struct {
	ID           string
	Name         string
	Status       ContainerStatus
	StartedAt    time.Time
	ExitCode     int
	Error        string
	PortBindings map[string]string // containerPort/tcp -> hostPort, e.g. "5432/tcp" -> "32789"
	Labels       map[string]string // Container labels for config hash and management
}

// ContainerStatus represents the lifecycle state of a container
type ContainerStatus string

const (
	StatusCreated    ContainerStatus = "created"
	StatusRunning    ContainerStatus = "running"
	StatusPaused     ContainerStatus = "paused"
	StatusRestarting ContainerStatus = "restarting"
	StatusExited     ContainerStatus = "exited"
	StatusRemoving   ContainerStatus = "removing"
	StatusDead       ContainerStatus = "dead"
)

// DriverInfo provides metadata about a driver implementation
type DriverInfo struct {
	Name        string // "local", "aws-ecs", "kubernetes"
	Version     string
	MaxParallel int // Maximum concurrent startup operations
}

// HealthCheckConfig defines how to health check a service
type HealthCheckConfig struct {
	ContainerPort int           // The container port to check (e.g., 5432)
	Path          string        // HTTP path for HTTP checks (empty = TCP only)
	Timeout       time.Duration // Total timeout (default 60s)
	Interval      time.Duration // Check interval (default 1s)
}
