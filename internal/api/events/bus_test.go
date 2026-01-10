package events_test

import (
	"sync"
	"testing"
	"time"

	"github.com/cyanstack/cyanstack/internal/api/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBus_Subscribe(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch, unsubscribe := bus.Subscribe("proj-1")
	defer unsubscribe()

	assert.NotNil(t, ch)
}

func TestEventBus_Publish(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch, unsubscribe := bus.Subscribe("proj-1")
	defer unsubscribe()

	event := events.NewEvent(events.EventDeployStarted, "proj-1", nil)
	bus.Publish("proj-1", event)

	select {
	case received := <-ch:
		assert.Equal(t, events.EventDeployStarted, received.Type)
		assert.Equal(t, "proj-1", received.ProjectID)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch1, unsub1 := bus.Subscribe("proj-1")
	defer unsub1()

	ch2, unsub2 := bus.Subscribe("proj-1")
	defer unsub2()

	event := events.NewEvent(events.EventNodeReady, "proj-1", nil)
	bus.Publish("proj-1", event)

	// Both subscribers should receive the event
	select {
	case received := <-ch1:
		assert.Equal(t, events.EventNodeReady, received.Type)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for event on ch1")
	}

	select {
	case received := <-ch2:
		assert.Equal(t, events.EventNodeReady, received.Type)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for event on ch2")
	}
}

func TestEventBus_ProjectIsolation(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch1, unsub1 := bus.Subscribe("proj-1")
	defer unsub1()

	ch2, unsub2 := bus.Subscribe("proj-2")
	defer unsub2()

	// Publish to proj-1
	bus.Publish("proj-1", events.NewEvent(events.EventDeployStarted, "proj-1", nil))

	// ch1 should receive
	select {
	case received := <-ch1:
		assert.Equal(t, "proj-1", received.ProjectID)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for event on ch1")
	}

	// ch2 should not receive
	select {
	case <-ch2:
		t.Fatal("proj-2 subscriber should not receive proj-1 events")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch, unsubscribe := bus.Subscribe("proj-1")

	// Unsubscribe
	unsubscribe()

	// Channel should be closed
	_, ok := <-ch
	assert.False(t, ok, "Channel should be closed after unsubscribe")

	// Publishing should not panic
	assert.NotPanics(t, func() {
		bus.Publish("proj-1", events.NewEvent(events.EventDeployStarted, "proj-1", nil))
	})
}

func TestEventBus_Close(t *testing.T) {
	bus := events.NewInMemoryEventBus()

	ch1, _ := bus.Subscribe("proj-1")
	ch2, _ := bus.Subscribe("proj-2")

	bus.Close()

	// All channels should be closed
	_, ok1 := <-ch1
	_, ok2 := <-ch2

	assert.False(t, ok1, "ch1 should be closed")
	assert.False(t, ok2, "ch2 should be closed")
}

func TestEventBus_CloseIdempotent(t *testing.T) {
	bus := events.NewInMemoryEventBus()

	bus.Close()

	// Second close should not panic
	assert.NotPanics(t, func() {
		bus.Close()
	})
}

func TestEventBus_PublishAfterClose(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	bus.Close()

	// Should not panic
	assert.NotPanics(t, func() {
		bus.Publish("proj-1", events.NewEvent(events.EventDeployStarted, "proj-1", nil))
	})
}

func TestEventBus_SubscribeAfterClose(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	bus.Close()

	ch, unsub := bus.Subscribe("proj-1")
	defer unsub()

	// Channel should be immediately closed
	_, ok := <-ch
	assert.False(t, ok, "Channel should be closed when subscribing to closed bus")
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch, unsub := bus.Subscribe("proj-1")
	defer unsub()

	// Publish concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish("proj-1", events.NewEvent(events.EventDeployStarted, "proj-1", nil))
		}()
	}

	// Collect events
	received := 0
	done := make(chan struct{})
	go func() {
		for range ch {
			received++
			if received >= 100 {
				close(done)
				return
			}
		}
	}()

	wg.Wait()

	select {
	case <-done:
		// All events received
	case <-time.After(2 * time.Second):
		// Some events might be dropped due to buffer
		t.Logf("Received %d/100 events (some may be dropped due to buffer)", received)
	}

	// Should have received most events (buffer is 100)
	assert.GreaterOrEqual(t, received, 50, "Should receive at least 50% of events")
}

func TestEventBus_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	var wg sync.WaitGroup

	// Concurrent subscribe/unsubscribe
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			projectID := "proj-1"
			if id%2 == 0 {
				projectID = "proj-2"
			}

			ch, unsub := bus.Subscribe(projectID)
			time.Sleep(10 * time.Millisecond)
			unsub()

			// Channel should be closed
			_, ok := <-ch
			assert.False(t, ok)
		}(i)
	}

	wg.Wait()
}

func TestEventBus_BufferedChannel(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch, unsub := bus.Subscribe("proj-1")
	defer unsub()

	// Publish more events than buffer size (100)
	for i := 0; i < 150; i++ {
		bus.Publish("proj-1", events.NewEvent(events.EventDeployStarted, "proj-1", nil))
	}

	// Should be able to receive at least 100 events (buffer size)
	received := 0
	for i := 0; i < 100; i++ {
		select {
		case <-ch:
			received++
		default:
			break
		}
	}

	assert.GreaterOrEqual(t, received, 100, "Buffer should hold at least 100 events")
}

func TestEventBus_NonBlockingPublish(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch, unsub := bus.Subscribe("proj-1")
	defer unsub()

	// Fill the buffer
	for i := 0; i < 200; i++ {
		bus.Publish("proj-1", events.NewEvent(events.EventDeployStarted, "proj-1", nil))
	}

	// Publish should not block
	done := make(chan struct{})
	go func() {
		bus.Publish("proj-1", events.NewEvent(events.EventDeployCompleted, "proj-1", nil))
		close(done)
	}()

	select {
	case <-done:
		// Publish completed without blocking
	case <-time.After(1 * time.Second):
		t.Fatal("Publish should not block even when buffer is full")
	}

	// Drain some events
	for i := 0; i < 100; i++ {
		<-ch
	}
}

func TestEventBus_EventWithData(t *testing.T) {
	bus := events.NewInMemoryEventBus()
	defer bus.Close()

	ch, unsub := bus.Subscribe("proj-1")
	defer unsub()

	data := events.NodeProgressData{
		NodeID:   "postgres-1",
		PluginID: "com.test.postgres",
		Phase:    "starting",
		Message:  "Container starting",
	}

	event := events.NewEvent(events.EventNodeStarting, "proj-1", data)
	bus.Publish("proj-1", event)

	select {
	case received := <-ch:
		assert.Equal(t, events.EventNodeStarting, received.Type)
		assert.NotNil(t, received.Data)
		assert.False(t, received.Timestamp.IsZero())
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestNewEvent(t *testing.T) {
	event := events.NewEvent(events.EventDeployFailed, "proj-123", nil)

	assert.Equal(t, events.EventDeployFailed, event.Type)
	assert.Equal(t, "proj-123", event.ProjectID)
	assert.False(t, event.Timestamp.IsZero())
}

func TestEventTypes(t *testing.T) {
	// Verify all event types are distinct
	types := []events.EventType{
		events.EventDeployStarted,
		events.EventNodeStarting,
		events.EventNodeHealthCheck,
		events.EventNodeReady,
		events.EventNodeFailed,
		events.EventDeployCompleted,
		events.EventDeployFailed,
	}

	seen := make(map[events.EventType]bool)
	for _, et := range types {
		require.False(t, seen[et], "Duplicate event type: %s", et)
		seen[et] = true
	}
}
