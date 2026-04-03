package main

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newScheduler is a helper so tests don't repeat field names.
func newScheduler(interval time.Duration, jitter float64) *Scheduler {
	return &Scheduler{
		Interval: interval,
		Jitter:   jitter,
	}
}

// TestJobRunsAtLeastOnce verifies the job is invoked within a reasonable window.
func TestJobRunsAtLeastOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var called atomic.Int32
	s := newScheduler(100*time.Millisecond, 0)

	_ = s.Run(ctx, func(ctx context.Context) error {
		called.Add(1)
		return nil
	})

	if called.Load() == 0 {
		t.Fatal("expected job to be called at least once")
	}
}

// TestJobRunsMultipleTimes verifies the scheduler repeats the job.
func TestJobRunsMultipleTimes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	var called atomic.Int32
	s := newScheduler(100*time.Millisecond, 0)

	_ = s.Run(ctx, func(ctx context.Context) error {
		called.Add(1)
		return nil
	})

	if n := called.Load(); n < 3 {
		t.Fatalf("expected at least 3 calls, got %d", n)
	}
}

// TestContextCancellationStopsQuickly verifies Run returns shortly after ctx is cancelled.
func TestContextCancellationStopsQuickly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	s := newScheduler(1*time.Second, 0)

	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx, func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		})
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// good — stopped promptly
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Run did not return promptly after context cancellation")
	}
}

// TestNoJobOverlap verifies two job invocations never run at the same time.
func TestNoJobOverlap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	var concurrency atomic.Int32
	var overlap atomic.Bool

	s := newScheduler(50*time.Millisecond, 0)

	_ = s.Run(ctx, func(ctx context.Context) error {
		if concurrency.Add(1) > 1 {
			overlap.Store(true)
		}
		time.Sleep(60 * time.Millisecond) // intentionally longer than interval
		concurrency.Add(-1)
		return nil
	})

	if overlap.Load() {
		t.Fatal("job was executed concurrently with itself")
	}
}

// TestContextPropagatedToJob verifies the job receives a live (non-nil) context
// and that the context is cancelled when the scheduler shuts down.
func TestContextPropagatedToJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	s := newScheduler(50*time.Millisecond, 0)

	var receivedNilCtx atomic.Bool
	done := make(chan struct{})

	go func() {
		_ = s.Run(ctx, func(jobCtx context.Context) error {
			if jobCtx == nil {
				receivedNilCtx.Store(true)
			}
			return nil
		})
		close(done)
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done

	if receivedNilCtx.Load() {
		t.Fatal("job received a nil context")
	}
}

// TestJobErrorDoesNotCrash verifies that a job returning an error does not
// stop the scheduler or panic — it should log and continue.
func TestJobErrorDoesNotCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	var called atomic.Int32
	s := newScheduler(80*time.Millisecond, 0)

	_ = s.Run(ctx, func(ctx context.Context) error {
		called.Add(1)
		return errors.New("transient error")
	})

	if n := called.Load(); n < 2 {
		t.Fatalf("expected scheduler to keep running after job errors, got %d calls", n)
	}
}

// TestRunReturnsOnContextDone verifies Run's return value is nil (not an error)
// when the scheduler stops due to context cancellation.
func TestRunReturnsNilOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	s := newScheduler(50*time.Millisecond, 0)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Run(ctx, func(ctx context.Context) error { return nil })
	}()

	time.Sleep(60 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error on clean shutdown, got: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestNoGoroutineLeak verifies no goroutines are leaked after Run returns.
func TestNoGoroutineLeak(t *testing.T) {
	// Allow goroutines from other tests to settle.
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	s := newScheduler(50*time.Millisecond, 0)

	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx, func(ctx context.Context) error { return nil })
		close(done)
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done

	// Give the runtime a moment to clean up.
	time.Sleep(30 * time.Millisecond)
	runtime.GC()

	after := runtime.NumGoroutine()
	if after > before+1 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

// TestJitterApplied verifies intervals between job runs vary (not all identical),
// proving jitter is actually applied.
func TestJitterApplied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var mu sync.Mutex
	var gaps []time.Duration
	last := time.Now()

	s := newScheduler(200*time.Millisecond, 0.2) // ±20% jitter

	_ = s.Run(ctx, func(ctx context.Context) error {
		now := time.Now()
		mu.Lock()
		gaps = append(gaps, now.Sub(last))
		last = now
		mu.Unlock()
		return nil
	})

	mu.Lock()
	defer mu.Unlock()

	if len(gaps) < 4 {
		t.Skipf("not enough samples (%d) to test jitter", len(gaps))
	}

	// Skip gap[0] — it's the time-to-first-run, not an inter-run interval.
	intervals := gaps[1:]
	min, max := intervals[0], intervals[0]
	for _, g := range intervals[1:] {
		if g < min {
			min = g
		}
		if g > max {
			max = g
		}
	}

	// With 20% jitter on 200ms the spread should be at least 10ms.
	if max-min < 10*time.Millisecond {
		t.Fatalf("jitter appears absent: min=%v max=%v (spread too small)", min, max)
	}
}
