package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"sync"
)

func main() {
	http.Handle("/ping", AuditBody(64, http.HandlerFunc(PingHandler)))
	if err := http.ListenAndServe(":8080", nil); err != nil {
		slog.Error("failed to start server", "error", err)
	}
}

func AuditBody(max int, next http.Handler) http.Handler {
	pool := sync.Pool{
		New: func() any {
			buf := new(bytes.Buffer)
			buf.Grow(max)
			return buf
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("<-- audit body middleware called")

		buf := pool.Get().(*bytes.Buffer)
		defer func() {
			buf.Reset()
			if buf.Cap() <= max {
				pool.Put(buf)
			}
			slog.Info("--> audit body middleware completed")
		}()

		if _, err := io.Copy(buf, io.LimitReader(r.Body, int64(max))); err != nil {
			slog.Error("failed to read request body", "error", err)
		}
		slog.Info("request", "length", buf.Len(), "body", buf.String())

		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf.Bytes()), r.Body))
		next.ServeHTTP(w, r)
	})
}

func PingHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read request body", "error", err)
		return
	}
	slog.Info("ping handler received body", "length", len(body), "body", string(body))

	w.WriteHeader(http.StatusOK)
	if _, err = w.Write([]byte("pong")); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}
