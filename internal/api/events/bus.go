package events

import (
	"sync"
)

// EventBus defines the interface for event publishing and subscribing
type EventBus interface {
	// Subscribe returns a channel for receiving events for a project
	// The returned function should be called to unsubscribe
	Subscribe(projectID string) (<-chan Event, func())

	// Publish sends an event to all subscribers of a project
	Publish(projectID string, event Event)

	// Close shuts down the event bus
	Close()
}

// InMemoryEventBus implements EventBus using in-memory channels
type InMemoryEventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
	closed      bool
}

// NewInMemoryEventBus creates a new in-memory event bus
func NewInMemoryEventBus() *InMemoryEventBus {
	return &InMemoryEventBus{
		subscribers: make(map[string][]chan Event),
	}
}

// Subscribe returns a channel for receiving events for a project
func (b *InMemoryEventBus) Subscribe(projectID string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan Event, 100) // Buffered to prevent blocking
	b.subscribers[projectID] = append(b.subscribers[projectID], ch)

	// Return unsubscribe function
	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		subs := b.subscribers[projectID]
		for i, sub := range subs {
			if sub == ch {
				// Remove this subscriber
				b.subscribers[projectID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}

		// Clean up empty subscription lists
		if len(b.subscribers[projectID]) == 0 {
			delete(b.subscribers, projectID)
		}
	}

	return ch, unsubscribe
}

// Publish sends an event to all subscribers of a project
func (b *InMemoryEventBus) Publish(projectID string, event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	subs := b.subscribers[projectID]
	for _, ch := range subs {
		// Non-blocking send - drop event if channel is full
		select {
		case ch <- event:
		default:
			// Channel full, skip this subscriber
		}
	}
}

// Close shuts down the event bus and closes all subscriber channels
func (b *InMemoryEventBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true

	for _, subs := range b.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}

	b.subscribers = make(map[string][]chan Event)
}
