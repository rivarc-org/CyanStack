package interfaces

import "time"

// ContainerConfig defines the configuration for running a container
type ContainerConfig struct {
	Image   string
	Name    string
	Env     map[string]string
	Ports   map[string]string // host:container mapping, e.g. "8080" -> "80"
	Volumes map[string]string // host:container mapping, e.g. "/host/path" -> "/container/path"
	Labels  map[string]string

	// Extended configuration
	Command       []string // Override entrypoint command
	WorkDir       string   // Working directory inside container
	Network       string   // Network to attach to
	RestartPolicy string   // "no", "always", "on-failure", "unless-stopped"
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
	ID        string
	Name      string
	Status    ContainerStatus
	StartedAt time.Time
	ExitCode  int
	Error     string
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
