// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/iencodev/live-subtitles/internal/config"
)

var testDist = fstest.MapFS{
	"index.html":         {Data: []byte("<!doctype html><title>app</title>")},
	"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
	"favicon.svg":        {Data: []byte("<svg/>")},
}

func newTestApp(dist fstest.MapFS) *App {
	return New(config.Config{Addr: "127.0.0.1:0"}, slog.New(slog.DiscardHandler), dist)
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name, method, path string
		dist               fstest.MapFS
		wantStatus         int
		wantBody           string
		wantCache          string
	}{
		{"health", "GET", "/healthz", testDist, 200, `"status":"ok"`, ""},
		{"unimplemented operation", "GET", "/api/sessions", testDist, 501, `"code":"not_implemented"`, ""},
		{"unknown api path", "GET", "/api/nope", testDist, 404, `"code":"route.not_found"`, ""},
		{"unknown ws path", "GET", "/ws/nope", testDist, 404, `"code":"route.not_found"`, ""},
		{"root", "GET", "/", testDist, 200, "<title>app</title>", "no-cache"},
		{"spa fallback", "GET", "/s/main-stage", testDist, 200, "<title>app</title>", "no-cache"},
		{"static file", "GET", "/favicon.svg", testDist, 200, "<svg/>", ""},
		{"hashed asset", "GET", "/assets/app-1a2b.js", testDist, 200, "console.log", "public, max-age=31536000, immutable"},
		{"post to spa", "POST", "/s/main-stage", testDist, 405, `"code":"method.not_allowed"`, ""},
		{"placeholder before web build", "GET", "/admin", fstest.MapFS{".gitkeep": {}}, 200, "isn't built yet", "no-cache"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestApp(tt.dist).Handler().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d", rec.Code, tt.wantStatus)
			}
			if body := rec.Body.String(); !strings.Contains(body, tt.wantBody) {
				t.Errorf("body %q does not contain %q", body, tt.wantBody)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control %q, want %q", got, tt.wantCache)
			}
		})
	}
}

func TestServeShutsDownOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- newTestApp(testDist).Serve(ctx, ln) }()

	res, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(ShutdownTimeout):
		t.Fatal("Serve did not return after cancel")
	}
}
