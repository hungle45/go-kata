package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ExpectedResult = []string{"User: Alice", "Orders: 5"}

func TestUserAggregator_Aggregate(t *testing.T) {
	tests := []struct {
		name           string
		services       []Service
		timeout        time.Duration
		contextTimeout time.Duration
		cancelContext  bool
		userID         string
		wantResults    []string
		wantErr        bool
		errContains    string
		maxDuration    time.Duration
		minDuration    time.Duration
	}{
		{
			name: "success - both services return quickly",
			services: []Service{
				NewProfileService(0, false),
				NewOrderService(0, false),
			},
			timeout:     0,
			userID:      "user-123",
			wantResults: ExpectedResult,
			wantErr:     false,
			maxDuration: 500 * time.Millisecond,
		},
		{
			name: "success - multiple services with varying times",
			services: []Service{
				NewProfileService(50*time.Millisecond, false),
				NewOrderService(100*time.Millisecond, false),
			},
			timeout:     1 * time.Second,
			userID:      "user-multi",
			wantResults: ExpectedResult,
			wantErr:     false,
			maxDuration: 500 * time.Millisecond,
		},
		{
			name: "timeout - slow service exceeds aggregator timeout",
			services: []Service{
				NewProfileService(2*time.Second, false),
				NewOrderService(0, false),
			},
			timeout:     1 * time.Second,
			userID:      "user-123",
			wantResults: nil,
			wantErr:     true,
			errContains: "context deadline exceeded",
			maxDuration: 1500 * time.Millisecond,
			minDuration: 900 * time.Millisecond,
		},
		{
			name: "fail-fast - profile service fails immediately",
			services: []Service{
				NewProfileService(0, true),
				NewOrderService(10*time.Second, false),
			},
			timeout:     0,
			userID:      "user-123",
			wantResults: nil,
			wantErr:     true,
			errContains: "failed to fetch data",
			maxDuration: 1 * time.Second,
		},
		{
			name: "fail-fast - order service fails immediately",
			services: []Service{
				NewProfileService(10*time.Second, false),
				NewOrderService(0, true),
			},
			timeout:     0,
			userID:      "user-123",
			wantResults: nil,
			wantErr:     true,
			errContains: "failed to fetch data",
			maxDuration: 1 * time.Second,
		},
		{
			name: "multiple failures - first error is returned",
			services: []Service{
				NewProfileService(10*time.Millisecond, true),
				NewOrderService(20*time.Millisecond, true),
			},
			timeout:     0,
			userID:      "user-789",
			wantResults: nil,
			wantErr:     true,
			errContains: "failed to fetch data",
			maxDuration: 500 * time.Millisecond,
		},
		{
			name:        "empty services - should succeed with empty results",
			services:    []Service{},
			timeout:     0,
			userID:      "user-empty",
			wantResults: []string{},
			wantErr:     false,
			maxDuration: 100 * time.Millisecond,
		},
		{
			name: "invalid input - empty userID",
			services: []Service{
				NewProfileService(0, false),
				NewOrderService(0, false),
			},
			timeout:     0,
			userID:      "",
			wantResults: nil,
			wantErr:     true,
			errContains: "userID cannot be empty",
			maxDuration: 100 * time.Millisecond,
		},
		{
			name: "pre-cancelled context",
			services: []Service{
				NewProfileService(100*time.Millisecond, false),
				NewOrderService(100*time.Millisecond, false),
			},
			timeout:       0,
			cancelContext: true,
			userID:        "user-cancelled",
			wantResults:   nil,
			wantErr:       true,
			errContains:   "context canceled",
			maxDuration:   150 * time.Millisecond,
		},
		{
			name: "context timeout before aggregator timeout",
			services: []Service{
				NewProfileService(2*time.Second, false),
				NewOrderService(2*time.Second, false),
			},
			timeout:        5 * time.Second,
			contextTimeout: 500 * time.Millisecond,
			userID:         "user-ctx-timeout",
			wantResults:    nil,
			wantErr:        true,
			errContains:    "context deadline exceeded",
			maxDuration:    1 * time.Second,
			minDuration:    400 * time.Millisecond,
		},
		{
			name: "mixed success and slow - timeout triggers",
			services: []Service{
				NewProfileService(10*time.Millisecond, false),
				NewOrderService(5*time.Second, false),
			},
			timeout:     500 * time.Millisecond,
			userID:      "user-mixed",
			wantResults: nil,
			wantErr:     true,
			errContains: "context deadline exceeded",
			maxDuration: 1 * time.Second,
			minDuration: 400 * time.Millisecond,
		},
		{
			name: "both services timeout simultaneously",
			services: []Service{
				NewProfileService(2*time.Second, false),
				NewOrderService(2*time.Second, false),
			},
			timeout:     1 * time.Second,
			userID:      "user-simultaneous",
			wantResults: nil,
			wantErr:     true,
			errContains: "context deadline exceeded",
			maxDuration: 1500 * time.Millisecond,
			minDuration: 900 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.contextTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.contextTimeout)
				defer cancel()
			} else if tt.cancelContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				defer cancel()
			}

			opts := []Options{WithServices(tt.services...)}
			if tt.timeout > 0 {
				opts = append(opts, WithTimeout(tt.timeout))
			}
			aggregator := NewUserAggregator(opts...)

			start := time.Now()
			results, err := aggregator.Aggregate(ctx, tt.userID)
			duration := time.Since(start)

			if tt.wantErr {
				require.Error(t, err, "Expected an error but got none")
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains,
						"Error message should contain expected text")
				}
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)
			}

			if tt.wantResults != nil {
				require.Equal(t, len(tt.wantResults), len(results),
					"Result count mismatch")
				for _, expected := range tt.wantResults {
					assert.Contains(t, results, expected,
						"Results should contain expected value")
				}
			} else if !tt.wantErr {
				assert.Empty(t, results, "Expected empty results")
			}

			if tt.maxDuration > 0 {
				assert.Less(t, duration, tt.maxDuration,
					"Operation took longer than expected: %v > %v", duration, tt.maxDuration)
			}
			if tt.minDuration > 0 {
				assert.GreaterOrEqual(t, duration, tt.minDuration,
					"Operation completed too quickly: %v < %v", duration, tt.minDuration)
			}

			t.Logf("Duration: %v, Error: %v", duration, err)
		})
	}
}

