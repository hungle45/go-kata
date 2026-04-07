package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

func main() {}

const maxBodySize int64 = 1024

type APIClient struct {
	client *http.Client
}

func NewAPIClient() *APIClient {
	return &APIClient{
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
	}
}

func (c *APIClient) GetJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Error("failed to create request", "error", err)
		return err
	}

	start := time.Now()
	slog.Info("send request", "method", req.Method, "url", req.URL.String())
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Error("failed to send request", "error", err)
		return err
	}
	defer resp.Body.Close()
	slog.Info("received response", "method", req.Method, "url", req.URL.String(), "status", resp.StatusCode, "duration", time.Since(start))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("non-2xx response", "status", resp.StatusCode)
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		if err != nil {
			slog.Error("failed to read error response body", "error", err)
			return err
		}
		slog.Debug("error response body", "body", string(body))

		_, err = io.Copy(io.Discard, resp.Body)
		if err != nil {
			slog.Error("failed to drain response body", "error", err)
			return err
		}
		return fmt.Errorf("request failed with status %d and body: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		slog.Error("failed to decode response body", "error", err)
		return err
	}

	return nil
}

func (c *APIClient) HTTPClient() *http.Client {
	return c.client
}
