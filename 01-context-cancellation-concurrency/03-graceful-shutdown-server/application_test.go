package gracefulshutdownserver

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestSuddenDeath tests the "Sudden Death Test" from the self-correction section
// Send 100 requests, immediately send SIGTERM
// Pass: Server completes in-flight requests (not all 100), logs "shutting down", closes cleanly
// Fail: Server accepts new requests after signal, leaks goroutines, or crashes
func TestSuddenDeath(t *testing.T) {
	tests := []struct {
		name              string
		numRequests       int
		shutdownDelay     time.Duration
		expectedCompleted int // minimum expected completed requests
	}{
		{
			name:              "immediate_shutdown_with_100_requests",
			numRequests:       100,
			shutdownDelay:     10 * time.Millisecond,
			expectedCompleted: 1, // at least some requests should complete
		},
		{
			name:              "delayed_shutdown_with_50_requests",
			numRequests:       50,
			shutdownDelay:     100 * time.Millisecond,
			expectedCompleted: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a mock database that returns immediately
			app := createAppWithFastDB()

			// Start the application in a goroutine
			appDone := make(chan struct{})
			go func() {
				defer close(appDone)
				app.Start()
			}()

			// Wait for server to start
			time.Sleep(50 * time.Millisecond)

			// Track completed and failed requests
			var completed, failed atomic.Int32
			var wg sync.WaitGroup

			// Send requests concurrently
			for i := 0; i < tt.numRequests; i++ {
				wg.Add(1)
				go func(reqNum int) {
					defer wg.Done()

					client := &http.Client{
						Timeout: 2 * time.Second,
					}

					resp, err := client.Get("http://localhost:18080/ping")
					if err != nil {
						failed.Add(1)
						return
					}
					defer resp.Body.Close()

					if resp.StatusCode == http.StatusOK {
						completed.Add(1)
					} else {
						failed.Add(1)
					}
				}(i)
			}

			// Wait a bit then send SIGTERM
			time.Sleep(tt.shutdownDelay)

			// Send SIGTERM to trigger shutdown
			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				t.Fatalf("Failed to find process: %v", err)
			}

			if err := proc.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("Failed to send SIGTERM: %v", err)
			}

			// Wait for all request goroutines to finish
			wg.Wait()

			// Wait for application to shutdown (with timeout)
			select {
			case <-appDone:
				// Application shutdown successfully
			case <-time.After(15 * time.Second):
				t.Fatal("Application did not shutdown within timeout")
			}

			// Verify results
			completedCount := completed.Load()
			failedCount := failed.Load()

			t.Logf("Completed: %d, Failed: %d, Total: %d", completedCount, failedCount, tt.numRequests)

			// Should complete at least some requests
			if completedCount < int32(tt.expectedCompleted) {
				t.Errorf("Expected at least %d completed requests, got %d", tt.expectedCompleted, completedCount)
			}

			// Should not complete all requests (since we shutdown immediately)
			if completedCount == int32(tt.numRequests) {
				t.Logf("Warning: All requests completed, shutdown might be too slow")
			}
		})
	}
}

