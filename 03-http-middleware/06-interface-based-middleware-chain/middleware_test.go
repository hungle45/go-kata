package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Mock processor for testing
type mockProcessor struct {
	processFunc func(ctx context.Context, event Event) ([]Event, error)
	callCount   int
}

func (m *mockProcessor) Process(ctx context.Context, event Event) ([]Event, error) {
	m.callCount++
	if m.processFunc != nil {
		return m.processFunc(ctx, event)
	}
	return []Event{event}, nil
}

func newMockProcessor(fn func(ctx context.Context, event Event) ([]Event, error)) *mockProcessor {
	return &mockProcessor{processFunc: fn}
}

// Test Validator Processor
func TestValidatorProcessor(t *testing.T) {
	t.Run("valid event passes through", func(t *testing.T) {
		mockNext := newMockProcessor(nil)
		validator := NewValidatorProcessorBuilder()(mockNext)

		event := NewEvent("user123", ActionUploadFile)
		result, err := validator.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 event, got %d", len(result))
		}
		if mockNext.callCount != 1 {
			t.Errorf("expected next processor called once, got %d", mockNext.callCount)
		}
	})

	t.Run("invalid event rejected", func(t *testing.T) {
		mockNext := newMockProcessor(nil)
		validator := NewValidatorProcessorBuilder()(mockNext)

		event := NewEvent("", ActionUploadFile) // Empty UserID
		result, err := validator.Process(context.Background(), event)

		if !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
		if mockNext.callCount != 0 {
			t.Errorf("expected next processor not called, got %d calls", mockNext.callCount)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		mockNext := newMockProcessor(nil)
		validator := NewValidatorProcessorBuilder()(mockNext)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		event := NewEvent("user123", ActionUploadFile)
		result, err := validator.Process(ctx, event)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
		if mockNext.callCount != 0 {
			t.Errorf("expected next processor not called due to cancellation, got %d calls", mockNext.callCount)
		}
	})
}

// Test Timeout Processor
func TestTimeoutProcessor(t *testing.T) {
	t.Run("completes before timeout", func(t *testing.T) {
		mockNext := newMockProcessor(func(ctx context.Context, event Event) ([]Event, error) {
			time.Sleep(10 * time.Millisecond)
			return []Event{event}, nil
		})

		timeout := NewTimeoutProcessorBuilder(100 * time.Millisecond)(mockNext)
		event := NewEvent("user123", ActionUploadFile)

		result, err := timeout.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 event, got %d", len(result))
		}
	})

	t.Run("times out on slow processing", func(t *testing.T) {
		mockNext := newMockProcessor(func(ctx context.Context, event Event) ([]Event, error) {
			select {
			case <-time.After(200 * time.Millisecond):
				return []Event{event}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

		timeout := NewTimeoutProcessorBuilder(50 * time.Millisecond)(mockNext)
		event := NewEvent("user123", ActionUploadFile)

		result, err := timeout.Process(context.Background(), event)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})

	t.Run("respects parent context cancellation", func(t *testing.T) {
		mockNext := newMockProcessor(nil)
		timeout := NewTimeoutProcessorBuilder(1 * time.Second)(mockNext)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		event := NewEvent("user123", ActionUploadFile)
		result, err := timeout.Process(ctx, event)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})
}

// Test Event Splitter Processor
func TestEventSplitterProcessor(t *testing.T) {
	t.Run("splits event based on rules", func(t *testing.T) {
		mockNext := newMockProcessor(nil)
		splitter := NewEventSplitterProcessorBuilder(
			WithSplitRule(ActionUploadFile, []Action{ActionUploadToStorage, ActionUploadMetadata}),
		)(mockNext)

		event := NewEvent("user123", ActionUploadFile)
		result, err := splitter.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 events, got %d", len(result))
		}
		if mockNext.callCount != 2 {
			t.Errorf("expected next processor called twice, got %d", mockNext.callCount)
		}
		if result[0].Action != ActionUploadToStorage {
			t.Errorf("expected first event to be UploadToStorage, got %v", result[0].Action)
		}
		if result[1].Action != ActionUploadMetadata {
			t.Errorf("expected second event to be UploadMetadata, got %v", result[1].Action)
		}
	})

	t.Run("passes through event without split rule", func(t *testing.T) {
		mockNext := newMockProcessor(nil)
		splitter := NewEventSplitterProcessorBuilder()(mockNext)

		event := NewEvent("user123", ActionUploadFile)
		result, err := splitter.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 event, got %d", len(result))
		}
		if mockNext.callCount != 1 {
			t.Errorf("expected next processor called once, got %d", mockNext.callCount)
		}
	})

	t.Run("handles partial errors in split events", func(t *testing.T) {
		callCount := 0
		mockNext := newMockProcessor(func(ctx context.Context, event Event) ([]Event, error) {
			callCount++
			if event.Action == ActionUploadToStorage {
				return nil, errors.New("storage error")
			}
			return []Event{event}, nil
		})

		splitter := NewEventSplitterProcessorBuilder(
			WithSplitRule(ActionUploadFile, []Action{ActionUploadToStorage, ActionUploadMetadata}),
		)(mockNext)

		event := NewEvent("user123", ActionUploadFile)
		result, err := splitter.Process(context.Background(), event)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errors.New("storage error")) && err.Error() != "storage error" {
			t.Errorf("expected storage error in joined errors, got %v", err)
		}
		// Should still process the successful one
		if len(result) != 1 {
			t.Errorf("expected 1 successful event, got %d", len(result))
		}
		if callCount != 2 {
			t.Errorf("expected both events to be processed, got %d calls", callCount)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		mockNext := newMockProcessor(nil)
		splitter := NewEventSplitterProcessorBuilder()(mockNext)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		event := NewEvent("user123", ActionUploadFile)
		result, err := splitter.Process(ctx, event)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})

	t.Run("infinite loop prevention", func(t *testing.T) {
		// This tests the "infinite loop" scenario from the kata
		processCount := 0
		mockNext := newMockProcessor(func(ctx context.Context, event Event) ([]Event, error) {
			processCount++
			// Even if we return multiple events, they don't get re-split
			return []Event{event}, nil
		})

		splitter := NewEventSplitterProcessorBuilder(
			WithSplitRule(ActionUploadFile, []Action{ActionUploadToStorage, ActionUploadMetadata}),
		)(mockNext)

		event := NewEvent("user123", ActionUploadFile)
		result, err := splitter.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Should split once and process 2 events, not infinitely
		if processCount != 2 {
			t.Errorf("expected 2 process calls (not infinite), got %d", processCount)
		}
		if len(result) != 2 {
			t.Errorf("expected 2 result events, got %d", len(result))
		}
	})
}

