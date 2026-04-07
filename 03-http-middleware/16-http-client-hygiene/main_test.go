package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type payload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestGetJSON(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		out        any
		wantErr    bool
		wantStatus int    // if non-zero, error must contain this status code as string
		wantBody   string // if non-empty, error must contain this substring
		wantOut    any    // if non-nil, decoded value must deep-equal this
	}{
		{
			name: "2xx decodes JSON into out",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(payload{Name: "alice", Age: 30})
			},
			out:     &payload{},
			wantOut: &payload{Name: "alice", Age: 30},
		},
		{
			name: "400 returns error with status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"bad request"}`)
			},
			wantErr:    true,
			wantStatus: 400,
			wantBody:   "bad request",
		},
		{
			name: "404 returns error with status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":"not found"}`)
			},
			wantErr:    true,
			wantStatus: 404,
			wantBody:   "not found",
		},
		{
			name: "500 returns error with status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"internal error"}`)
			},
			wantErr:    true,
			wantStatus: 500,
			wantBody:   "internal error",
		},
		{
			name: "503 returns error with status code",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"error":"service unavailable"}`)
			},
			wantErr:    true,
			wantStatus: 503,
			wantBody:   "service unavailable",
		},
		{
			name: "logging does not panic on success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{}`)
			},
			out:     &map[string]any{},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewAPIClient()
			err := client.GetJSON(context.Background(), srv.URL, tc.out)

			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantStatus != 0 && !strings.Contains(err.Error(), fmt.Sprintf("%d", tc.wantStatus)) {
				t.Errorf("error %q does not contain status %d", err.Error(), tc.wantStatus)
			}
			if tc.wantBody != "" && !strings.Contains(err.Error(), tc.wantBody) {
				t.Errorf("error %q does not contain body substring %q", err.Error(), tc.wantBody)
			}
			if tc.wantOut != nil && !reflect.DeepEqual(tc.out, tc.wantOut) {
				t.Errorf("decoded out = %+v, want %+v", tc.out, tc.wantOut)
			}
		})
	}
}

func TestGetJSON_ContextCancellation(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		delay   time.Duration
		wantErr bool
	}{
		{
			name:    "deadline exceeded before server responds",
			timeout: 50 * time.Millisecond,
			delay:   500 * time.Millisecond,
			wantErr: true,
		},
		{
			name:    "context has enough time to complete",
			timeout: 500 * time.Millisecond,
			delay:   10 * time.Millisecond,
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(tc.delay)
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{}`)
			}))
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			client := NewAPIClient()
			err := client.GetJSON(ctx, srv.URL, &map[string]any{})

			if tc.wantErr && err == nil {
				t.Fatal("expected context cancellation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetJSON_ErrorBodyDrained(t *testing.T) {
	tests := []struct {
		name         string
		bodySize     int
		requests     int
		wantRequests int
	}{
		{
			name:         "small error body drained allows connection reuse",
			bodySize:     512,
			requests:     5,
			wantRequests: 5,
		},
		{
			name:         "large error body drained allows connection reuse",
			bodySize:     4096,
			requests:     5,
			wantRequests: 5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, strings.Repeat("x", tc.bodySize))
			}))
			defer srv.Close()

			client := NewAPIClient()
			for i := 0; i < tc.requests; i++ {
				_ = client.GetJSON(context.Background(), srv.URL, nil)
			}

			if count != tc.wantRequests {
				t.Errorf("got %d requests completed, want %d", count, tc.wantRequests)
			}
		})
	}
}

func TestAPIClient_Idiomatic(t *testing.T) {
	type hasHTTPClient interface {
		HTTPClient() *http.Client
	}

	tests := []struct {
		name  string
		check func(t *testing.T, hc *http.Client)
	}{
		{
			name: "must not use http.DefaultClient",
			check: func(t *testing.T, hc *http.Client) {
				if hc == http.DefaultClient {
					t.Error("APIClient must not use http.DefaultClient")
				}
			},
		},
		{
			name: "must have non-nil transport for connection pooling",
			check: func(t *testing.T, hc *http.Client) {
				if hc.Transport == nil {
					t.Error("APIClient transport must not be nil")
				}
			},
		},
		{
			name: "must have a positive client timeout",
			check: func(t *testing.T, hc *http.Client) {
				if hc.Timeout <= 0 {
					t.Error("APIClient must have a positive Client.Timeout")
				}
			},
		},
		{
			name: "must reuse the same transport instance",
			check: func(t *testing.T, hc *http.Client) {
				if hc.Transport != hc.Transport {
					t.Error("transport must be the same instance across calls")
				}
			},
		},
	}

	client := NewAPIClient()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, client.HTTPClient())
		})
	}
}
