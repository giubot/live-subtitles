// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/config"
	"github.com/iencodev/live-subtitles/internal/metrics"
)

func TestObserveLog(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{sessionId}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("hello")) })
	mux.HandleFunc("GET /api/boom", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) })

	for _, c := range []struct {
		name  string
		path  string
		level string
		want  map[string]any
	}{
		{"api call with session", "/api/sessions/main?token=secret", "INFO",
			map[string]any{"route": "/api/sessions/{sessionId}", "session": "main", "status": 200.0, "bytes": 5.0, "path": "/api/sessions/main"}},
		{"server error", "/api/boom", "WARN", map[string]any{"route": "/api/boom", "status": 500.0}},
		{"health probe", "/healthz", "DEBUG", map[string]any{"route": "/healthz", "status": 200.0}},
		{"static asset", "/assets/app.js", "DEBUG", map[string]any{"route": "/", "status": 200.0}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			m := metrics.NewApp(metrics.Sources{})
			observe(mux, log, m).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, c.path, nil))

			var line map[string]any
			if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
				t.Fatalf("log %q: %v", buf.String(), err)
			}
			if line["level"] != c.level || line["msg"] != "http request" {
				t.Errorf("level %v msg %v, want %s", line["level"], line["msg"], c.level)
			}
			for k, v := range c.want {
				if line[k] != v {
					t.Errorf("%s = %v, want %v", k, line[k], v)
				}
			}
			if strings.Contains(buf.String(), "secret") {
				t.Errorf("the query reached the log: %s", buf.String())
			}
			if _, ok := line["session"]; ok && c.want["session"] == nil {
				t.Errorf("unexpected session in %s", buf.String())
			}
			var out strings.Builder
			_ = m.Registry().Write(context.Background(), &out)
			wantSample := fmt.Sprintf(`livesubs_http_requests_total{route=%q,method="GET",code="%v"} 1`, c.want["route"], c.want["status"])
			if !strings.Contains(out.String(), wantSample) {
				t.Errorf("missing %s in\n%s", wantSample, out.String())
			}
		})
	}
}

func TestRouteOf(t *testing.T) {
	for _, c := range []struct{ pattern, want string }{
		{"", "unmatched"},
		{"/", "/"},
		{"GET /api/sessions/{sessionId}", "/api/sessions/{sessionId}"},
		{"GET  /metrics", "/metrics"},
	} {
		if got := routeOf(c.pattern); got != c.want {
			t.Errorf("routeOf(%q) = %q, want %q", c.pattern, got, c.want)
		}
	}
}

func TestWSEndpoint(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"/ws/captions/main", "captions"},
		{"/ws/ingest/main", "ingest"},
		{"/ws/admin", "admin"},
		{"/ws/other", ""},
		{"/api/ws/admin", ""},
	} {
		if got := wsEndpoint(c.path); got != c.want {
			t.Errorf("wsEndpoint(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestObserveWebSocket checks that the wrapped writer still upgrades and
// that the connection counts as a WebSocket client while it is open.
func TestObserveWebSocket(t *testing.T) {
	m := metrics.NewApp(metrics.Sources{})
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/captions/{sessionId}", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = c.CloseNow() }()
		<-release
	})
	srv := httptest.NewServer(observe(mux, slog.New(slog.DiscardHandler), m))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/captions/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	waitFor(t, m, `livesubs_ws_clients{endpoint="captions"} 1`)
	close(release)
	waitFor(t, m, `livesubs_ws_clients{endpoint="captions"} 0`)
	waitFor(t, m, `livesubs_http_requests_total{route="/ws/captions/{sessionId}",method="GET",code="101"} 1`)
}

func waitFor(t *testing.T, m *metrics.App, sample string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var b strings.Builder
		_ = m.Registry().Write(context.Background(), &b)
		if strings.Contains(b.String(), sample+"\n") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing %s in\n%s", sample, b.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	const token = "0123456789abcdef-metrics"
	for _, c := range []struct {
		name       string
		metrics    bool
		bearer     string
		wantStatus int
		wantBody   string
	}{
		{"off", false, token, http.StatusNotFound, "404 page not found"},
		{"on without credentials", true, "", http.StatusUnauthorized, `"code":"auth.required"`},
		{"on with a wrong token", true, "wrong-token-wrong-token", http.StatusUnauthorized, `"code":"auth.required"`},
		{"on with the admin token", true, token, http.StatusOK, `livesubs_sessions{state="idle"} 0`},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t, config.Config{Metrics: c.metrics, AdminToken: token}, testDist)
			req := httptest.NewRequest(http.MethodGet, MetricsPath, nil)
			if c.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+c.bearer)
			}
			rec := httptest.NewRecorder()
			a.Handler().ServeHTTP(rec, req)
			if rec.Code != c.wantStatus || !strings.Contains(rec.Body.String(), c.wantBody) {
				t.Errorf("status %d body %q, want %d with %q", rec.Code, rec.Body.String(), c.wantStatus, c.wantBody)
			}
			if c.wantStatus == http.StatusOK && rec.Header().Get("Content-Type") != metrics.ContentType {
				t.Errorf("content type %q", rec.Header().Get("Content-Type"))
			}
		})
	}
}
