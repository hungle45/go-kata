package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

func main() {}

type DataFetcher struct{}

func NewDataFetcher() *DataFetcher {
	return &DataFetcher{}
}

func (df *DataFetcher) Fetch(ctx context.Context, urls []string) <-chan Result {
	// Unbuffered: backpressure is intentional — senders block until the consumer
	// reads, which gives the select-on-send its cancellation path. A buffer here
	// would delay goroutine exit after ctx cancel by up to `cap` results.
	results := make(chan Result)

	wg := sync.WaitGroup{}

	for _, url := range urls {
		wg.Add(1)
		go func(u string) {
			defer func() {
				if r := recover(); r != nil {
					select {
					case <-ctx.Done():
					case results <- Result{URL: u, Err: fmt.Errorf("panic: %v", r)}:
					}
				}
				wg.Done()
			}()

			result := df.doWork(ctx, u)

			select {
			case <-ctx.Done():
			case results <- result:
			}
		}(url)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

func (df *DataFetcher) doWork(ctx context.Context, url string) Result {
	duration := time.Duration(rand.Intn(200)) * time.Millisecond
	select {
	case <-ctx.Done():
		return Result{URL: url, Err: context.Canceled}
	case <-time.After(duration):
		return Result{URL: url, Body: []byte(fmt.Sprintf("Data from %s after %v", url, duration))}
	}
}

type Result struct {
	URL  string
	Body []byte
	Err  error
}

func (r Result) String() string {
	if r.Err != nil {
		return fmt.Sprintf("Error fetching %s: %v", r.URL, r.Err)
	}
	return fmt.Sprintf("Fetched from %s: %s", r.URL, string(r.Body))
}
