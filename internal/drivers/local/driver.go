package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
)

// Driver implements RuntimeDriver for local Docker.
type Driver struct {
	client    *client.Client
	semaphore *Semaphore
	dataRoot  string // ~/.cyanstack/data
}

// DriverOption configures the Driver.
type DriverOption func(*Driver)

// WithConcurrencyLimit sets the maximum concurrent startup operations.
func WithConcurrencyLimit(limit int) DriverOption {
	return func(d *Driver) {
		d.semaphore = NewSemaphore(limit)
	}
}

// WithDataRoot sets the data directory root.
func WithDataRoot(path string) DriverOption {
	return func(d *Driver) {
		d.dataRoot = path
	}
}

// NewDriver creates a new local Docker driver.
func NewDriver(opts ...DriverOption) (*Driver, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	homeDir, _ := os.UserHomeDir()
	d := &Driver{
		client:    cli,
		semaphore: NewSemaphore(0), // Default to NumCPU
		dataRoot:  filepath.Join(homeDir, ".cyanstack", "data"),
	}

	for _, opt := range opts {
		opt(d)
	}

	return d, nil
}

// Info returns driver metadata.
func (d *Driver) Info() interfaces.DriverInfo {
	return interfaces.DriverInfo{
		Name:        "local",
		Version:     "1.0.0",
		MaxParallel: d.semaphore.Limit(),
	}
}

// CanSchedule checks if resources are available for startup.
func (d *Driver) CanSchedule(ctx context.Context, requirements interfaces.ResourceRequirements) bool {
	// For Phase 1, we only check semaphore availability
	// Future: Check actual system resources
	return d.semaphore.Available() > 0
}

// Run starts a container, acquiring semaphore during startup only.
func (d *Driver) Run(ctx context.Context, conf interfaces.ContainerConfig) (string, error) {
	// Acquire semaphore slot for startup operations
	if err := d.semaphore.Acquire(ctx); err != nil {
		return "", fmt.Errorf("acquiring semaphore: %w", err)
	}

	// Ensure we release the semaphore when startup completes (success or failure)
	var containerID string
	var runErr error

	defer func() {
		d.semaphore.Release()
	}()

	// Pull image if not present
	if err := d.ensureImage(ctx, conf.Image); err != nil {
		return "", fmt.Errorf("ensuring image: %w", err)
	}

	// Prepare container config
	containerConfig := &container.Config{
		Image:  conf.Image,
		Env:    mapToEnvSlice(conf.Env),
		Labels: conf.Labels,
	}

	if len(conf.Command) > 0 {
		containerConfig.Cmd = conf.Command
	}
	if conf.WorkDir != "" {
		containerConfig.WorkingDir = conf.WorkDir
	}

	// Prepare port bindings
	portBindings := nat.PortMap{}
	exposedPorts := nat.PortSet{}
	for hostPort, containerPort := range conf.Ports {
		port := nat.Port(containerPort + "/tcp")
		exposedPorts[port] = struct{}{}
		portBindings[port] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: hostPort},
		}
	}
	containerConfig.ExposedPorts = exposedPorts

	// Prepare mounts
	var mounts []mount.Mount
	for hostPath, containerPath := range conf.Volumes {
		// Ensure host directory exists
		if err := os.MkdirAll(hostPath, 0755); err != nil {
			return "", fmt.Errorf("creating volume directory %s: %w", hostPath, err)
		}
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: hostPath,
			Target: containerPath,
		})
	}

	// Host config
	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Mounts:       mounts,
	}

	if conf.RestartPolicy != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{
			Name: container.RestartPolicyMode(conf.RestartPolicy),
		}
	}

	// Create container
	resp, err := d.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, conf.Name)
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}
	containerID = resp.ID

	// Start container
	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		// Clean up created container on start failure
		d.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
		runErr = fmt.Errorf("starting container: %w", err)
		return "", runErr
	}

	return containerID, nil
}

// Stop gracefully stops a running container.
func (d *Driver) Stop(ctx context.Context, containerID string) error {
	timeout := 30 // seconds
	stopOptions := container.StopOptions{Timeout: &timeout}

	if err := d.client.ContainerStop(ctx, containerID, stopOptions); err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}

	return nil
}

