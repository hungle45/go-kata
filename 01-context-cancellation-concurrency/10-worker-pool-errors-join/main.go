package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Job func(context.Context) error

type Option func(*Pool)

func WithStopOnFirstError() Option {
	return func(p *Pool) {
		p.stopOnFirstError = true
	}
}

type Pool struct {
	workerCount      int
	stopOnFirstError bool
}

func NewPool(workerCount int, opts ...Option) *Pool {
	p := &Pool{
		workerCount: workerCount,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Pool) Run(ctx context.Context, jobs <-chan Job) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan error, p.workerCount)
	var wg sync.WaitGroup

	for i := 0; i < p.workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.runWorker(ctx, jobs, results)
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var aggregateErr error
	for err := range results {
		if err == nil {
			continue
		}

		if p.stopOnFirstError {
			return err
		}

		aggregateErr = errors.Join(aggregateErr, err)
	}

	return errors.Join(aggregateErr, ctx.Err())
}

func (p *Pool) runWorker(ctx context.Context, jobs <-chan Job, results chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}

			err := p.safeExecute(ctx, job)

			select {
			case <-ctx.Done():
				return
			case results <- err:
			}
		}
	}
}

func (p *Pool) safeExecute(ctx context.Context, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return job(ctx)
}
