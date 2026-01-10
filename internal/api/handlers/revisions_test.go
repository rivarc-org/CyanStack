package handlers_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/testutil"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevisionHandler_List_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project with revisions
	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")
	testutil.CreateTestRevision(t, db, "rev-2", "proj-1", 2, `{"nodes":[{"id":"node-1"}]}`)

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/revisions", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-1/revisions", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var revisions []dto.RevisionSummary
	testutil.DecodeResponse(t, w, &revisions)
	assert.Len(t, revisions, 2)

	// Should be ordered by version (descending)
	assert.Equal(t, 2, revisions[0].VersionNumber)
	assert.Equal(t, 1, revisions[1].VersionNumber)
}

func TestRevisionHandler_List_ProjectNotFound(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/revisions", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/nonexistent/revisions", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeNotFound, resp.Code)
}

func TestRevisionHandler_GetLatest_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project with multiple revisions
	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")
	testutil.CreateTestRevision(t, db, "rev-2", "proj-1", 2, `{"nodes":[{"id":"node-a","plugin_id":"test","image":"test:1","inputs":{}}]}`)

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/revisions/latest", handler.GetLatest)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-1/revisions/latest", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.RevisionDetailResponse
	testutil.DecodeResponse(t, w, &resp)

	// Should return the latest version (2)
	assert.Equal(t, "rev-2", resp.ID)
	assert.Equal(t, 2, resp.VersionNumber)
	assert.Equal(t, "proj-1", resp.ProjectID)

	// Should include graph state
	assert.Len(t, resp.GraphState.Nodes, 1)
	assert.Equal(t, "node-a", resp.GraphState.Nodes[0].ID)
}

func TestRevisionHandler_GetLatest_NoRevisions(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project without any revisions
	testutil.CreateTestProject(t, db, "proj-empty", "Empty Project")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/revisions/latest", handler.GetLatest)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-empty/revisions/latest", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeNotFound, resp.Code)
}

func TestRevisionHandler_Get_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-specific")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/revisions/:rev_id", handler.Get)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-1/revisions/rev-specific", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.RevisionDetailResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, "rev-specific", resp.ID)
	assert.Equal(t, "proj-1", resp.ProjectID)
}

func TestRevisionHandler_Get_WrongProject(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create revision for proj-1
	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Project One", "rev-for-proj1")
	// Create a different project
	testutil.CreateTestProject(t, db, "proj-2", "Project Two")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/revisions/:rev_id", handler.Get)

	// Try to access proj-1's revision through proj-2
	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-2/revisions/rev-for-proj1", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeNotFound, resp.Code)
}

func TestRevisionHandler_Get_NotFound(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	testutil.CreateTestProject(t, db, "proj-1", "Test Project")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/revisions/:rev_id", handler.Get)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-1/revisions/nonexistent", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRevisionHandler_Create_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project with initial revision
	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/revisions", handler.Create)

	reqBody := dto.CreateRevisionRequest{
		GraphState: dto.GraphStateDTO{
			Nodes: []dto.NodeStateDTO{
				{
					ID:       "new-node",
					PluginID: "com.test.service",
					Image:    "myapp:latest",
					Inputs:   map[string]string{"PORT": "8080"},
				},
			},
		},
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/revisions", reqBody)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp dto.RevisionDetailResponse
	testutil.DecodeResponse(t, w, &resp)

	// Should create version 2
	assert.Equal(t, 2, resp.VersionNumber)
	assert.Equal(t, "proj-1", resp.ProjectID)
	assert.Len(t, resp.GraphState.Nodes, 1)
	assert.Equal(t, "new-node", resp.GraphState.Nodes[0].ID)

	// Verify project's current revision was updated
	project, err := projectRepo.GetByID(context.Background(), "proj-1")
	require.NoError(t, err)
	assert.Equal(t, resp.ID, *project.CurrentRevisionID)
}

func TestRevisionHandler_Create_MissingBody(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/revisions", handler.Create)

	// Send nil body
	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/revisions", nil)

	// With nil body, Gin returns 400 for required fields
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeValidation, resp.Code)
}

func TestRevisionHandler_Create_EmptyGraphState(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/revisions", handler.Create)

	// Send empty graph state (valid - creates revision with empty nodes)
	reqBody := dto.CreateRevisionRequest{
		GraphState: dto.GraphStateDTO{Nodes: []dto.NodeStateDTO{}},
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/revisions", reqBody)

	// Empty graph state should be valid
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRevisionHandler_Create_ProjectNotFound(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/revisions", handler.Create)

	reqBody := dto.CreateRevisionRequest{
		GraphState: dto.GraphStateDTO{
			Nodes: []dto.NodeStateDTO{},
		},
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/nonexistent/revisions", reqBody)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRevisionHandler_Create_VersionIncrement(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project with 3 existing revisions
	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")
	testutil.CreateTestRevision(t, db, "rev-2", "proj-1", 2, `{"nodes":[]}`)
	testutil.CreateTestRevision(t, db, "rev-3", "proj-1", 3, `{"nodes":[]}`)

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects/:id/revisions", handler.Create)

	reqBody := dto.CreateRevisionRequest{
		GraphState: dto.GraphStateDTO{Nodes: []dto.NodeStateDTO{}},
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects/proj-1/revisions", reqBody)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp dto.RevisionDetailResponse
	testutil.DecodeResponse(t, w, &resp)

	// Should be version 4
	assert.Equal(t, 4, resp.VersionNumber)
}

func TestRevisionHandler_List_DoesNotIncludeGraphState(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project with revision that has graph state
	testutil.CreateTestProjectWithRevision(t, db, "proj-1", "Test Project", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewRevisionHandler(revisionRepo, projectRepo)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/revisions", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-1/revisions", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	// Parse raw JSON to verify graph_state is not included
	var rawResp []map[string]interface{}
	testutil.DecodeResponse(t, w, &rawResp)

	require.Len(t, rawResp, 1)
	_, hasGraphState := rawResp[0]["graph_state"]
	assert.False(t, hasGraphState, "List response should not include graph_state")
}
