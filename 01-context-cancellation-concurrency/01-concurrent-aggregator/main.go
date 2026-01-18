package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"golang.org/x/sync/errgroup"
)

// ErrNoServices is returned when no services are configured
var ErrNoServices = errors.New("no services configured")

// ErrInvalidUserID is returned when userID is empty
var ErrInvalidUserID = errors.New("userID cannot be empty")

// Options is a function that configures a UserAggregator
type Options func(*UserAggregator)

// WithServices configures the aggregator with the given services
func WithServices(services ...Service) Options {
	return func(ua *UserAggregator) {
		ua.services = services
	}
}

// WithTimeout configures the aggregator with a timeout
func WithTimeout(timeout time.Duration) Options {
	return func(ua *UserAggregator) {
		ua.timeout = timeout
	}
}

// WithLogger configures the aggregator with a custom logger
func WithLogger(logger *slog.Logger) Options {
	return func(ua *UserAggregator) {
		if logger != nil {
			ua.logger = logger
		}
	}
}

// UserAggregator aggregates data from multiple services concurrently
type UserAggregator struct {
	services []Service
	timeout  time.Duration
	logger   *slog.Logger
}

// NewUserAggregator creates a new UserAggregator with the given options
func NewUserAggregator(opts ...Options) *UserAggregator {
	ua := &UserAggregator{
		services: []Service{},
		timeout:  0,
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}

	for _, opt := range opts {
		opt(ua)
	}

	return ua
}

// Aggregate fetches data from all services concurrently and aggregates the results.
// It returns immediately if any service fails (fail-fast behavior).
// If a timeout is configured, it will cancel all operations when the timeout is reached.
func (ua *UserAggregator) Aggregate(ctx context.Context, userID string) ([]string, error) {
	// Input validation
	if userID == "" {
		ua.logger.Error("aggregation failed", slog.String("error", ErrInvalidUserID.Error()))
		return nil, ErrInvalidUserID
	}
	if len(ua.services) == 0 {
		ua.logger.Warn("no services configured, returning empty result")
		return []string{}, nil
	}

	ctx, cancel := ua.createContextWithTimeout(ctx)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)
	resultChan := make(chan string, len(ua.services))
	for _, svc := range ua.services {
		svc := svc
		g.Go(func() error {
			data, err := svc.FetchData(ctx, userID)
			if err != nil {
				ua.logger.Error("service fetch failed",
					slog.String("error", err.Error()),
					slog.String("userID", userID),
				)
				return err
			}
			resultChan <- data
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		ua.logger.Error("aggregation failed",
			slog.String("error", err.Error()),
			slog.String("userID", userID),
			slog.Int("serviceCount", len(ua.services)),
		)
		return nil, err
	}

	close(resultChan)
	results := make([]string, 0, len(ua.services))
	for data := range resultChan {
		results = append(results, data)
	}

	ua.logger.Info("aggregation succeeded",
		slog.String("userID", userID),
		slog.Int("resultCount", len(results)),
		slog.Any("results", results),
	)
	return results, nil
}

// createContextWithTimeout creates a context with timeout if configured
func (ua *UserAggregator) createContextWithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ua.timeout > 0 {
		return context.WithTimeout(ctx, ua.timeout)
	}
	return context.WithCancel(ctx)
}

// Service defines the interface for data fetching services
type Service interface {
	FetchData(ctx context.Context, id string) (string, error)
}

// ProfileService is a mock service that fetches user profile data
type ProfileService struct {
	processTimeout time.Duration
	shouldFail     bool
}

// NewProfileService creates a new ProfileService
func NewProfileService(processTimeout time.Duration, shouldFail bool) *ProfileService {
	return &ProfileService{
		processTimeout: processTimeout,
		shouldFail:     shouldFail,
	}
}

// FetchData simulates fetching user profile data
func (ps *ProfileService) FetchData(ctx context.Context, id string) (string, error) {
	timer := time.NewTimer(ps.processTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("[ProfileService] %w", ctx.Err())
	case <-timer.C:
		if ps.shouldFail {
			return "", errors.New("[ProfileService] failed to fetch data")
		}
		return "User: Alice", nil
	}
}

// OrderService is a mock service that fetches user order data
type OrderService struct {
	processTimeout time.Duration
	shouldFail     bool
}

// NewOrderService creates a new OrderService
func NewOrderService(processTimeout time.Duration, shouldFail bool) *OrderService {
	return &OrderService{
		processTimeout: processTimeout,
		shouldFail:     shouldFail,
	}
}

// FetchData simulates fetching user order data
func (os *OrderService) FetchData(ctx context.Context, id string) (string, error) {
	timer := time.NewTimer(os.processTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("[OrderService] %w", ctx.Err())
	case <-timer.C:
		if os.shouldFail {
			return "", errors.New("[OrderService] failed to fetch data")
		}
		return "Orders: 5", nil
	}
}
