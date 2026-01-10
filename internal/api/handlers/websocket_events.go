package handlers

import (
	"encoding/json"

	"github.com/cyanstack/cyanstack/internal/api/events"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// EventsHandler handles event streaming via WebSocket
type EventsHandler struct {
	eventBus *events.InMemoryEventBus
}

// NewEventsHandler creates a new EventsHandler
func NewEventsHandler(eventBus *events.InMemoryEventBus) *EventsHandler {
	return &EventsHandler{
		eventBus: eventBus,
	}
}

// StreamEvents streams deployment events over WebSocket
func (h *EventsHandler) StreamEvents(c *gin.Context) {
	projectID := c.Param("id")

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade writes its own error response
		return
	}
	defer ws.Close()

	// Subscribe to events for this project
	eventCh, unsubscribe := h.eventBus.Subscribe(projectID)
	defer unsubscribe()

	// Handle client disconnection via a separate goroutine
	done := make(chan struct{})
	go func() {
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				close(done)
				return
			}
		}
	}()

	// Forward events to WebSocket
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed (event bus shutdown)
				return
			}

			// Marshal event to JSON
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}

			// Send to client
			if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
				// Client disconnected
				return
			}

		case <-done:
			// Client disconnected
			return
		}
	}
}
