package handlers

import (
	"fmt"
	"io"
	"net/http"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from localhost
		origin := r.Header.Get("Origin")
		allowedOrigins := map[string]bool{
			"http://localhost:3000":  true,
			"http://127.0.0.1:3000":  true,
			"https://localhost:3000": true,
			"":                       true, // Allow no origin (e.g., wscat)
		}
		return allowedOrigins[origin]
	},
}

// LogsHandler handles container log streaming via WebSocket
type LogsHandler struct {
	driver interfaces.RuntimeDriver
}

// NewLogsHandler creates a new LogsHandler
func NewLogsHandler(driver interfaces.RuntimeDriver) *LogsHandler {
	return &LogsHandler{
		driver: driver,
	}
}

// StreamLogs streams container logs over WebSocket
func (h *LogsHandler) StreamLogs(c *gin.Context) {
	projectID := c.Param("id")
	nodeID := c.Param("node_id")

	// Build container name following the pattern: cyan-{projectID}-{nodeID}
	containerName := fmt.Sprintf("cyan-%s-%s", projectID, nodeID)

	// Verify container exists
	container, err := h.driver.GetContainer(c.Request.Context(), containerName)
	if err != nil {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(
			"Container not found",
			dto.ErrCodeNotFound,
		))
		return
	}

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade writes its own error response
		return
	}
	defer ws.Close()

	// Get log stream
	reader, err := h.driver.StreamLogs(c.Request.Context(), container.ID)
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}
	defer reader.Close()

	// Stream logs to WebSocket
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				ws.WriteMessage(websocket.TextMessage, []byte("Error reading logs: "+err.Error()))
			}
			break
		}

		if err := ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
			// Client disconnected
			break
		}
	}
}