// Remove removes a stopped container.
func (d *Driver) Remove(ctx context.Context, containerID string) error {
	return d.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		RemoveVolumes: false, // Preserve data
	})
}

// StreamLogs returns a reader for container logs.
func (d *Driver) StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
	}

	return d.client.ContainerLogs(ctx, containerID, options)
}

// Inspect returns container state.
func (d *Driver) Inspect(ctx context.Context, containerID string) (*interfaces.ContainerState, error) {
	info, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspecting container: %w", err)
	}

	state := &interfaces.ContainerState{
		ID:     info.ID,
		Name:   info.Name,
		Status: mapDockerStatus(info.State.Status),
	}

	if info.State.StartedAt != "" {
		state.StartedAt, _ = time.Parse(time.RFC3339Nano, info.State.StartedAt)
	}

	if info.State.ExitCode != 0 {
		state.ExitCode = info.State.ExitCode
	}

	if info.State.Error != "" {
		state.Error = info.State.Error
	}

	return state, nil
}

// List returns all containers with cyanstack labels.
func (d *Driver) List(ctx context.Context) ([]interfaces.ContainerState, error) {
	containers, err := d.client.ContainerList(ctx, container.ListOptions{
		All: true,
		// Future: Filter by cyanstack label
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	var states []interfaces.ContainerState
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		states = append(states, interfaces.ContainerState{
			ID:     c.ID,
			Name:   name,
			Status: mapDockerStatus(c.State),
		})
	}

	return states, nil
}

// Exec runs a command in a container.
func (d *Driver) Exec(ctx context.Context, containerID string, cmd []string) (int, error) {
	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return -1, fmt.Errorf("creating exec: %w", err)
	}

	if err := d.client.ContainerExecStart(ctx, execResp.ID, types.ExecStartCheck{}); err != nil {
		return -1, fmt.Errorf("starting exec: %w", err)
	}

	// Wait for completion and get exit code
	for {
		inspect, err := d.client.ContainerExecInspect(ctx, execResp.ID)
		if err != nil {
			return -1, fmt.Errorf("inspecting exec: %w", err)
		}
		if !inspect.Running {
			return inspect.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Continue polling
		}
	}
}

// WaitForHealthy waits for container health check to pass.
func (d *Driver) WaitForHealthy(ctx context.Context, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		info, err := d.client.ContainerInspect(ctx, containerID)
		if err != nil {
			return fmt.Errorf("inspecting container: %w", err)
		}

		// If container has health check configured and is healthy
		if info.State.Health != nil && info.State.Health.Status == "healthy" {
			return nil
		}

		// If no health check configured, just check if running
		if info.State.Health == nil && info.State.Status == "running" {
			return nil
		}

		if info.State.Status != "running" {
			return fmt.Errorf("container exited with status: %s", info.State.Status)
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("timeout waiting for healthy status")
}

// DataPath returns the data directory for a project/plugin.
func (d *Driver) DataPath(projectID, pluginID string) string {
	return filepath.Join(d.dataRoot, projectID, pluginID)
}

// ActiveStartups returns the number of currently active startup operations.
// This is the number of goroutines that have acquired the semaphore.
func (d *Driver) ActiveStartups() int {
	return d.semaphore.Active()
}

// StartupLimit returns the maximum concurrent startup operations.
func (d *Driver) StartupLimit() int {
	return d.semaphore.Limit()
}

// ensureImage pulls an image if not present locally.
func (d *Driver) ensureImage(ctx context.Context, imageName string) error {
	_, _, err := d.client.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil // Image exists
	}

	reader, err := d.client.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Consume the reader to complete the pull
	io.Copy(io.Discard, reader)
	return nil
}

// Helper functions

func mapToEnvSlice(env map[string]string) []string {
	if env == nil {
		return nil
	}
	var result []string
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

func mapDockerStatus(status string) interfaces.ContainerStatus {
	switch status {
	case "created":
		return interfaces.StatusCreated
	case "running":
		return interfaces.StatusRunning
	case "paused":
		return interfaces.StatusPaused
	case "restarting":
		return interfaces.StatusRestarting
	case "exited":
		return interfaces.StatusExited
	case "removing":
		return interfaces.StatusRemoving
	case "dead":
		return interfaces.StatusDead
	default:
		return interfaces.ContainerStatus(status)
	}
}