// Test Storage Processor
func TestStorageProcessor(t *testing.T) {
	t.Run("stores event successfully", func(t *testing.T) {
		storage := NewStorageProcessor()
		event := NewEvent("user123", ActionUploadFile)

		result, err := storage.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 event, got %d", len(result))
		}
		if result[0].UserID != "user123" {
			t.Errorf("expected event with user123, got %s", result[0].UserID)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		storage := NewStorageProcessor()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		event := NewEvent("user123", ActionUploadFile)
		result, err := storage.Process(ctx, event)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})
}

// Test Pipeline Composition
func TestPipelineComposition(t *testing.T) {
	t.Run("builds pipeline correctly", func(t *testing.T) {
		pipeline := NewPipeline().
			Then(NewValidatorProcessorBuilder()).
			Then(NewEventSplitterProcessorBuilder(
				WithSplitRule(ActionUploadFile, []Action{ActionUploadToStorage, ActionUploadMetadata}),
			)).
			Build(NewStorageProcessor)

		event := NewEvent("user123", ActionUploadFile)
		result, err := pipeline.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 events (split), got %d", len(result))
		}
	})

	t.Run("validates before splitting", func(t *testing.T) {
		pipeline := NewPipeline().
			Then(NewValidatorProcessorBuilder()).
			Then(NewEventSplitterProcessorBuilder(
				WithSplitRule(ActionUploadFile, []Action{ActionUploadToStorage, ActionUploadMetadata}),
			)).
			Build(NewStorageProcessor)

		event := NewEvent("", ActionUploadFile) // Invalid
		result, err := pipeline.Process(context.Background(), event)

		if !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})

	t.Run("context cancellation propagates through pipeline", func(t *testing.T) {
		pipeline := NewPipeline().
			Then(NewValidatorProcessorBuilder()).
			Then(NewEventSplitterProcessorBuilder()).
			Build(NewStorageProcessor)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		event := NewEvent("user123", ActionUploadFile)
		result, err := pipeline.Process(ctx, event)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})
}