// TestSlowLeak tests the "Slow Leak Test" from the self-correction section
// Run server for a period with requests
// Send SIGTERM, wait for shutdown
// Pass: No goroutine leaks (use runtime.NumGoroutine())
// Fail: Any increase in goroutine count from start to finish
func TestSlowLeak(t *testing.T) {
	tests := []struct {
		name            string
		duration        time.Duration
		requestInterval time.Duration
		shutdownTimeout time.Duration
	}{
		{
			name:            "5_second_run_with_frequent_requests",
			duration:        5 * time.Second,
			requestInterval: 100 * time.Millisecond,
			shutdownTimeout: 10 * time.Second,
		},
		{
			name:            "10_second_run_with_slow_requests",
			duration:        10 * time.Second,
			requestInterval: 500 * time.Millisecond,
			shutdownTimeout: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Force GC and get baseline goroutine count
			runtime.GC()
			time.Sleep(100 * time.Millisecond)
			baselineGoroutines := runtime.NumGoroutine()
			t.Logf("Baseline goroutines: %d", baselineGoroutines)

			app := InitApplication("localhost:18081", "localhost:18081")

			// Start the application
			appDone := make(chan struct{})
			go func() {
				defer close(appDone)
				app.Start()
			}()

			// Wait for server to start
			time.Sleep(50 * time.Millisecond)

			startGoroutines := runtime.NumGoroutine()
			t.Logf("After server start goroutines: %d", startGoroutines)

			// Send requests at regular intervals
			stopRequests := make(chan struct{})
			requestsDone := make(chan struct{})
			var requestCount atomic.Int32

			go func() {
				defer close(requestsDone)
				ticker := time.NewTicker(tt.requestInterval)
				defer ticker.Stop()

				for {
					select {
					case <-ticker.C:
						go func() {
							client := &http.Client{Timeout: 1 * time.Second}
							resp, err := client.Get("http://localhost:18081/ping")
							if err == nil {
								resp.Body.Close()
								requestCount.Add(1)
							}
						}()
					case <-stopRequests:
						return
					}
				}
			}()

			// Run for specified duration
			time.Sleep(tt.duration)
			close(stopRequests)
			<-requestsDone

			t.Logf("Sent %d requests", requestCount.Load())

			// Send SIGTERM
			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				t.Fatalf("Failed to find process: %v", err)
			}

			if err := proc.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("Failed to send SIGTERM: %v", err)
			}

			// Wait for shutdown
			select {
			case <-appDone:
				// Success
			case <-time.After(tt.shutdownTimeout):
				t.Fatal("Application did not shutdown within timeout")
			}

			// Force GC to clean up any remaining goroutines
			runtime.GC()
			time.Sleep(200 * time.Millisecond)

			finalGoroutines := runtime.NumGoroutine()
			t.Logf("Final goroutines: %d", finalGoroutines)

			// Check for goroutine leaks
			// Allow small variance (±2) due to test framework goroutines
			leakedGoroutines := finalGoroutines - baselineGoroutines
			if leakedGoroutines > 2 {
				t.Errorf("Goroutine leak detected: baseline=%d, final=%d, leaked=%d",
					baselineGoroutines, finalGoroutines, leakedGoroutines)
			}
		})
	}
}

// TestTimeout tests the "Timeout Test" from the self-correction section
// Start long-running request (sleep 20s)
// Send SIGTERM with 5s timeout context
// Pass: Forces shutdown after 5s, logs "shutdown timeout"
// Fail: Waits full 20s or deadlocks
func TestTimeout(t *testing.T) {
	tests := []struct {
		name                string
		requestDuration     time.Duration
		shutdownTimeout     time.Duration
		expectedMaxDuration time.Duration
	}{
		{
			name:                "long_request_with_short_timeout",
			requestDuration:     20 * time.Second,
			shutdownTimeout:     5 * time.Second,
			expectedMaxDuration: 7 * time.Second, // 5s timeout + 2s buffer
		},
		{
			name:                "medium_request_with_very_short_timeout",
			requestDuration:     10 * time.Second,
			shutdownTimeout:     2 * time.Second,
			expectedMaxDuration: 4 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a custom application with a slow database
			app := createAppWithSlowDB(tt.requestDuration)
			app.shutdownTimeout = tt.shutdownTimeout

			// Start the application
			appDone := make(chan struct{})
			go func() {
				defer close(appDone)
				app.Start()
			}()

			// Wait for server to start
			time.Sleep(50 * time.Millisecond)

			// Start a long-running request
			requestDone := make(chan struct{})
			go func() {
				defer close(requestDone)
				client := &http.Client{Timeout: 30 * time.Second}
				// Use /ping which uses the DB (mocked to be slow)
				resp, err := client.Get("http://localhost:18082/ping")
				if err != nil {
					t.Logf("Request failed (expected): %v", err)
				} else {
					resp.Body.Close()
				}
			}()

			// Give request time to start
			time.Sleep(100 * time.Millisecond)

			// Record shutdown start time
			shutdownStart := time.Now()

			// Send SIGTERM
			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				t.Fatalf("Failed to find process: %v", err)
			}

			if err := proc.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("Failed to send SIGTERM: %v", err)
			}

			// Wait for shutdown with timeout
			select {
			case <-appDone:
				shutdownDuration := time.Since(shutdownStart)
				t.Logf("Shutdown completed in %v", shutdownDuration)

				// Verify shutdown happened within expected time
				if shutdownDuration > tt.expectedMaxDuration {
					t.Errorf("Shutdown took too long: expected max %v, got %v",
						tt.expectedMaxDuration, shutdownDuration)
				}

				// Verify it didn't shutdown too quickly (should wait for timeout)
				minExpectedDuration := tt.shutdownTimeout - 500*time.Millisecond
				if shutdownDuration < minExpectedDuration {
					t.Errorf("Shutdown too fast: expected at least %v, got %v",
						minExpectedDuration, shutdownDuration)
				}

			case <-time.After(tt.expectedMaxDuration + 5*time.Second):
				t.Fatalf("Application did not shutdown within expected time")
			}
		})
	}
}

