package handlers_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyanstack/cyanstack/internal/api/events"
	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/testutil"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventsHandler_ReceivesEvents(t *testing.T) {
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	handler := handlers.NewEventsHandler(eventBus)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/events", handler.StreamEvents)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/events"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Give the handler time to subscribe
	time.Sleep(50 * time.Millisecond)

	// Publish an event
	testEvent := events.NewEvent(events.EventDeployStarted, "proj-1", nil)
	eventBus.Publish("proj-1", testEvent)

	// Read the event
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	require.NoError(t, err)

	// Parse the event
	var receivedEvent events.Event
	err = json.Unmarshal(msg, &receivedEvent)
	require.NoError(t, err)

	assert.Equal(t, events.EventDeployStarted, receivedEvent.Type)
	assert.Equal(t, "proj-1", receivedEvent.ProjectID)
}

func TestEventsHandler_ProjectSpecific(t *testing.T) {
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	handler := handlers.NewEventsHandler(eventBus)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/events", handler.StreamEvents)

	server := httptest.NewServer(router)
	defer server.Close()

	// Connect to proj-1
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Publish event to proj-2 (different project)
	eventBus.Publish("proj-2", events.NewEvent(events.EventDeployStarted, "proj-2", nil))

	// Publish event to proj-1
	eventBus.Publish("proj-1", events.NewEvent(events.EventNodeReady, "proj-1", nil))

	// Should only receive proj-1 event
	ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, msg, err := ws.ReadMessage()
	require.NoError(t, err)

	var receivedEvent events.Event
	err = json.Unmarshal(msg, &receivedEvent)
	require.NoError(t, err)

	assert.Equal(t, "proj-1", receivedEvent.ProjectID)
	assert.Equal(t, events.EventNodeReady, receivedEvent.Type)
}

func TestEventsHandler_MultipleEvents(t *testing.T) {
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	handler := handlers.NewEventsHandler(eventBus)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/events", handler.StreamEvents)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Publish multiple events
	eventTypes := []events.EventType{
		events.EventDeployStarted,
		events.EventNodeStarting,
		events.EventNodeReady,
		events.EventDeployCompleted,
	}

	for _, et := range eventTypes {
		eventBus.Publish("proj-1", events.NewEvent(et, "proj-1", nil))
	}

	// Receive all events
	receivedTypes := make([]events.EventType, 0, len(eventTypes))
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	for i := 0; i < len(eventTypes); i++ {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}

		var event events.Event
		if err := json.Unmarshal(msg, &event); err == nil {
			receivedTypes = append(receivedTypes, event.Type)
		}
	}

	assert.Len(t, receivedTypes, len(eventTypes))
	for i, et := range eventTypes {
		assert.Equal(t, et, receivedTypes[i])
	}
}

func TestEventsHandler_EventWithData(t *testing.T) {
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	handler := handlers.NewEventsHandler(eventBus)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/events", handler.StreamEvents)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Publish event with data
	nodeData := events.NodeProgressData{
		NodeID:   "postgres-1",
		PluginID: "com.test.postgres",
		Phase:    "starting",
		Message:  "Pulling image...",
	}
	eventBus.Publish("proj-1", events.NewEvent(events.EventNodeStarting, "proj-1", nodeData))

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	require.NoError(t, err)

	var receivedEvent events.Event
	err = json.Unmarshal(msg, &receivedEvent)
	require.NoError(t, err)

	assert.Equal(t, events.EventNodeStarting, receivedEvent.Type)
	assert.NotNil(t, receivedEvent.Data)
}

func TestEventsHandler_ClientDisconnect(t *testing.T) {
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	handler := handlers.NewEventsHandler(eventBus)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/events", handler.StreamEvents)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Close the connection
	ws.Close()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Publishing events should not panic
	assert.NotPanics(t, func() {
		eventBus.Publish("proj-1", events.NewEvent(events.EventDeployStarted, "proj-1", nil))
	})
}

func TestEventsHandler_MultipleSubscribers(t *testing.T) {
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	handler := handlers.NewEventsHandler(eventBus)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/events", handler.StreamEvents)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/events"

	// Connect two clients to the same project
	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws1.Close()

	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws2.Close()

	time.Sleep(100 * time.Millisecond)

	// Publish an event
	eventBus.Publish("proj-1", events.NewEvent(events.EventDeployStarted, "proj-1", nil))

	// Both clients should receive the event
	ws1.SetReadDeadline(time.Now().Add(2 * time.Second))
	ws2.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, msg1, err1 := ws1.ReadMessage()
	_, msg2, err2 := ws2.ReadMessage()

	require.NoError(t, err1)
	require.NoError(t, err2)

	var event1, event2 events.Event
	require.NoError(t, json.Unmarshal(msg1, &event1))
	require.NoError(t, json.Unmarshal(msg2, &event2))

	assert.Equal(t, events.EventDeployStarted, event1.Type)
	assert.Equal(t, events.EventDeployStarted, event2.Type)
}

func TestEventsHandler_DeployResultData(t *testing.T) {
	eventBus := events.NewInMemoryEventBus()
	defer eventBus.Close()

	handler := handlers.NewEventsHandler(eventBus)
	router := testutil.SetupTestRouter()
	router.GET("/api/v1/projects/:id/events", handler.StreamEvents)

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/projects/proj-1/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Publish deploy completed event with result data
	resultData := events.DeployResultData{
		Success: true,
		ContainerIDs: map[string]string{
			"postgres": "container-1",
			"backend":  "container-2",
		},
	}
	eventBus.Publish("proj-1", events.NewEvent(events.EventDeployCompleted, "proj-1", resultData))

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	require.NoError(t, err)

	var receivedEvent events.Event
	err = json.Unmarshal(msg, &receivedEvent)
	require.NoError(t, err)

	assert.Equal(t, events.EventDeployCompleted, receivedEvent.Type)
	assert.NotNil(t, receivedEvent.Data)

	// Verify data is serialized
	var dataMap map[string]interface{}
	err = json.Unmarshal(receivedEvent.Data, &dataMap)
	require.NoError(t, err)
	assert.True(t, dataMap["success"].(bool))
}
