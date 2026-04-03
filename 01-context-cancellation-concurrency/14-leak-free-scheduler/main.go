package main

import (
	"context"
	"log/slog"
	"math/rand"
	"time"
)

type Scheduler struct {
	Interval time.Duration
	Jitter   float64
}

func (s *Scheduler) Run(ctx context.Context, job func(ctx2 context.Context) error) error {
	timer := time.NewTimer(s.durationWithJitter())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			start := time.Now()
			err := job(ctx)
			dur := time.Since(start)
			if err != nil {
				slog.Error("job error", "error", err, "duration", dur)
			} else {
				slog.Info("job done", "duration", dur)
			}
			timer.Reset(s.durationWithJitter())
		}
	}
}

func (s *Scheduler) durationWithJitter() time.Duration {
	delta := float64(s.Interval) * s.Jitter * (2*rand.Float64() - 1)
	return s.Interval + time.Duration(delta)
}
