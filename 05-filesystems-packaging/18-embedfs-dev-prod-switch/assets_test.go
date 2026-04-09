package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// requireAssets calls Assets() and skips the test if either FS is nil
// (i.e. not yet implemented). Use this in tests that would panic on nil FS.
func requireAssets(t *testing.T) (templates fs.FS, static fs.FS) {
	t.Helper()
	tmpl, stat, err := Assets()
	if err != nil {
		t.Fatalf("Assets() error = %v", err)
	}
	if tmpl == nil || stat == nil {
		t.Skip("Assets() not yet implemented (returns nil)")
	}
	return tmpl, stat
}

// TestAssetsNotNil verifies that Assets() returns non-nil filesystems and no error.
func TestAssetsNotNil(t *testing.T) {
	templates, static, err := Assets()
	if err != nil {
		t.Fatalf("Assets() error = %v", err)
	}
	if templates == nil {
		t.Error("templates FS is nil")
	}
	if static == nil {
		t.Error("static FS is nil")
	}
}

// TestAssetsCleanRoots verifies that returned FS values use clean roots
// (i.e. files are reachable without a leading directory prefix).
func TestAssetsCleanRoots(t *testing.T) {
	tests := []struct {
		name     string
		getFS    func(tmpl, static fs.FS) fs.FS
		filePath string // path as seen by the caller (no top-level dir prefix)
	}{
		{
			name:     "template index.html reachable at clean root",
			getFS:    func(tmpl, _ fs.FS) fs.FS { return tmpl },
			filePath: "index.html",
		},
		{
			name:     "static app.css reachable at clean root",
			getFS:    func(_, static fs.FS) fs.FS { return static },
			filePath: "app.css",
		},
	}

	templates, static := requireAssets(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := tt.getFS(templates, static)
			f, err := fsys.Open(tt.filePath)
			if err != nil {
				t.Errorf("Open(%q) = %v; want nil error (check fs.Sub usage)", tt.filePath, err)
				return
			}
			f.Close()
		})
	}
}

// TestAssetsNoPrefixBug verifies the double-prefix path does NOT exist.
// e.g. "static/app.css" should NOT be reachable on the static FS.
func TestAssetsNoPrefixBug(t *testing.T) {
	tests := []struct {
		name     string
		getFS    func(tmpl, static fs.FS) fs.FS
		badPath  string // path that would exist if fs.Sub was omitted
	}{
		{
			name:    "static FS must not expose static/app.css",
			getFS:   func(_, static fs.FS) fs.FS { return static },
			badPath: "static/app.css",
		},
		{
			name:    "templates FS must not expose templates/index.html",
			getFS:   func(tmpl, _ fs.FS) fs.FS { return tmpl },
			badPath: "templates/index.html",
		},
	}

	templates, static := requireAssets(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := tt.getFS(templates, static)
			f, err := fsys.Open(tt.badPath)
			if err == nil {
				f.Close()
				t.Errorf("Open(%q) succeeded; expected error — missing fs.Sub?", tt.badPath)
			}
		})
	}
}

// TestHTTPRoutes verifies that the HTTP server responds correctly to
// the required routes, using the same handler wiring for both modes.
func TestHTTPRoutes(t *testing.T) {
	templates, static := requireAssets(t)

	srv := httptest.NewServer(newMux(templates, static))
	defer srv.Close()

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string // substring expected in response body
	}{
		{
			name:       "GET / renders index template",
			path:       "/",
			wantStatus: http.StatusOK,
			wantBody:   "Dashboard",
		},
		{
			name:       "GET /static/app.css serves CSS",
			path:       "/static/app.css",
			wantStatus: http.StatusOK,
			wantBody:   "font-family",
		},
		{
			name:       "GET /static/missing.css returns 404",
			path:       "/static/missing.css",
			wantStatus: http.StatusNotFound,
			wantBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d; want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantBody != "" {
				buf := make([]byte, 4096)
				n, _ := resp.Body.Read(buf)
				body := string(buf[:n])
				if !strings.Contains(body, tt.wantBody) {
					t.Errorf("body does not contain %q\ngot: %s", tt.wantBody, body)
				}
			}
		})
	}
}
