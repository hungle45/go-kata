package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

type FanOutClient struct {
	url    string
	client *http.Client

	logger    *slog.Logger
	semaphore *semaphore.Weighted
	limiter   *rate.Limiter
}

func NewFanOutClient(url string) *FanOutClient {
	return &FanOutClient{
		url: url,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				MaxIdleConnsPerHost:   10,
			},
		},
		logger:    slog.New(slog.NewTextHandler(os.Stdout, nil)),
		semaphore: semaphore.NewWeighted(8),
		limiter:   rate.NewLimiter(rate.Every(time.Second/10), 20),
	}
}

func (c *FanOutClient) FetchAll(ctx context.Context, userIDs []int) (map[int][]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultChan := make(chan Result, len(userIDs))
	var wg sync.WaitGroup

	go func() {
		defer close(resultChan)
		defer wg.Wait()

		for _, id := range userIDs {
			if err := c.limiter.Wait(ctx); err != nil {
				resultChan <- Result{UserID: id, Err: err}
				return
			}

			if err := c.semaphore.Acquire(ctx, 1); err != nil {
				resultChan <- Result{UserID: id, Err: err}
				return
			}

			wg.Add(1)
			go func(uid int) {
				defer wg.Done()
				defer c.semaphore.Release(1)

				data, err := c.Fetch(ctx, uid)
				resultChan <- Result{UserID: uid, Data: data, Err: err}
			}(id)
		}
	}()

	results := make(map[int][]byte, len(userIDs))
	for res := range resultChan {
		if res.Err != nil {
			return nil, res.Err
		}
		results[res.UserID] = res.Data
	}

	return results, nil
}

func (c *FanOutClient) Fetch(ctx context.Context, userID int) (data []byte, err error) {
	start := time.Now()

	defer func() {
		args := []any{
			slog.Int("userID", userID),
			slog.Int("attempt", 1),
			slog.Duration("latency", time.Since(start)),
		}
		if err != nil {
			c.logger.Error("fetch failed", append(args, slog.Any("err", err))...)
		} else {
			c.logger.Info("fetch success", args...)
		}
	}()

	url := fmt.Sprintf("%s?userid=%d", c.url, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for user %d: %w", userID, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: status %d", resp.StatusCode)
	}

	data, err = io.ReadAll(resp.Body)
	return data, err
}

type Result struct {
	UserID int
	Data   []byte
	Err    error
}
