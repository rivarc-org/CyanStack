package local

import (
	"context"
	"runtime"
	"sync"
)

// Semaphore controls concurrent container startup operations using a buffered channel.
// This prevents "startup storms" where many containers try to pull images and start
// simultaneously, which would spike CPU usage.
type Semaphore struct {
	slots  chan struct{}
	limit  int
	mu     sync.Mutex
	active int
}

// NewSemaphore creates a semaphore with the specified limit.
// If limit <= 0, defaults to runtime.NumCPU().
func NewSemaphore(limit int) *Semaphore {
	if limit <= 0 {
		limit = runtime.NumCPU()
	}

	s := &Semaphore{
		slots: make(chan struct{}, limit),
		limit: limit,
	}

	// Pre-fill the channel with available slots
	for i := 0; i < limit; i++ {
		s.slots <- struct{}{}
	}

	return s
}

// Acquire blocks until a slot is available or context is cancelled.
// Returns nil on success, or the context error if cancelled.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case <-s.slots:
		s.mu.Lock()
		s.active++
		s.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquire attempts to acquire a slot without blocking.
// Returns true if acquired, false otherwise.
func (s *Semaphore) TryAcquire() bool {
	select {
	case <-s.slots:
		s.mu.Lock()
		s.active++
		s.mu.Unlock()
		return true
	default:
		return false
	}
}

// Release returns a slot to the semaphore.
// Must be called exactly once for each successful Acquire or TryAcquire.
func (s *Semaphore) Release() {
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	s.slots <- struct{}{}
}

// Available returns the number of available slots.
func (s *Semaphore) Available() int {
	return len(s.slots)
}

// Active returns the number of currently acquired slots.
func (s *Semaphore) Active() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// Limit returns the maximum number of concurrent slots.
func (s *Semaphore) Limit() int {
	return s.limit
}
