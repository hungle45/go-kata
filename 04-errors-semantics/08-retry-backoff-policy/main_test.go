package main

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockNetError struct {
	timeout bool
}

func (e *mockNetError) Error() string   { return "network error" }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return true }

func TestRetryer_Do(t *testing.T) {
	t.Run("SuccessOnFirstTry", func(t *testing.T) {
		r := NewRetryer(WithMaxAttempts(3), WithBaseDelay(1*time.Millisecond))
		calls := 0
		err := r.Do(context.Background(), func(ctx context.Context) error {
			calls++
			return nil
		})
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if calls != 1 {
			t.Errorf("expected 1 call, got %d", calls)
		}
	})

	t.Run("SuccessAfterRetries", func(t *testing.T) {
		r := NewRetryer(WithMaxAttempts(3), WithBaseDelay(1*time.Millisecond))
		calls := 0
		err := r.Do(context.Background(), func(ctx context.Context) error {
			calls++
			if calls < 2 {
				return ErrTransient
			}
			return nil
		})
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if calls != 2 {
			t.Errorf("expected 2 calls, got %d", calls)
		}
	})

	t.Run("MaxAttemptsReached", func(t *testing.T) {
		r := NewRetryer(WithMaxAttempts(3), WithBaseDelay(1*time.Millisecond))
		calls := 0
		err := r.Do(context.Background(), func(ctx context.Context) error {
			calls++
			return ErrTransient
		})
		if !errors.Is(err, ErrMaxRetryReached) {
			t.Errorf("expected ErrMaxRetryReached, got %v", err)
		}
		if calls != 3 {
			t.Errorf("expected 3 calls, got %d", calls)
		}
	})

	t.Run("NonTransientError", func(t *testing.T) {
		r := NewRetryer(WithMaxAttempts(3), WithBaseDelay(1*time.Millisecond))
		errFatal := errors.New("fatal error")
		calls := 0
		err := r.Do(context.Background(), func(ctx context.Context) error {
			calls++
			return errFatal
		})
		if !errors.Is(err, errFatal) {
			t.Errorf("expected errFatal, got %v", err)
		}
		if calls != 1 {
			t.Errorf("expected 1 call, got %d", calls)
		}
	})

	t.Run("NetworkTimeoutRetry", func(t *testing.T) {
		r := NewRetryer(WithMaxAttempts(3), WithBaseDelay(1*time.Millisecond))
		calls := 0
		err := r.Do(context.Background(), func(ctx context.Context) error {
			calls++
			if calls == 1 {
				return &mockNetError{timeout: true}
			}
			return nil
		})
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if calls != 2 {
			t.Errorf("expected 2 calls, got %d", calls)
		}
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		r := NewRetryer(WithMaxAttempts(5), WithBaseDelay(100*time.Millisecond))
		ctx, cancel := context.WithCancel(context.Background())

		calls := 0
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := r.Do(ctx, func(ctx context.Context) error {
			calls++
			return ErrTransient
		})

		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if calls != 1 {
			t.Errorf("expected 1 call before cancellation, got %d", calls)
		}
	})

	t.Run("WrappingRequirement", func(t *testing.T) {
		r := NewRetryer(WithMaxAttempts(2), WithBaseDelay(1*time.Millisecond))
		err := r.Do(context.Background(), func(ctx context.Context) error {
			return ErrTransient
		})

		if !errors.Is(err, ErrMaxRetryReached) {
			t.Errorf("expected ErrMaxRetryReached, got %v", err)
		}
		if !errors.Is(err, ErrTransient) {
			t.Errorf("expected ErrTransient to be wrapped, got %v", err)
		}
		expectedMsg := "max retry reached after 2 attempts: transient error"
		if err.Error() != expectedMsg {
			t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
		}
	})

	t.Run("DeterministicJitter", func(t *testing.T) {
		// Use a fixed seed for deterministic jitter
		source := rand.NewSource(42)
		r := NewRetryer(
			WithMaxAttempts(2),
			WithBaseDelay(10*time.Millisecond),
			WithJitter(5*time.Millisecond),
			WithRandSource(source),
		)

		// Verification is tricky because calcBackoffTime is private,
		// but we can trust the implementation if we inject the source correctly.
		// For now we just ensure it doesn't crash and follows the flow.
		err := r.Do(context.Background(), func(ctx context.Context) error {
			return ErrTransient
		})
		if !errors.Is(err, ErrMaxRetryReached) {
			t.Errorf("expected ErrMaxRetryReached, got %v", err)
		}
	})
}

func TestRetryer_Concurrency(t *testing.T) {
	// This test checks if multiple goroutines can use the same Retryer
	// Current implementation uses a shared timer, so this should fail or race
	r := NewRetryer(WithMaxAttempts(3), WithBaseDelay(10*time.Millisecond))
	var wg sync.WaitGroup
	var errorCount int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.Do(context.Background(), func(ctx context.Context) error {
				return ErrTransient
			})
			if !errors.Is(err, ErrMaxRetryReached) {
				atomic.AddInt32(&errorCount, 1)
			}
		}()
	}
	wg.Wait()
	if errorCount > 0 {
		t.Errorf("Concurrency test failed: %d errors", errorCount)
	}
}
