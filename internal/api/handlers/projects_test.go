package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/testutil"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectHandler_List_Empty(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var projects []dto.ProjectResponse
	testutil.DecodeResponse(t, w, &projects)
	assert.Len(t, projects, 0)
}

func TestProjectHandler_List_WithProjects(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create some test projects
	testutil.CreateTestProject(t, db, "proj-1", "Project One")
	testutil.CreateTestProject(t, db, "proj-2", "Project Two")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects", handler.List)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var projects []dto.ProjectResponse
	testutil.DecodeResponse(t, w, &projects)
	assert.Len(t, projects, 2)

	// Verify project data
	names := []string{projects[0].Name, projects[1].Name}
	assert.Contains(t, names, "Project One")
	assert.Contains(t, names, "Project Two")
}

func TestProjectHandler_Create_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects", handler.Create)

	reqBody := dto.CreateProjectRequest{
		Name: "My New Project",
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects", reqBody)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp dto.CreateProjectResponse
	testutil.DecodeResponse(t, w, &resp)

	// Verify project
	assert.Equal(t, "My New Project", resp.Project.Name)
	assert.NotEmpty(t, resp.Project.ID)
	assert.NotNil(t, resp.Project.CurrentRevisionID)

	// Verify initial revision was created
	assert.NotEmpty(t, resp.Revision.ID)
	assert.Equal(t, 1, resp.Revision.VersionNumber)
}

func TestProjectHandler_Create_MissingName(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects", handler.Create)

	// Send empty body
	w := testutil.MakeRequest(router, "POST", "/api/v1/projects", map[string]string{})

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeValidation, resp.Code)
}

func TestProjectHandler_Create_DuplicateName(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create existing project
	testutil.CreateTestProject(t, db, "existing-id", "Existing Project")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects", handler.Create)

	reqBody := dto.CreateProjectRequest{
		Name: "Existing Project",
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects", reqBody)

	assert.Equal(t, http.StatusConflict, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeConflict, resp.Code)
}

func TestProjectHandler_Create_WithGraphState(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.POST("/api/v1/projects", handler.Create)

	reqBody := dto.CreateProjectRequest{
		Name: "Project With Graph",
		GraphState: &dto.GraphStateDTO{
			Nodes: []dto.NodeStateDTO{
				{
					ID:       "node-1",
					PluginID: "com.test.postgres",
					Image:    "postgres:15",
					Inputs:   map[string]string{"DB_PASSWORD": "secret"},
				},
			},
		},
	}

	w := testutil.MakeRequest(router, "POST", "/api/v1/projects", reqBody)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp dto.CreateProjectResponse
	testutil.DecodeResponse(t, w, &resp)

	// Verify project was created
	assert.Equal(t, "Project With Graph", resp.Project.Name)

	// Verify the graph state was saved by fetching the revision
	revision, err := revisionRepo.GetByID(nil, resp.Revision.ID)
	require.NoError(t, err)

	var graphState dto.GraphStateDTO
	err = json.Unmarshal([]byte(revision.GraphState), &graphState)
	require.NoError(t, err)
	assert.Len(t, graphState.Nodes, 1)
	assert.Equal(t, "node-1", graphState.Nodes[0].ID)
}

func TestProjectHandler_Get_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create test project
	testutil.CreateTestProject(t, db, "proj-123", "Test Project")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id", handler.Get)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/proj-123", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp dto.ProjectResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, "proj-123", resp.ID)
	assert.Equal(t, "Test Project", resp.Name)
}

func TestProjectHandler_Get_NotFound(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id", handler.Get)

	w := testutil.MakeRequest(router, "GET", "/api/v1/projects/nonexistent", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeNotFound, resp.Code)
}

func TestProjectHandler_Delete_Success(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create test project (without coordinator, so no containers to stop)
	testutil.CreateTestProject(t, db, "proj-to-delete", "Project To Delete")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.DELETE("/api/v1/projects/:id", handler.Delete)

	w := testutil.MakeRequest(router, "DELETE", "/api/v1/projects/proj-to-delete", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, "deleted", resp["status"])

	// Verify project no longer exists
	project, err := projectRepo.GetByID(nil, "proj-to-delete")
	require.NoError(t, err)
	assert.Nil(t, project)
}

func TestProjectHandler_Delete_NotFound(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.DELETE("/api/v1/projects/:id", handler.Delete)

	w := testutil.MakeRequest(router, "DELETE", "/api/v1/projects/nonexistent", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp dto.ErrorResponse
	testutil.DecodeResponse(t, w, &resp)
	assert.Equal(t, dto.ErrCodeNotFound, resp.Code)
}

func TestProjectHandler_Delete_WithRevisions(t *testing.T) {
	db, cleanup := testutil.CreateTestDB(t)
	defer cleanup()

	// Create project with revision (tests CASCADE delete)
	testutil.CreateTestProjectWithRevision(t, db, "proj-with-rev", "Project With Rev", "rev-1")

	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	handler := handlers.NewProjectHandler(projectRepo, revisionRepo, nil)
	router := testutil.SetupTestRouter()
	router.DELETE("/api/v1/projects/:id", handler.Delete)

	w := testutil.MakeRequest(router, "DELETE", "/api/v1/projects/proj-with-rev", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify project and revision are both deleted
	project, err := projectRepo.GetByID(nil, "proj-with-rev")
	require.NoError(t, err)
	assert.Nil(t, project)

	revision, err := revisionRepo.GetByID(nil, "rev-1")
	require.NoError(t, err)
	assert.Nil(t, revision)
}
