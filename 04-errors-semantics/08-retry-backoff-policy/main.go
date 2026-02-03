package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"sync"
	"time"
)

type Retryer struct {
	baseDelay   time.Duration
	maxDelay    time.Duration
	jitter      time.Duration
	maxAttempts int
	rand        *rand.Rand
	mu          sync.Mutex
}

func NewRetryer(opts ...Options) *Retryer {
	retryer := &Retryer{
		baseDelay:   100 * time.Millisecond,
		maxDelay:    5 * time.Second,
		maxAttempts: 3,
		jitter:      0,
		rand:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, opt := range opts {
		opt(retryer)
	}

	return retryer
}

func (r *Retryer) Do(ctx context.Context, fn func(ctx2 context.Context) error) error {
	if r.maxAttempts <= 0 {
		return fn(ctx)
	}

	var lastErr error
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for attempt := range r.maxAttempts {
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}

		if !r.shouldRetry(lastErr) {
			return lastErr
		}

		if attempt < r.maxAttempts-1 {
			var err error
			if timer, err = r.backoff(ctx, timer, attempt); err != nil {
				return err
			}
		}
	}

	return fmt.Errorf("%w after %d attempts: %w", ErrMaxRetryReached, r.maxAttempts, lastErr)
}
func (r *Retryer) shouldRetry(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, ErrTransient) {
		return true
	}
	return false
}

func (r *Retryer) backoff(ctx context.Context, t *time.Timer, attempt int) (*time.Timer, error) {
	delay := r.calcBackoffTime(attempt)
	if t == nil {
		t = time.NewTimer(delay)
	} else {
		r.resetTimer(t, delay)
	}

	select {
	case <-ctx.Done():
		return t, ctx.Err()
	case <-t.C:
		return t, nil
	}
}

func (r *Retryer) resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

func (r *Retryer) calcBackoffTime(attempt int) time.Duration {
	backOff := r.baseDelay * time.Duration(math.Pow(2, float64(attempt)))
	if r.jitter > 0 {
		r.mu.Lock()
		backOff = backOff + time.Duration(r.rand.Int63n(int64(r.jitter)))
		r.mu.Unlock()
	}

	if backOff > r.maxDelay {
		return r.maxDelay
	}
	return backOff
}

type Options func(retryer *Retryer)

func WithBaseDelay(delay time.Duration) Options {
	return func(retryer *Retryer) {
		retryer.baseDelay = delay
	}
}

func WithMaxDelay(delay time.Duration) Options {
	return func(retryer *Retryer) {
		retryer.maxDelay = delay
	}
}

func WithJitter(jitter time.Duration) Options {
	return func(retryer *Retryer) {
		retryer.jitter = jitter
	}
}

func WithMaxAttempts(attempts int) Options {
	return func(retryer *Retryer) {
		retryer.maxAttempts = attempts
	}
}

func WithRandSource(source rand.Source) Options {
	return func(retryer *Retryer) {
		retryer.rand = rand.New(source)
	}
}

var (
	ErrMaxRetryReached = errors.New("max retry reached")
	ErrTransient       = errors.New("transient error")
)
