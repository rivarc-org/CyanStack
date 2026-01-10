package handlers_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/testutil"
	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRuntimeDriver implements interfaces.RuntimeDriver for testing
type mockRuntimeDriver struct {
	getContainerFunc func(ctx context.Context, name string) (*interfaces.ContainerState, error)
	streamLogsFunc   func(ctx context.Context, containerID string) (io.ReadCloser, error)
}

func (m *mockRuntimeDriver) CanSchedule(ctx context.Context, requirements interfaces.ResourceRequirements) bool {
	return true
}

func (m *mockRuntimeDriver) Run(ctx context.Context, conf interfaces.ContainerConfig) (string, error) {
	return "", nil
}

func (m *mockRuntimeDriver) Stop(ctx context.Context, containerID string) error {
	return nil
}

func (m *mockRuntimeDriver) Remove(ctx context.Context, containerID string) error {
	return nil
}

func (m *mockRuntimeDriver) StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	if m.streamLogsFunc != nil {
		return m.streamLogsFunc(ctx, containerID)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockRuntimeDriver) Inspect(ctx context.Context, containerID string) (*interfaces.ContainerState, error) {
	return nil, nil
}

func (m *mockRuntimeDriver) List(ctx context.Context) ([]interfaces.ContainerState, error) {
	return nil, nil
}

func (m *mockRuntimeDriver) Exec(ctx context.Context, containerID string, cmd []string, output io.Writer) (int, error) {
	return 0, nil
}

func (m *mockRuntimeDriver) WaitForHealthy(ctx context.Context, containerID string, timeout time.Duration) error {
	return nil
}

func (m *mockRuntimeDriver) GetContainer(ctx context.Context, name string) (*interfaces.ContainerState, error) {
	if m.getContainerFunc != nil {
		return m.getContainerFunc(ctx, name)
	}
	return nil, interfaces.ErrContainerNotFound
}

func (m *mockRuntimeDriver) WaitForService(ctx context.Context, containerID string, config interfaces.HealthCheckConfig) error {
	return nil
}

func TestLogsHandler_ContainerNotFound(t *testing.T) {
	driver := &mockRuntimeDriver{
		getContainerFunc: func(ctx context.Context, name string) (*interfaces.ContainerState, error) {
			return nil, interfaces.ErrContainerNotFound
		},
	}

	handler := handlers.NewLogsHandler(driver)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/containers/:node_id/logs", handler.StreamLogs)

	// Make regular HTTP request (not WebSocket)
	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-1/containers/node-1/logs", nil)

	// Should return 404 for container not found
	assert.Equal(t, 404, w.Code)
}

func TestLogsHandler_StreamSuccess(t *testing.T) {
	logContent := "Line 1\nLine 2\nLine 3\n"

	driver := &mockRuntimeDriver{
		getContainerFunc: func(ctx context.Context, name string) (*interfaces.ContainerState, error) {
			// Verify container name format: cyan-{projectID}-{nodeID}
			assert.Equal(t, "cyan-proj-1-node-1", name)
			return &interfaces.ContainerState{
				ID:     "container-123",
				Name:   "/cyan-proj-1-node-1",
				Status: interfaces.StatusRunning,
			}, nil
		},
		streamLogsFunc: func(ctx context.Context, containerID string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(logContent)), nil
		},
	}

	handler := handlers.NewLogsHandler(driver)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/containers/:node_id/logs", handler.StreamLogs)

	// Start test server
	server := httptest.NewServer(router)
	defer server.Close()

	// Build WebSocket URL
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/containers/node-1/logs"

	// Connect with WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Set read deadline
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Read message
	_, msg, err := ws.ReadMessage()
	require.NoError(t, err)

	// Verify log content
	assert.Contains(t, string(msg), "Line 1")
}

func TestLogsHandler_ContainerNameFormat(t *testing.T) {
	var capturedName string

	driver := &mockRuntimeDriver{
		getContainerFunc: func(ctx context.Context, name string) (*interfaces.ContainerState, error) {
			capturedName = name
			return &interfaces.ContainerState{
				ID:     "container-123",
				Status: interfaces.StatusRunning,
			}, nil
		},
		streamLogsFunc: func(ctx context.Context, containerID string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("logs")), nil
		},
	}

	handler := handlers.NewLogsHandler(driver)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/containers/:node_id/logs", handler.StreamLogs)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/my-project/containers/postgres-db/logs"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	ws.Close()

	// Verify container name format
	assert.Equal(t, "cyan-my-project-postgres-db", capturedName)
}

func TestLogsHandler_LogStreamError(t *testing.T) {
	driver := &mockRuntimeDriver{
		getContainerFunc: func(ctx context.Context, name string) (*interfaces.ContainerState, error) {
			return &interfaces.ContainerState{
				ID:     "container-123",
				Status: interfaces.StatusRunning,
			}, nil
		},
		streamLogsFunc: func(ctx context.Context, containerID string) (io.ReadCloser, error) {
			return nil, io.ErrUnexpectedEOF
		},
	}

	handler := handlers.NewLogsHandler(driver)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/containers/:node_id/logs", handler.StreamLogs)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/containers/node-1/logs"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Should receive error message
	_, msg, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(msg), "Error")
}

func TestLogsHandler_MultipleChunks(t *testing.T) {
	// Create a reader that returns data in chunks
	chunk1 := strings.Repeat("a", 100)
	chunk2 := strings.Repeat("b", 100)
	allContent := chunk1 + chunk2

	driver := &mockRuntimeDriver{
		getContainerFunc: func(ctx context.Context, name string) (*interfaces.ContainerState, error) {
			return &interfaces.ContainerState{ID: "container-123"}, nil
		},
		streamLogsFunc: func(ctx context.Context, containerID string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(allContent)), nil
		},
	}

	handler := handlers.NewLogsHandler(driver)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/containers/:node_id/logs", handler.StreamLogs)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/containers/node-1/logs"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Collect all messages
	var received strings.Builder
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		received.Write(msg)
		if received.Len() >= len(allContent) {
			break
		}
	}

	assert.Equal(t, allContent, received.String())
}
