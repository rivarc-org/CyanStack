package handlers

import (
	"context"
	"net/http"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/api/events"
	"github.com/cyanstack/cyanstack/internal/orchestrator"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/gin-gonic/gin"
)

// DeployHandler handles deployment-related API requests
type DeployHandler struct {
	projectRepo *sqlite.ProjectRepository
	coordinator *orchestrator.Coordinator
	eventBus    *events.InMemoryEventBus
}

// NewDeployHandler creates a new DeployHandler
func NewDeployHandler(
	projectRepo *sqlite.ProjectRepository,
	coordinator *orchestrator.Coordinator,
	eventBus *events.InMemoryEventBus,
) *DeployHandler {
	return &DeployHandler{
		projectRepo: projectRepo,
		coordinator: coordinator,
		eventBus:    eventBus,
	}
}

// Deploy starts an async deployment of the current revision
func (h *DeployHandler) Deploy(c *gin.Context) {
	projectID := c.Param("id")

	// Get project to find current revision
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

	if project.CurrentRevisionID == nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(
			"Project has no current revision",
			dto.ErrCodeValidation,
		))
		return
	}

	var req dto.DeployRequest
	_ = c.ShouldBindJSON(&req) // Allow empty body

	// Use provided revision or default to current
	revisionID := *project.CurrentRevisionID
	if req.RevisionID != "" {
		revisionID = req.RevisionID
	}

	// Start async deployment
	go func() {
		// Publish deploy started event
		h.eventBus.Publish(projectID, events.NewEvent(
			events.EventDeployStarted,
			projectID,
			nil,
		))

		// Run deployment (using background context since request context will be done)
		result, err := h.coordinator.Deploy(context.Background(), revisionID)

		if err != nil {
			h.eventBus.Publish(projectID, events.NewEvent(
				events.EventDeployFailed,
				projectID,
				events.DeployResultData{
					Success: false,
					Error:   err.Error(),
				},
			))
			return
		}

		h.eventBus.Publish(projectID, events.NewEvent(
			events.EventDeployCompleted,
			projectID,
			events.DeployResultData{
				Success:      result.Success,
				ContainerIDs: result.ContainerIDs,
			},
		))
	}()

	c.JSON(http.StatusAccepted, dto.DeployResponse{
		Status:  "started",
		Message: "Deployment started. Connect to /projects/" + projectID + "/events for progress updates.",
	})
}

// Stop stops all containers for a project
func (h *DeployHandler) Stop(c *gin.Context) {
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

	// Stop all containers
	if err := h.coordinator.StopProject(c.Request.Context(), projectID); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
			"Failed to stop project containers",
			dto.ErrCodeInternal,
		))
		return
	}

	c.JSON(http.StatusOK, dto.StopResponse{
		Status:  "stopped",
		Message: "All containers for this project have been stopped.",
	})
}
