package gracefulshutdownserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
)

var (
	ErrWorkerPoolShutdown = errors.New("worker pool is shutdown")
	ErrTaskQueueFull      = errors.New("task queue is full")
)

type Task[R any] func(ctx context.Context) (R, error)

type WorkerPool[R any] interface {
	Submit(ctx context.Context, task Task[R]) *Future[R]
	Shutdown()
}

type workerPool[R any] struct {
	ctx             context.Context
	cancel          context.CancelCauseFunc
	size            int
	workerWaitGroup sync.WaitGroup
	taskQueue       chan *Future[R]
}

func NewWorkerPool[R any](ctx context.Context, size int) WorkerPool[R] {
	if size <= 0 {
		size = 1
	}

	poolCtx, cancel := context.WithCancelCause(ctx)
	wp := &workerPool[R]{
		ctx:             poolCtx,
		cancel:          cancel,
		size:            size,
		workerWaitGroup: sync.WaitGroup{},
		taskQueue:       make(chan *Future[R], size*2),
	}

	for i := 0; i < size; i++ {
		go wp.worker()
	}

	return wp
}

func (wp *workerPool[R]) Submit(ctx context.Context, task Task[R]) *Future[R] {
	if wp.IsShutdown() {
		return NewFuture[R](ctx, func(ctx context.Context) (R, error) {
			var zero R
			return zero, ErrWorkerPoolShutdown
		})
	}

	future := NewFuture[R](ctx, task)
	select {
	case wp.taskQueue <- future:
		return future
	default:
		return NewFuture[R](ctx, func(ctx context.Context) (R, error) {
			var zero R
			return zero, ErrTaskQueueFull
		})
	}
}

func (wp *workerPool[R]) Shutdown() {
	log.Default().Println("shutting down worker pool")
	if wp.IsShutdown() {
		return
	}
	wp.cancel(ErrWorkerPoolShutdown)
	wp.workerWaitGroup.Wait()
	log.Default().Println("worker pool stopped")
}

func (wp *workerPool[R]) IsShutdown() bool {
	return wp.ctx.Err() != nil
}

func (wp *workerPool[R]) worker() {
	wp.workerWaitGroup.Add(1)
	defer wp.workerWaitGroup.Done()

	for {
		if wp.IsShutdown() {
			return
		}

		select {
		case future := <-wp.taskQueue:
			future.Run()
		case <-wp.ctx.Done():
			return
		}
	}
}

type Future[R any] struct {
	ctx      context.Context
	task     Task[R]
	callback func(R, error)
}

func NewFuture[R any](ctx context.Context, task Task[R]) *Future[R] {
	ctx, cancel := context.WithCancelCause(ctx)
	return &Future[R]{
		ctx:  ctx,
		task: task,
		callback: func(result R, err error) {
			cancel(&futureResult[R]{
				result: result,
				err:    err,
			})
		},
	}
}

func (f *Future[R]) Run() {
	if f.IsDone() {
		return
	}

	result, err := f.invoke()
	f.callback(result, err)
}

func (f *Future[R]) Get() (R, error) {
	<-f.Done()

	cause := context.Cause(f.ctx)
	if cause != nil {
		var resultErr *futureResult[R]
		if ok := errors.As(cause, &resultErr); ok {
			return resultErr.result, resultErr.err
		}
		return *new(R), cause
	}

	var zero R
	return zero, cause
}

func (f *Future[R]) Cancel() {
	var zero R
	f.callback(zero, context.Canceled)
}

func (f *Future[R]) Done() <-chan struct{} {
	return f.ctx.Done()
}

func (f *Future[R]) IsDone() bool {
	return f.ctx.Err() != nil
}

func (f *Future[R]) invoke() (R, error) {
	defer func() {
		if r := recover(); r != nil {
			var zero R
			f.callback(zero, fmt.Errorf("task panicked: %v", r))
		}
	}()

	return f.task(f.ctx)
}

type futureResult[R any] struct {
	result R
	err    error
}

func (fr *futureResult[R]) Error() string {
	if fr.err != nil {
		return fr.err.Error()
	}
	return fmt.Sprintf("future result: %v", fr.result)
}