// Test Metrics Processor
func TestMetricsProcessor(t *testing.T) {
	t.Run("wraps processor with metrics", func(t *testing.T) {
		mockNext := newMockProcessor(func(ctx context.Context, event Event) ([]Event, error) {
			time.Sleep(10 * time.Millisecond)
			return []Event{event}, nil
		})

		metrics := NewMetricsProcessor(1, mockNext)
		event := NewEvent("user123", ActionUploadFile)

		result, err := metrics.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 event, got %d", len(result))
		}
		if mockNext.callCount != 1 {
			t.Errorf("expected next processor called once, got %d", mockNext.callCount)
		}
	})

	t.Run("metrics processor respects context", func(t *testing.T) {
		mockNext := newMockProcessor(nil)
		metrics := NewMetricsProcessor(1, mockNext)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		event := NewEvent("user123", ActionUploadFile)
		result, err := metrics.Process(ctx, event)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})
}

// Test Pipeline with Metrics
func TestPipelineWithMetrics(t *testing.T) {
	t.Run("pipeline with metrics enabled", func(t *testing.T) {
		pipeline := NewPipeline().
			WithMetrics().
			Then(NewValidatorProcessorBuilder()).
			Build(NewStorageProcessor)

		event := NewEvent("user123", ActionUploadFile)
		result, err := pipeline.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 event, got %d", len(result))
		}
	})
}

// Test Interface Pollution (Test Yourself #3)
func TestInterfacePollution(t *testing.T) {
	t.Run("add database middleware without modifying Processor interface", func(t *testing.T) {
		// Simulate adding a database-dependent middleware
		// The key is that we capture the DB connection in the closure,
		// not by modifying the Processor interface

		type DB struct {
			Name string
		}

		mockDB := &DB{Name: "testdb"}

		// Database middleware factory that captures the DB connection
		NewDatabaseProcessorBuilder := func(db *DB) ProcessBuilder {
			return func(next Processor) Processor {
				return ProcessorFunc(func(ctx context.Context, event Event) ([]Event, error) {
					// Use db here without changing Processor interface
					if db.Name == "" {
						return nil, errors.New("db not configured")
					}
					return next.Process(ctx, event)
				})
			}
		}

		// Build pipeline with database middleware
		pipeline := NewPipeline().
			Then(NewDatabaseProcessorBuilder(mockDB)).
			Then(NewValidatorProcessorBuilder()).
			Build(NewStorageProcessor)

		event := NewEvent("user123", ActionUploadFile)
		result, err := pipeline.Process(context.Background(), event)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 event, got %d", len(result))
		}

		t.Log("✅ Successfully added database middleware without modifying Processor interface")
	})
}

// Test Zero Global State
func TestZeroGlobalState(t *testing.T) {
	t.Run("split rules are instance-specific not global", func(t *testing.T) {
		mockNext := newMockProcessor(nil)

		// Create two splitters with different configs
		splitter1 := NewEventSplitterProcessorBuilder(
			WithSplitRule(ActionUploadFile, []Action{ActionUploadToStorage}),
		)(mockNext)

		mockNext2 := newMockProcessor(nil)
		splitter2 := NewEventSplitterProcessorBuilder(
			WithSplitRule(ActionUploadFile, []Action{ActionUploadMetadata}),
		)(mockNext2)

		event := NewEvent("user123", ActionUploadFile)

		// Process with first splitter
		result1, err := splitter1.Process(context.Background(), event)
		if err != nil {
			t.Fatalf("splitter1 error: %v", err)
		}

		// Process with second splitter
		result2, err := splitter2.Process(context.Background(), event)
		if err != nil {
			t.Fatalf("splitter2 error: %v", err)
		}

		// Verify they have different behaviors (proving no shared global state)
		if len(result1) != 1 {
			t.Errorf("splitter1 expected 1 event, got %d", len(result1))
		}
		if len(result2) != 1 {
			t.Errorf("splitter2 expected 1 event, got %d", len(result2))
		}
		if result1[0].Action != ActionUploadToStorage {
			t.Errorf("splitter1 expected UploadToStorage, got %v", result1[0].Action)
		}
		if result2[0].Action != ActionUploadMetadata {
			t.Errorf("splitter2 expected UploadMetadata, got %v", result2[0].Action)
		}

		t.Log("✅ No global state - each instance maintains its own configuration")
	})
}
