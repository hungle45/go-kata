package main

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func makeURLs(n int) []string {
	urls := make([]string, n)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://example.com/item/%d", i)
	}
	return urls
}

// drain reads from ch until it is closed or timeout is exceeded.
// It fails the test if the channel is not closed within the deadline.
func drain(t *testing.T, ch <-chan Result, timeout time.Duration) []Result {
	t.Helper()
	var out []Result
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case r, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, r)
		case <-timer.C:
			t.Error("channel was not closed within timeout — producer must close the output channel after all goroutines exit")
			return out
		}
	}
}

// goroutineBaseline stabilises the scheduler and returns the current goroutine count.
func goroutineBaseline() int {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	return runtime.NumGoroutine()
}

// ── Functional Requirements ───────────────────────────────────────────────────

// TestFetch_AllURLsProduceResults verifies that every URL yields exactly one result
// and that the output channel is closed by the producer once all results are delivered.
func TestFetch_AllURLsProduceResults(t *testing.T) {
	tests := []struct {
		name        string
		urlCount    int
		wantResults int
		timeout     time.Duration
	}{
		{
			name:        "empty url list closes channel immediately",
			urlCount:    0,
			wantResults: 0,
			timeout:     2 * time.Second,
		},
		{
			name:        "single url produces one result",
			urlCount:    1,
			wantResults: 1,
			timeout:     3 * time.Second,
		},
		{
			name:        "five urls each produce a result",
			urlCount:    5,
			wantResults: 5,
			timeout:     3 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			ch := NewDataFetcher().Fetch(ctx, makeURLs(tc.urlCount))
			results := drain(t, ch, tc.timeout)

			if len(results) != tc.wantResults {
				t.Errorf("got %d results, want %d", len(results), tc.wantResults)
			}

			// No URL may appear twice.
			seen := make(map[string]bool, len(results))
			for _, r := range results {
				if seen[r.URL] {
					t.Errorf("duplicate result for URL %s", r.URL)
				}
				seen[r.URL] = true
			}
		})
	}
}

// TestFetch_SuccessfulResultsHaveBody verifies that results for URLs that complete
// without cancellation carry a non-empty body and no error.
func TestFetch_SuccessfulResultsHaveBody(t *testing.T) {
	tests := []struct {
		name     string
		urlCount int
	}{
		{name: "1 url", urlCount: 1},
		{name: "3 urls", urlCount: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			ch := NewDataFetcher().Fetch(ctx, makeURLs(tc.urlCount))
			results := drain(t, ch, 5*time.Second)

			for _, r := range results {
				if r.Err != nil {
					continue // canceled results are allowed in partial-result scenarios
				}
				if len(r.Body) == 0 {
					t.Errorf("URL %s: successful result has empty body", r.URL)
				}
			}
		})
	}
}

// TestFetch_ConcurrentExecution verifies that N URLs are fetched concurrently, not sequentially.
// doWork sleeps 200–500 ms per URL, so N concurrent fetches should finish in ~500 ms.
// A sequential run of N=10 would take 2–5 s — far beyond the maxElapsed limit.
func TestFetch_ConcurrentExecution(t *testing.T) {
	tests := []struct {
		name       string
		urlCount   int
		maxElapsed time.Duration
	}{
		{
			name:       "5 urls complete near simultaneously",
			urlCount:   5,
			maxElapsed: 1500 * time.Millisecond,
		},
		{
			name:       "10 urls complete near simultaneously",
			urlCount:   10,
			maxElapsed: 1500 * time.Millisecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			start := time.Now()
			ch := NewDataFetcher().Fetch(ctx, makeURLs(tc.urlCount))
			drain(t, ch, 5*time.Second)
			elapsed := time.Since(start)

			if elapsed > tc.maxElapsed {
				t.Errorf("fetchers appear sequential: elapsed=%v, want < %v — are goroutines started per URL?", elapsed, tc.maxElapsed)
			}
		})
	}
}

// TestFetch_PartialResultsBeforeCancellation verifies requirement:
// "Return partial results that already completed before cancellation."
// At least some goroutines will have finished work before the cancel fires.
func TestFetch_PartialResultsBeforeCancellation(t *testing.T) {
	tests := []struct {
		name        string
		urlCount    int
		cancelAfter time.Duration
		minResults  int // at least this many results must be returned
	}{
		{
			// doWork takes 200-500ms; after 350ms some are done.
			name:        "cancel at 350ms — expect at least 1 result from 10 urls",
			urlCount:    10,
			cancelAfter: 350 * time.Millisecond,
			minResults:  1,
		},
		{
			// After 600ms all 5 doWork calls (200-500ms) should have finished.
			name:        "cancel at 600ms — expect at least 3 results from 5 urls",
			urlCount:    5,
			cancelAfter: 600 * time.Millisecond,
			minResults:  3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())

			ch := NewDataFetcher().Fetch(ctx, makeURLs(tc.urlCount))
			time.AfterFunc(tc.cancelAfter, cancel)

			results := drain(t, ch, 5*time.Second)

			if len(results) < tc.minResults {
				t.Errorf("got %d results before cancel, want at least %d — partial results must not be discarded", len(results), tc.minResults)
			}
			if len(results) > tc.urlCount {
				t.Errorf("got %d results, more than %d URLs — impossible", len(results), tc.urlCount)
			}
		})
	}
}

