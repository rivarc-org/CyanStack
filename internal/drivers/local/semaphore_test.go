package local_test

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyanstack/cyanstack/internal/drivers/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemaphore_DefaultLimit(t *testing.T) {
	s := local.NewSemaphore(0)
	assert.Equal(t, runtime.NumCPU(), s.Limit())
}

func TestSemaphore_CustomLimit(t *testing.T) {
	s := local.NewSemaphore(5)
	assert.Equal(t, 5, s.Limit())
	assert.Equal(t, 5, s.Available())
	assert.Equal(t, 0, s.Active())
}

func TestSemaphore_AcquireRelease(t *testing.T) {
	s := local.NewSemaphore(2)
	ctx := context.Background()

	// Acquire first slot
	err := s.Acquire(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, s.Available())
	assert.Equal(t, 1, s.Active())

	// Acquire second slot
	err = s.Acquire(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, s.Available())
	assert.Equal(t, 2, s.Active())

	// Release one
	s.Release()
	assert.Equal(t, 1, s.Available())
	assert.Equal(t, 1, s.Active())

	// Release other
	s.Release()
	assert.Equal(t, 2, s.Available())
	assert.Equal(t, 0, s.Active())
}

func TestSemaphore_ConcurrencyLimit(t *testing.T) {
	limit := 4
	s := local.NewSemaphore(limit)

	var maxConcurrent int32
	var currentConcurrent int32
	var wg sync.WaitGroup

	// Launch 20 goroutines
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx := context.Background()
			err := s.Acquire(ctx)
			require.NoError(t, err)

			// Track concurrent executions
			current := atomic.AddInt32(&currentConcurrent, 1)
			for {
				max := atomic.LoadInt32(&maxConcurrent)
				if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
					break
				}
			}

			// Simulate work
			time.Sleep(10 * time.Millisecond)

			atomic.AddInt32(&currentConcurrent, -1)
			s.Release()
		}()
	}

	wg.Wait()

	// Verify max concurrent never exceeded limit
	assert.LessOrEqual(t, int(maxConcurrent), limit)
	assert.Equal(t, limit, s.Available())
	assert.Equal(t, 0, s.Active())
}

func TestSemaphore_ContextCancellation(t *testing.T) {
	s := local.NewSemaphore(1)

	// Acquire the only slot
	ctx := context.Background()
	err := s.Acquire(ctx)
	require.NoError(t, err)

	// Try to acquire with cancelled context
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err = s.Acquire(cancelCtx)
	assert.Equal(t, context.Canceled, err)

	// Active should still be 1 (failed acquire doesn't increment)
	assert.Equal(t, 1, s.Active())

	s.Release()
}

func TestSemaphore_ContextTimeout(t *testing.T) {
	s := local.NewSemaphore(1)

	// Acquire the only slot
	ctx := context.Background()
	err := s.Acquire(ctx)
	require.NoError(t, err)

	// Try to acquire with timeout
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = s.Acquire(timeoutCtx)
	elapsed := time.Since(start)

	assert.Equal(t, context.DeadlineExceeded, err)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)

	s.Release()
}

func TestSemaphore_TryAcquire(t *testing.T) {
	s := local.NewSemaphore(1)

	// First try should succeed
	assert.True(t, s.TryAcquire())
	assert.Equal(t, 1, s.Active())
	assert.Equal(t, 0, s.Available())

	// Second try should fail (no blocking)
	assert.False(t, s.TryAcquire())
	assert.Equal(t, 1, s.Active()) // Still 1, not 2

	s.Release()

	// After release, try should succeed again
	assert.True(t, s.TryAcquire())
	assert.Equal(t, 1, s.Active())

	s.Release()
}

func TestSemaphore_BlockingBehavior(t *testing.T) {
	s := local.NewSemaphore(1)
	ctx := context.Background()

	// Acquire the slot
	err := s.Acquire(ctx)
	require.NoError(t, err)

	// Start a goroutine that will block
	acquired := make(chan struct{})
	go func() {
		err := s.Acquire(ctx)
		require.NoError(t, err)
		close(acquired)
	}()

	// Give the goroutine time to start blocking
	time.Sleep(50 * time.Millisecond)

	// Should not have acquired yet
	select {
	case <-acquired:
		t.Fatal("goroutine should not have acquired the semaphore yet")
	default:
		// Expected
	}

	// Release the slot
	s.Release()

	// Now the goroutine should acquire
	select {
	case <-acquired:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("goroutine should have acquired the semaphore")
	}

	// Clean up
	s.Release()
}