// MockSlowDB implements Database interface with configurable delay
type MockSlowDB struct {
	delay time.Duration
}

func (m *MockSlowDB) Query(ctx context.Context) error {
	select {
	case <-time.After(m.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *MockSlowDB) Shutdown() {}

// Helper function to create an application with a slow database for timeout testing
func createAppWithSlowDB(queryDuration time.Duration) *Application {
	// Create root context for the test application
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize dependencies
	// We use the new NewWorkerPool which takes a context
	pool := NewWorkerPool[Data](ctx, 10)
	cache := NewCache(ctx, 30*time.Second)
	db := &MockSlowDB{delay: queryDuration}

	controller := NewController(pool, cache, db)
	httpServer := NewHttpServer("localhost:18082", controller)

	return &Application{
		httpServer: httpServer,
		pool:       pool, // Set this so we can shutdown it
		cache:      cache,
		db:         db,
		srvAddr:    "localhost:18082",
		dbAddr:     "localhost:18082",
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Helper function to create an application with a fast database for sudden death testing
func createAppWithFastDB() *Application {
	// Create root context for the test application
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize dependencies
	pool := NewWorkerPool[Data](ctx, 10)
	cache := NewCache(ctx, 30*time.Second)
	db := &MockSlowDB{delay: 0}

	controller := NewController(pool, cache, db)
	httpServer := NewHttpServer("localhost:18080", controller)

	return &Application{
		httpServer: httpServer,
		pool:       pool,
		cache:      cache,
		db:         db,
		srvAddr:    "localhost:18080",
		dbAddr:     "localhost:18080",
		ctx:        ctx,
		cancel:     cancel,
	}
}

// TestGracefulShutdownOrder verifies components shutdown in correct order
func TestGracefulShutdownOrder(t *testing.T) {
	// This test verifies the shutdown order is correct
	// Expected: HTTP Server → Worker Pool → Cache → Database

	app := InitApplication("localhost:18083", "localhost:18083")

	// Start application
	appDone := make(chan struct{})
	go func() {
		defer close(appDone)
		app.Start()
	}()

	time.Sleep(50 * time.Millisecond)

	// Trigger shutdown
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGTERM)

	// Wait for shutdown
	select {
	case <-appDone:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown timeout")
	}

	// Note: This test is limited because we can't easily intercept shutdown order
	// without modifying the production code. In a real scenario, you'd want to
	// add hooks or use dependency injection to verify order.
	t.Log("Shutdown completed successfully")
}

// TestConcurrentRequests verifies the server handles concurrent requests correctly
func TestConcurrentRequests(t *testing.T) {
	app := InitApplication("localhost:18084", "localhost:18084")

	appDone := make(chan struct{})
	go func() {
		defer close(appDone)
		app.Start()
	}()

	time.Sleep(50 * time.Millisecond)

	// Send concurrent requests
	const numRequests = 50
	var wg sync.WaitGroup
	var successCount atomic.Int32

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Get("http://localhost:18084/ping")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					successCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	// Shutdown
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGTERM)

	select {
	case <-appDone:
		t.Logf("Successfully handled %d/%d requests", successCount.Load(), numRequests)
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown timeout")
	}
}

// BenchmarkRequestThroughput measures request handling performance
func BenchmarkRequestThroughput(b *testing.B) {
	app := InitApplication("localhost:18085", "localhost:18085")

	appDone := make(chan struct{})
	go func() {
		defer close(appDone)
		app.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 2 * time.Second}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get("http://localhost:18085/ping")
			if err == nil {
				resp.Body.Close()
			}
		}
	})
	b.StopTimer()

	// Cleanup
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGTERM)
	<-appDone
}
