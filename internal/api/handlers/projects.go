package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/orchestrator"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ProjectHandler handles project-related API requests
type ProjectHandler struct {
	projectRepo  *sqlite.ProjectRepository
	revisionRepo *sqlite.RevisionRepository
	coordinator  *orchestrator.Coordinator
}

// NewProjectHandler creates a new ProjectHandler
func NewProjectHandler(
	projectRepo *sqlite.ProjectRepository,
	revisionRepo *sqlite.RevisionRepository,
	coordinator *orchestrator.Coordinator,
) *ProjectHandler {
	return &ProjectHandler{
		projectRepo:  projectRepo,
		revisionRepo: revisionRepo,
		coordinator:  coordinator,
	}
}

// List returns all projects
func (h *ProjectHandler) List(c *gin.Context) {
	projects, err := h.projectRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to list projects",
			dto.ErrCodeInternal,
		))
		return
	}

	response := make([]dto.ProjectResponse, len(projects))
	for i, p := range projects {
		response[i] = dto.ProjectResponse{
			ID:                p.ID,
			Name:              p.Name,
			CurrentRevisionID: p.CurrentRevisionID,
			CreatedAt:         p.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, response)
}

// Create creates a new project with an initial revision
func (h *ProjectHandler) Create(c *gin.Context) {
	var req dto.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(
			"Invalid request body",
			dto.ErrCodeValidation,
		))
		return
	}

	// Check if project with same name already exists
	existing, _ := h.projectRepo.GetByName(c.Request.Context(), req.Name)
	if existing != nil {
		c.JSON(http.StatusConflict, dto.NewErrorResponse(
			"A project with this name already exists",
			dto.ErrCodeConflict,
		))
		return
	}

	// Generate IDs
	projectID := uuid.New().String()
	revisionID := uuid.New().String()

	// Prepare graph state (empty if not provided)
	graphState := dto.GraphStateDTO{Nodes: []dto.NodeStateDTO{}}
	if req.GraphState != nil {
		graphState = *req.GraphState
	}

	graphJSON, err := json.Marshal(graphState)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to serialize graph state",
			dto.ErrCodeInternal,
		))
		return
	}

	// Create initial revision
	revision := &sqlite.Revision{
		ID:            revisionID,
		ProjectID:     projectID,
		VersionNumber: 1,
		GraphState:    string(graphJSON),
	}

	if err := h.revisionRepo.Create(c.Request.Context(), revision); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to create initial revision",
			dto.ErrCodeInternal,
		))
		return
	}

	// Create project with revision reference
	project := &sqlite.Project{
		ID:                projectID,
		Name:              req.Name,
		CurrentRevisionID: &revisionID,
	}

	if err := h.projectRepo.Create(c.Request.Context(), project); err != nil {
		// Try to clean up the revision
		_ = h.revisionRepo.Delete(c.Request.Context(), revisionID)

		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to create project",
			dto.ErrCodeInternal,
		))
		return
	}

	// Fetch the created project to get the CreatedAt timestamp
	createdProject, _ := h.projectRepo.GetByID(c.Request.Context(), projectID)
	createdRevision, _ := h.revisionRepo.GetByID(c.Request.Context(), revisionID)

	response := dto.CreateProjectResponse{
		Project: dto.ProjectResponse{
			ID:                createdProject.ID,
			Name:              createdProject.Name,
			CurrentRevisionID: createdProject.CurrentRevisionID,
			CreatedAt:         createdProject.CreatedAt,
		},
		Revision: dto.RevisionSummary{
			ID:            createdRevision.ID,
			VersionNumber: createdRevision.VersionNumber,
			CreatedAt:     createdRevision.CreatedAt,
		},
	}

	c.JSON(http.StatusCreated, response)
}

// Get returns a specific project by ID
func (h *ProjectHandler) Get(c *gin.Context) {
	projectID := c.Param("id")

	project, err := h.projectRepo.GetByID(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to get project",
			dto.ErrCodeInternal,
		))
		return
	}

	if project == nil {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(
			"Project not found",
			dto.ErrCodeNotFound,
		))
		return
	}

	response := dto.ProjectResponse{
		ID:                project.ID,
		Name:              project.Name,
		CurrentRevisionID: project.CurrentRevisionID,
		CreatedAt:         project.CreatedAt,
	}

	c.JSON(http.StatusOK, response)
}

// Delete stops all containers and deletes a project
func (h *ProjectHandler) Delete(c *gin.Context) {
	projectID := c.Param("id")

	// Check if project exists
	project, err := h.projectRepo.GetByID(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to get project",
			dto.ErrCodeInternal,
		))
		return
	}

	if project == nil {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(
			"Project not found",
			dto.ErrCodeNotFound,
		))
		return
	}

	// Stop all running containers for this project
	if h.coordinator != nil {
		if err := h.coordinator.StopProject(c.Request.Context(), projectID); err != nil {
			// Log but continue - we still want to delete the project
			// even if stopping containers fails (they might not be running)
		}
	}

	// Delete the project (cascades to revisions via foreign key)
	if err := h.projectRepo.Delete(c.Request.Context(), projectID); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to delete project",
			dto.ErrCodeInternal,
		))
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