// ── Goroutine Leak Prevention ─────────────────────────────────────────────────

// TestFetch_NoGoroutineLeak_ForgottenSender is self-correction test 1 from the README:
// start N fetchers, consume only M results, then cancel.
// goroutine count must return near baseline within a short window.
func TestFetch_NoGoroutineLeak_ForgottenSender(t *testing.T) {
	tests := []struct {
		name      string
		urlCount  int
		consumeN  int
		waitAfter time.Duration
		slack     int
	}{
		{
			name:      "50 fetchers consume 1 then cancel",
			urlCount:  50,
			consumeN:  1,
			waitAfter: 2 * time.Second,
			slack:     5,
		},
		{
			name:      "10 fetchers consume 0 then cancel",
			urlCount:  10,
			consumeN:  0,
			waitAfter: 1500 * time.Millisecond,
			slack:     5,
		},
		{
			name:      "20 fetchers consume 5 then cancel",
			urlCount:  20,
			consumeN:  5,
			waitAfter: 2 * time.Second,
			slack:     5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseline := goroutineBaseline()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ch := NewDataFetcher().Fetch(ctx, makeURLs(tc.urlCount))

			for i := 0; i < tc.consumeN; i++ {
				select {
				case <-ch:
				case <-time.After(3 * time.Second):
					t.Fatal("timed out waiting for a result to consume")
				}
			}

			cancel()
			time.Sleep(tc.waitAfter)

			after := runtime.NumGoroutine()
			if after > baseline+tc.slack {
				t.Errorf("goroutine leak detected: baseline=%d, after cancel=%d (allowed slack=%d)", baseline, after, tc.slack)
			}
		})
	}
}

// TestFetch_NoGoroutineLeak_CancelBeforeFirstReceive is self-correction test 2:
// cancel ctx immediately after Fetch — no goroutine may block trying to send.
func TestFetch_NoGoroutineLeak_CancelBeforeFirstReceive(t *testing.T) {
	tests := []struct {
		name      string
		urlCount  int
		waitAfter time.Duration
		slack     int
	}{
		{
			name:      "5 urls, cancel before any receive",
			urlCount:  5,
			waitAfter: 1 * time.Second,
			slack:     5,
		},
		{
			name:      "20 urls, cancel before any receive",
			urlCount:  20,
			waitAfter: 1500 * time.Millisecond,
			slack:     5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseline := goroutineBaseline()

			ctx, cancel := context.WithCancel(context.Background())
			ch := NewDataFetcher().Fetch(ctx, makeURLs(tc.urlCount))
			cancel() // cancel before any receive

			// Drain whatever trickles out; also checks the channel eventually closes.
			drain(t, ch, 3*time.Second)

			time.Sleep(tc.waitAfter)

			after := runtime.NumGoroutine()
			if after > baseline+tc.slack {
				t.Errorf("goroutine leak after immediate cancel: baseline=%d, after=%d (allowed slack=%d)", baseline, after, tc.slack)
			}
		})
	}
}

// ── Channel Close Discipline ──────────────────────────────────────────────────

// TestFetch_ChannelClosedExactlyOnce is self-correction test 3:
// firing cancel multiple times must not trigger "panic: close of closed channel".
func TestFetch_ChannelClosedExactlyOnce(t *testing.T) {
	tests := []struct {
		name        string
		urlCount    int
		cancelTimes int
	}{
		{
			name:        "cancel 3 times with 5 urls",
			urlCount:    5,
			cancelTimes: 3,
		},
		{
			name:        "cancel 10 times with 10 urls",
			urlCount:    10,
			cancelTimes: 10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic (likely double close of channel): %v", r)
				}
			}()

			ctx, cancel := context.WithCancel(context.Background())
			ch := NewDataFetcher().Fetch(ctx, makeURLs(tc.urlCount))

			for i := 0; i < tc.cancelTimes; i++ {
				cancel() // idempotent by context contract; only one logical cancel
			}

			// must drain without panic and the channel must close
			drain(t, ch, 3*time.Second)
		})
	}
}

// TestFetch_ChannelOwnership verifies that only the producer side closes the output
// channel — proven structurally by ranging until close (no external close is attempted).
func TestFetch_ChannelOwnership(t *testing.T) {
	tests := []struct {
		name     string
		urlCount int
		timeout  time.Duration
	}{
		{
			name:     "channel closes after all results delivered",
			urlCount: 3,
			timeout:  3 * time.Second,
		},
		{
			name:     "channel closes after context timeout",
			urlCount: 10,
			timeout:  1 * time.Second, // shorter than doWork so ctx expires first
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			ch := NewDataFetcher().Fetch(ctx, makeURLs(tc.urlCount))

			done := make(chan struct{})
			go func() {
				for range ch {
				}
				close(done)
			}()

			select {
			case <-done:
				// channel was closed — ownership properly held by producer
			case <-time.After(tc.timeout + 2*time.Second):
				t.Error("channel was never closed — producer must close the output channel")
			}
		})
	}
}
