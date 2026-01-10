package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RevisionHandler handles revision-related API requests
type RevisionHandler struct {
	revisionRepo *sqlite.RevisionRepository
	projectRepo  *sqlite.ProjectRepository
}

// NewRevisionHandler creates a new RevisionHandler
func NewRevisionHandler(
	revisionRepo *sqlite.RevisionRepository,
	projectRepo *sqlite.ProjectRepository,
) *RevisionHandler {
	return &RevisionHandler{
		revisionRepo: revisionRepo,
		projectRepo:  projectRepo,
	}
}

// List returns all revisions for a project (lightweight, no graph_state)
func (h *RevisionHandler) List(c *gin.Context) {
	projectID := c.Param("id")

	// Verify project exists
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

	revisions, err := h.revisionRepo.ListByProject(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to list revisions",
			dto.ErrCodeInternal,
		))
		return
	}

	// Return lightweight summaries WITHOUT graph_state
	response := make([]dto.RevisionSummary, len(revisions))
	for i, r := range revisions {
		response[i] = dto.RevisionSummary{
			ID:            r.ID,
			VersionNumber: r.VersionNumber,
			CreatedAt:     r.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, response)
}

// GetLatest returns the latest revision for a project (with full graph_state)
func (h *RevisionHandler) GetLatest(c *gin.Context) {
	projectID := c.Param("id")

	// Verify project exists
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

	revision, err := h.revisionRepo.GetLatest(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to get latest revision",
			dto.ErrCodeInternal,
		))
		return
	}

	if revision == nil {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(
			"No revisions found for this project",
			dto.ErrCodeNotFound,
		))
		return
	}

	// Parse graph state from JSON
	var graphState dto.GraphStateDTO
	if err := json.Unmarshal([]byte(revision.GraphState), &graphState); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to parse graph state",
			dto.ErrCodeInternal,
		))
		return
	}

	response := dto.RevisionDetailResponse{
		ID:            revision.ID,
		ProjectID:     revision.ProjectID,
		VersionNumber: revision.VersionNumber,
		GraphState:    graphState,
		CreatedAt:     revision.CreatedAt,
	}

	c.JSON(http.StatusOK, response)
}

// Get returns a specific revision by ID (with full graph_state)
func (h *RevisionHandler) Get(c *gin.Context) {
	projectID := c.Param("id")
	revisionID := c.Param("rev_id")

	// Verify project exists
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

	revision, err := h.revisionRepo.GetByID(c.Request.Context(), revisionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to get revision",
			dto.ErrCodeInternal,
		))
		return
	}

	if revision == nil || revision.ProjectID != projectID {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(
			"Revision not found",
			dto.ErrCodeNotFound,
		))
		return
	}

	// Parse graph state from JSON
	var graphState dto.GraphStateDTO
	if err := json.Unmarshal([]byte(revision.GraphState), &graphState); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to parse graph state",
			dto.ErrCodeInternal,
		))
		return
	}

	response := dto.RevisionDetailResponse{
		ID:            revision.ID,
		ProjectID:     revision.ProjectID,
		VersionNumber: revision.VersionNumber,
		GraphState:    graphState,
		CreatedAt:     revision.CreatedAt,
	}

	c.JSON(http.StatusOK, response)
}

// Create creates a new revision for a project
func (h *RevisionHandler) Create(c *gin.Context) {
	projectID := c.Param("id")

	// Verify project exists
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

	var req dto.CreateRevisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(
			"Invalid request body",
			dto.ErrCodeValidation,
		))
		return
	}

	// Get next version number
	nextVersion, err := h.revisionRepo.GetNextVersionNumber(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to determine next version number",
			dto.ErrCodeInternal,
		))
		return
	}

	// Serialize graph state
	graphJSON, err := json.Marshal(req.GraphState)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to serialize graph state",
			dto.ErrCodeInternal,
		))
		return
	}

	// Create new revision
	revisionID := uuid.New().String()
	revision := &sqlite.Revision{
		ID:            revisionID,
		ProjectID:     projectID,
		VersionNumber: nextVersion,
		GraphState:    string(graphJSON),
	}

	if err := h.revisionRepo.Create(c.Request.Context(), revision); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to create revision",
			dto.ErrCodeInternal,
		))
		return
	}

	// Update project's current revision
	if err := h.projectRepo.SetCurrentRevision(c.Request.Context(), projectID, revisionID); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to update project's current revision",
			dto.ErrCodeInternal,
		))
		return
	}

	// Fetch the created revision to get the CreatedAt timestamp
	createdRevision, _ := h.revisionRepo.GetByID(c.Request.Context(), revisionID)

	response := dto.RevisionDetailResponse{
		ID:            createdRevision.ID,
		ProjectID:     createdRevision.ProjectID,
		VersionNumber: createdRevision.VersionNumber,
		GraphState:    req.GraphState,
		CreatedAt:     createdRevision.CreatedAt,
	}

	c.JSON(http.StatusCreated, response)
}
