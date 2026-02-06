package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCache_Get(t *testing.T) {
	type testCase struct {
		name          string
		ttl           time.Duration
		key           string
		setup         func(c *Cache[string, int], loaderCount *int32)
		calls         int
		delayBetween  time.Duration
		expectedValue int
		expectedLoads int32
		expectedErr   error
		ctxTimeout    time.Duration
	}

	tests := []testCase{
		{
			name:          "Cache Hit",
			ttl:           1 * time.Second,
			key:           "hit",
			calls:         2,
			expectedValue: 42,
			expectedLoads: 1,
		},
		{
			name:          "Cache Expiry",
			ttl:           10 * time.Millisecond,
			key:           "expiry",
			calls:         2,
			delayBetween:  20 * time.Millisecond,
			expectedValue: 42,
			expectedLoads: 2,
		},
		{
			name: "Loader Error",
			ttl:  1 * time.Second,
			key:  "error",
			setup: func(c *Cache[string, int], loaderCount *int32) {
				// No specific setup needed, error handled in loader
			},
			calls:         1,
			expectedLoads: 1,
			expectedErr:   errors.New("load failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var loaderCount int32
			c := NewCache[string, int](tt.ttl)

			loader := func(ctx context.Context) (int, error) {
				atomic.AddInt32(&loaderCount, 1)
				if tt.expectedErr != nil {
					return 0, tt.expectedErr
				}
				return 42, nil
			}

			var lastVal int
			var lastErr error

			for i := 0; i < tt.calls; i++ {
				ctx := context.Background()
				if tt.ctxTimeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
					defer cancel()
				}

				lastVal, lastErr = c.Get(ctx, tt.key, loader)
				if tt.delayBetween > 0 {
					time.Sleep(tt.delayBetween)
				}
			}

			if tt.expectedErr != nil {
				if lastErr == nil {
					t.Errorf("expected error containing %v, got nil", tt.expectedErr)
				}
			} else {
				if lastErr != nil {
					t.Errorf("unexpected error: %v", lastErr)
				}
				if lastVal != tt.expectedValue {
					t.Errorf("expected value %v, got %v", tt.expectedValue, lastVal)
				}
			}

			if atomic.LoadInt32(&loaderCount) != tt.expectedLoads {
				t.Errorf("expected %v loads, got %v", tt.expectedLoads, loaderCount)
			}
		})
	}
}

func TestCache_StructKey(t *testing.T) {
	type Key struct {
		ID   int
		Name string
	}
	c := NewCache[Key, string](1 * time.Second)

	key1 := Key{ID: 1, Name: "A"}
	key2 := Key{ID: 1, Name: "B"}

	// We use different contexts to ensure we aren't just getting lucky
	ctx1 := context.WithValue(context.Background(), "val", "1")
	ctx2 := context.WithValue(context.Background(), "val", "2")

	val1, _ := c.Get(ctx1, key1, func(ctx context.Context) (string, error) {
		return "val1", nil
	})
	val2, _ := c.Get(ctx2, key2, func(ctx context.Context) (string, error) {
		return "val2", nil
	})

	if val1 == val2 {
		t.Errorf("expected different values for different keys, got both %v", val1)
	}
}

func TestCache_Stampede(t *testing.T) {
	c := NewCache[string, int](1 * time.Second)
	var loaderCount int32
	var wg sync.WaitGroup
	numCalls := 100
	key := "stampede"

	loader := func(ctx context.Context) (int, error) {
		atomic.AddInt32(&loaderCount, 1)
		time.Sleep(50 * time.Millisecond) // Simulate slow load
		return 100, nil
	}

	results := make([]int, numCalls)
	errors := make([]error, numCalls)

	wg.Add(numCalls)
	for i := 0; i < numCalls; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errors[idx] = c.Get(context.Background(), key, loader)
		}(i)
	}
	wg.Wait()

	if atomic.LoadInt32(&loaderCount) != 1 {
		t.Errorf("expected 1 load, got %v", loaderCount)
	}

	for i := 0; i < numCalls; i++ {
		if errors[i] != nil {
			t.Errorf("call %d failed: %v", i, errors[i])
		}
		if results[i] != 100 {
			t.Errorf("call %d expected 100, got %v", i, results[i])
		}
	}
}

func TestCache_Cancellation(t *testing.T) {
	c := NewCache[string, int](1 * time.Second)
	key := "cancel"

	loaderStarted := make(chan struct{})
	loaderBlock := make(chan struct{})

	loader := func(ctx context.Context) (int, error) {
		close(loaderStarted)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-loaderBlock:
			return 200, nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start a waiter that will be cancelled
	errChan := make(chan error, 1)
	go func() {
		_, err := c.Get(ctx, key, loader)
		errChan <- err
	}()

	<-loaderStarted
	cancel() // Cancel the first waiter

	err := <-errChan
	if err == nil {
		t.Error("expected error for cancelled waiter, got nil")
	}

	// Now try to get the same key with a fresh context
	close(loaderBlock)

	val, err := c.Get(context.Background(), key, func(ctx context.Context) (int, error) {
		return 300, nil
	})

	if err != nil {
		t.Errorf("subsequent load failed: %v", err)
	}
	if val != 200 && val != 300 {
		t.Errorf("expected 200 or 300, got %v", val)
	}
}
