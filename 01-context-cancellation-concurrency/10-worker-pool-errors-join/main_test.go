package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_Parallelism(t *testing.T) {
	workerCount := 3
	pool := NewPool(workerCount)

	jobs := make(chan Job, 10)
	var activeWorkers int32
	var maxActiveWorkers int32

	jobFunc := func(ctx context.Context) error {
		current := atomic.AddInt32(&activeWorkers, 1)
		for {
			old := atomic.LoadInt32(&maxActiveWorkers)
			if current <= old || atomic.CompareAndSwapInt32(&maxActiveWorkers, old, current) {
				break
			}
		}
		
		time.Sleep(100 * time.Millisecond) // Simulate work
		atomic.AddInt32(&activeWorkers, -1)
		return nil
	}

	for i := 0; i < 6; i++ {
		jobs <- jobFunc
	}
	close(jobs)

	start := time.Now()
	err := pool.Run(context.Background(), jobs)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if duration > 500*time.Millisecond {
		t.Errorf("Pool ran too slowly! Duration was %v, expected < 300ms.", duration)
	}

	if atomic.LoadInt32(&maxActiveWorkers) <= 1 {
		t.Errorf("Pool did not use multiple workers, max active was %d", atomic.LoadInt32(&maxActiveWorkers))
	}
}

func TestPool_StopOnFirstError(t *testing.T) {
	pool := NewPool(3, WithStopOnFirstError())

	jobs := make(chan Job, 5)
	errMistake := errors.New("boom")
	
	jobs <- func(ctx context.Context) error { return nil }
	jobs <- func(ctx context.Context) error { return errMistake }
	jobs <- func(ctx context.Context) error { 
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(100 * time.Millisecond):
			return errors.New("should not be seen")
		}
	}
	close(jobs)

	err := pool.Run(context.Background(), jobs)
	if err == nil {
		t.Error("expected an error, got nil")
	}
	
	if !errors.Is(err, errMistake) {
		t.Errorf("expected error %v, got %v", errMistake, err)
	}
}

func TestPool_CollectAllErrors(t *testing.T) {
	pool := NewPool(2)
	err1 := errors.New("error 1")
	err2 := errors.New("error 2")

	jobs := make(chan Job, 3)
	jobs <- func(ctx context.Context) error { return err1 }
	jobs <- func(ctx context.Context) error { return nil }
	jobs <- func(ctx context.Context) error { return err2 }
	close(jobs)

	err := pool.Run(context.Background(), jobs)
	if err == nil {
		t.Fatal("expected aggregated errors, got nil")
	}

	if !errors.Is(err, err1) || !errors.Is(err, err2) {
		t.Errorf("expected joined errors containing both %v and %v, got: %v", err1, err2, err)
	}
}

func TestPool_PanicHandling(t *testing.T) {
	pool := NewPool(2)
	jobs := make(chan Job, 1)
	jobs <- func(ctx context.Context) error {
		panic("test panic")
	}
	close(jobs)

	err := pool.Run(context.Background(), jobs)
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}
}

func TestPool_ContextCancellation(t *testing.T) {
	pool := NewPool(2)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	jobs := make(chan Job, 10)
	go func() {
		for i := 0; i < 10; i++ {
			jobs <- func(ctx context.Context) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(200 * time.Millisecond):
					return nil
				}
			}
		}
		close(jobs)
	}()

	err := pool.Run(ctx, jobs)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}