func TestUserAggregator_Options(t *testing.T) {
	tests := []struct {
		name            string
		options         []Options
		expectedTimeout time.Duration
		expectedSvcLen  int
	}{
		{
			name:            "default options",
			options:         []Options{},
			expectedTimeout: 0,
			expectedSvcLen:  0,
		},
		{
			name: "with timeout",
			options: []Options{
				WithTimeout(5 * time.Second),
			},
			expectedTimeout: 5 * time.Second,
			expectedSvcLen:  0,
		},
		{
			name: "with services",
			options: []Options{
				WithServices(
					NewProfileService(0, false),
					NewOrderService(0, false),
				),
			},
			expectedTimeout: 0,
			expectedSvcLen:  2,
		},
		{
			name: "with all options",
			options: []Options{
				WithTimeout(3 * time.Second),
				WithServices(
					NewProfileService(0, false),
					NewOrderService(0, false),
				),
			},
			expectedTimeout: 3 * time.Second,
			expectedSvcLen:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aggregator := NewUserAggregator(tt.options...)

			assert.Equal(t, tt.expectedTimeout, aggregator.timeout,
				"Timeout should match expected value")
			assert.Equal(t, tt.expectedSvcLen, len(aggregator.services),
				"Service count should match expected value")
			assert.NotNil(t, aggregator.logger,
				"Logger should always be initialized")
		})
	}
}

func TestServices(t *testing.T) {
	tests := []struct {
		name        string
		service     Service
		timeout     time.Duration
		userID      string
		wantData    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "ProfileService - success",
			service:  NewProfileService(10*time.Millisecond, false),
			timeout:  1 * time.Second,
			userID:   "user-1",
			wantData: "User: Alice",
			wantErr:  false,
		},
		{
			name:        "ProfileService - failure",
			service:     NewProfileService(10*time.Millisecond, true),
			timeout:     1 * time.Second,
			userID:      "user-2",
			wantData:    "",
			wantErr:     true,
			errContains: "failed to fetch data",
		},
		{
			name:        "ProfileService - context timeout",
			service:     NewProfileService(2*time.Second, false),
			timeout:     100 * time.Millisecond,
			userID:      "user-3",
			wantData:    "",
			wantErr:     true,
			errContains: "context deadline exceeded",
		},
		{
			name:     "OrderService - success",
			service:  NewOrderService(10*time.Millisecond, false),
			timeout:  1 * time.Second,
			userID:   "user-4",
			wantData: "Orders: 5",
			wantErr:  false,
		},
		{
			name:        "OrderService - failure",
			service:     NewOrderService(10*time.Millisecond, true),
			timeout:     1 * time.Second,
			userID:      "user-5",
			wantData:    "",
			wantErr:     true,
			errContains: "failed to fetch data",
		},
		{
			name:        "OrderService - context timeout",
			service:     NewOrderService(2*time.Second, false),
			timeout:     100 * time.Millisecond,
			userID:      "user-6",
			wantData:    "",
			wantErr:     true,
			errContains: "context deadline exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			data, err := tt.service.FetchData(ctx, tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantData, data)
			}
		})
	}
}

func BenchmarkUserAggregator_Aggregate(b *testing.B) {
	aggregator := NewUserAggregator(
		WithServices(
			NewProfileService(1*time.Millisecond, false),
			NewOrderService(1*time.Millisecond, false),
		),
		WithTimeout(5*time.Second),
	)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = aggregator.Aggregate(ctx, "user-bench")
	}
}
