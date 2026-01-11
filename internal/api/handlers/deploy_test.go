package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/api/events"
	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/testutil"
	"github.com/cyanstack/cyanstack/internal/orchestrator"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/stretchr/testify/assert"
)

// mockCoordinator implements handlers.DeployCoordinator for testing
type mockCoordinator struct {
	mu              sync.Mutex
	deployFunc      func(ctx context.Context, revisionID string) (*orchestrator.DeployResult, error)
	stopProjectFunc func(ctx context.Context, projectID string) error
	deployCalled    bool
	stopCalled      bool
	lastRevisionID  string
	lastProjectID   string
}

func (m *mockCoordinator) Deploy(ctx context.Context, revisionID string) (*orchestrator.DeployResult, error) {
	m.mu.Lock()
	m.deployCalled = true
	m.lastRevisionID = revisionID
	m.mu.Unlock()

	if m.deployFunc != nil {
		return m.deployFunc(ctx, revisionID)
	}
	return &orchestrator.DeployResult{
		Success:      true,
		ContainerIDs: map[string]string{},
	}, nil
}

func (m *mockCoordinator) StopProject(ctx context.Context, projectID string) error {
	m.mu.Lock()
	m.stopCalled = true
	m.lastProjectID = projectID
	m.mu.Unlock()

	if m.stopProjectFunc != nil {
		return m.stopProjectFunc(ctx, projectID)
	}
	return nil
}

func TestDeployHandler_Deploy_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project with revision
	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/deploy", handler.Deploy)

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/deploy", dto.DeployRequest{RevisionID: "rev-1"})

	// Should return 202 Accepted immediately
	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp dto.DeployResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, "started", resp.Status)
	assert.Contains(t, resp.Message, "proj-1")

	// Wait for goroutine to start deployment
	time.Sleep(100 * time.Millisecond)

	coordinator.mu.Lock()
	assert.True(t, coordinator.deployCalled)
	assert.Equal(t, "rev-1", coordinator.lastRevisionID)
	coordinator.mu.Unlock()
}

func TestDeployHandler_Deploy_ProjectNotFound(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/deploy", handler.Deploy)

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/nonexistent/deploy", dto.DeployRequest{RevisionID: "rev-1"})

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeNotFound, resp.Code)

	// Coordinator should not be called
	assert.False(t, coordinator.deployCalled)
}

func TestDeployHandler_Deploy_NoCurrentRevision(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project without revision
	testutil.CreateTestProject(t, db, "proj-no-rev", "No Revision Project")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/deploy", handler.Deploy)

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-no-rev/deploy", dto.DeployRequest{RevisionID: "rev-1"})

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeValidation, resp.Code)

	// Coordinator should not be called
	assert.False(t, coordinator.deployCalled)
}

func TestDeployHandler_Deploy_CustomRevision(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project with two revisions
	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")
	testutil.CreateTestRevision(t, db, "rev-2", "proj-1", 2, `{"nodes":[]}`)

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/deploy", handler.Deploy)

	reqBody := dto.DeployRequest{
		RevisionID: "rev-2",
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/deploy", reqBody)

	assert.Equal(t, http.StatusAccepted, w.Code)

	// Wait for goroutine
	time.Sleep(100 * time.Millisecond)

	coordinator.mu.Lock()
	assert.True(t, coordinator.deployCalled)
	assert.Equal(t, "rev-2", coordinator.lastRevisionID, "Should use specified revision ID")
	coordinator.mu.Unlock()
}

func TestDeployHandler_Deploy_PublishesEvents(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/deploy", handler.Deploy)

	// Subscribe to events
	eventCh, unsubscribe := eventBus.Subscribe("proj-1")
	defer unsubscribe()

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/deploy", dto.DeployRequest{RevisionID: "rev-1"})
	assert.Equal(t, http.StatusAccepted, w.Code)

	// Collect events
	var receivedEvents []events.Event
	timeout := time.After(2 * time.Second)

	for len(receivedEvents) < 2 {
		select {
		case event := <-eventCh:
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			break
		}
		if len(receivedEvents) >= 2 {
			break
		}
	}

	// Should have received start and complete events
	assert.GreaterOrEqual(t, len(receivedEvents), 1)
	assert.Equal(t, events.EventDeployStarted, receivedEvents[0].Type)
}

func TestDeployHandler_Stop_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/stop", handler.Stop)

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/stop", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.StopResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, "stopped", resp.Status)

	assert.True(t, coordinator.stopCalled)
	assert.Equal(t, "proj-1", coordinator.lastProjectID)
}

func TestDeployHandler_Stop_ProjectNotFound(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/stop", handler.Stop)

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/nonexistent/stop", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeNotFound, resp.Code)

	assert.False(t, coordinator.stopCalled)
}

func TestDeployHandler_Stop_CoordinatorError(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{
		stopProjectFunc: func(ctx context.Context, projectID string) error {
			return errors.New("failed to stop containers")
		},
	}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/stop", handler.Stop)

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/stop", nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeInternal, resp.Code)
}

func TestDeployHandler_Deploy_FailedDeployment(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	coordinator := &mockCoordinator{
		deployFunc: func(ctx context.Context, revisionID string) (*orchestrator.DeployResult, error) {
			return nil, errors.New("deployment failed")
		},
	}

	handler := handlers.NewDeployHandler(projectRepo, revisionRepo, coordinator, eventBus)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/deploy", handler.Deploy)

	// Subscribe to events
	eventCh, unsubscribe := eventBus.Subscribe("proj-1")
	defer unsubscribe()

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/deploy", dto.DeployRequest{RevisionID: "rev-1"})

	// Should still return 202 (async)
	assert.Equal(t, http.StatusAccepted, w.Code)

	// Wait for events
	var receivedEvents []events.Event
	timeout := time.After(2 * time.Second)

loop:
	for {
		select {
		case event := <-eventCh:
			receivedEvents = append(receivedEvents, event)
			if event.Type == events.EventDeployFailed {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	// Should have received start and failed events
	assert.GreaterOrEqual(t, len(receivedEvents), 2)

	// Find the failed event
	var foundFailed bool
	for _, e := range receivedEvents {
		if e.Type == events.EventDeployFailed {
			foundFailed = true
			break
		}
	}
	assert.True(t, foundFailed, "Should have received EventDeployFailed")
}
