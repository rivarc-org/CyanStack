package interfaces

import (
	"context"
	"io"
	"time"
)

// RuntimeDriver defines the interface for container runtime operations.
// This interface enables swapping between local Docker and future cloud drivers.
type RuntimeDriver interface {
	// CanSchedule checks if the driver can accommodate the resource requirements.
	// Used to determine if a container can be started before attempting.
	CanSchedule(ctx context.Context, requirements ResourceRequirements) bool

	// Run starts a new container with the given configuration.
	// The semaphore is acquired during startup operations (pull, create, start)
	// and released once the container is running.
	// Returns the container ID on success.
	Run(ctx context.Context, conf ContainerConfig) (string, error)

	// Stop gracefully stops a running container.
	Stop(ctx context.Context, containerID string) error

	// Remove removes a stopped container.
	Remove(ctx context.Context, containerID string) error

	// StreamLogs returns a reader for container logs.
	// The caller is responsible for closing the reader.
	StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error)

	// Inspect returns the current state of a container.
	Inspect(ctx context.Context, containerID string) (*ContainerState, error)

	// List returns all containers managed by this driver.
	List(ctx context.Context) ([]ContainerState, error)

	// Exec executes a command inside a running container.
	// Used for lifecycle hooks like post_install and migration.
	// Returns the exit code.
	Exec(ctx context.Context, containerID string, cmd []string) (int, error)

	// WaitForHealthy waits for a container to pass health checks.
	WaitForHealthy(ctx context.Context, containerID string, timeout time.Duration) error
}

// RuntimeDriverWithInfo extends RuntimeDriver with metadata.
type RuntimeDriverWithInfo interface {
	RuntimeDriver
	Info() DriverInfo
}
