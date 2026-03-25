package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFanOutClient_FetchAll(t *testing.T) {
	t.Run("success_all", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.URL.Query().Get("userid")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf("data-%s", userID)))
		}))
		defer server.Close()

		client := NewFanOutClient(server.URL)
		userIDs := []int{1, 2, 3, 4, 5}

		results, err := client.FetchAll(context.Background(), userIDs)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(results) != len(userIDs) {
			t.Errorf("expected %d results, got %d", len(userIDs), len(results))
		}

		for _, id := range userIDs {
			expected := fmt.Sprintf("data-%d", id)
			if string(results[id]) != expected {
				t.Errorf("expected %s for ID %d, got %s", expected, id, string(results[id]))
			}
		}
	})

	t.Run("concurrency_limit", func(t *testing.T) {
		var maxInFlight int32
		var currentInFlight int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&currentInFlight, 1)
			defer atomic.AddInt32(&currentInFlight, -1)

			// Record max
			if val := atomic.LoadInt32(&currentInFlight); val > atomic.LoadInt32(&maxInFlight) {
				atomic.StoreInt32(&maxInFlight, val)
			}

			time.Sleep(100 * time.Millisecond) // Hold the connection
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewFanOutClient(server.URL)
		// Request more than maxInFlight (8)
		userIDs := make([]int, 20)
		for i := range userIDs {
			userIDs[i] = i
		}

		_, err := client.FetchAll(context.Background(), userIDs)
		if err != nil {
			t.Fatal(err)
		}

		if maxInFlight > 8 {
			t.Errorf("concurrency exceeded limit of 8: reached %d", maxInFlight)
		}
	})

	t.Run("rate_limit", func(t *testing.T) {
		var timestamps []time.Time
		var mu sync.Mutex

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			timestamps = append(timestamps, time.Now())
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewFanOutClient(server.URL)
		// 30 requests should take more than 1 second (10 req/s with burst 20)
		// First 20 are burst, next 10 take ~1s. 
		userIDs := make([]int, 30)
		for i := range userIDs {
			userIDs[i] = i
		}

		start := time.Now()
		_, err := client.FetchAll(context.Background(), userIDs)
		if err != nil {
			t.Fatal(err)
		}
		duration := time.Since(start)

		if duration < 500*time.Millisecond {
			t.Errorf("rate limit failed: 30 requests finished too fast in %v", duration)
		}
	})

	t.Run("fail_fast", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.URL.Query().Get("userid")
			if userID == "3" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			time.Sleep(50 * time.Millisecond) // Let others start
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewFanOutClient(server.URL)
		userIDs := []int{1, 2, 3, 4, 5}

		_, err := client.FetchAll(context.Background(), userIDs)
		if err == nil {
			t.Fatal("expected error from userID 3")
		}
	})
}
