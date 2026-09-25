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

	"github.com/iencodev/live-subtitles/internal/api/handlers"
	"github.com/iencodev/live-subtitles/internal/config"
)

var testDist = fstest.MapFS{
	"index.html":         {Data: []byte("<!doctype html><title>app</title>")},
	"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
	"favicon.svg":        {Data: []byte("<svg/>")},
}

func newTestApp(t *testing.T, cfg config.Config, dist fstest.MapFS) *App {
	t.Helper()
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:0"
	}
	cfg.DataDir, cfg.NoKeychain = t.TempDir(), true
	a, err := New(t.Context(), cfg, slog.New(slog.DiscardHandler), dist, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	a.out = io.Discard
	return a
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name, method, path string
		dist               fstest.MapFS
		wantStatus         int
		wantBody           string
		wantCache          string
	}{
		// ok vs degraded depends on the host's ffmpeg; handlers/system_test.go covers it.
		{"health", "GET", "/healthz", testDist, 200, `"database":"ok"`, ""},
		{"network on loopback", "GET", "/api/network", testDist, 200, `"viewerBaseUrl":"http://localhost:0"`, ""},
		{"admin operation needs login", "GET", "/api/sessions", testDist, 401, `"code":"auth.required"`, ""},
		{"setup status", "GET", "/api/setup", testDist, 200, `"adminPinSet":false`, ""},
		{"overlay presets are public", "GET", "/api/overlay-presets", testDist, 200, `"id":"classic"`, ""},
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
			newTestApp(t, config.Config{}, tt.dist).Handler().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
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
	a := newTestApp(t, config.Config{}, testDist)
	go func() { done <- a.Serve(ctx, ln) }()

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

func TestBanner(t *testing.T) {
	a := newTestApp(t, config.Config{Addr: ":8080", PublicBaseURL: "https://subs.example.com"}, testDist)
	var out strings.Builder
	a.out = &out
	a.banner()
	for _, want := range []string{"Audience  https://subs.example.com/s", "Admin     https://subs.example.com/admin"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("banner %q does not contain %q", out.String(), want)
		}
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Error("banner printed a QR code to a non-terminal")
	}
}

func TestNetworkFollowsSettings(t *testing.T) {
	a := newTestApp(t, config.Config{PublicBaseURL: "https://flag.example.com"}, testDist)
	get := func() string {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/network", nil))
		return rec.Body.String()
	}
	if body := get(); !strings.Contains(body, `"viewerBaseUrl":"https://flag.example.com"`) {
		t.Errorf("before saving: %s", body)
	}
	st := handlers.DefaultSettings()
	public := "https://subs.example.com"
	st.Network.PublicBaseUrl = &public
	if err := a.store.PutSettings(t.Context(), st); err != nil {
		t.Fatal(err)
	}
	if body := get(); !strings.Contains(body, `"viewerBaseUrl":"https://subs.example.com"`) {
		t.Errorf("after saving: %s", body)
	}
}
