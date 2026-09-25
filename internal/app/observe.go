// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/iencodev/live-subtitles/internal/api/handlers"
	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/metrics"
)

// MetricsPath serves Prometheus metrics when --metrics is on.
const MetricsPath = "/metrics"

// observe logs every request once it finishes and, when m isn't nil,
// counts it in the metrics (P3-13). The log line has the method, path
// (never the query, which can carry an ingest token), route pattern,
// status, size, duration, client address and, on session routes, the
// session ID. Probes, metrics scrapes and the web app's static files log
// at debug so they don't drown the API calls.
func observe(next http.Handler, log *slog.Logger, m *metrics.App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, endpoint: wsEndpoint(r.URL.Path), m: m}
		defer rec.closeWS()
		next.ServeHTTP(rec, r) // the mux fills r.Pattern and the path values in place

		d := time.Since(start)
		status := rec.status
		if status == 0 {
			status = http.StatusOK // nothing written
		}
		route := routeOf(r.Pattern)
		if m != nil {
			m.Request(route, r.Method, status, d, rec.upgraded)
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int64("bytes", rec.bytes),
			slog.Float64("duration_ms", float64(d.Microseconds())/1000),
			slog.String("remote", remoteHost(r.RemoteAddr)),
		}
		if id := r.PathValue("sessionId"); id != "" {
			attrs = append(attrs, slog.String("session", id))
		}
		if rec.upgraded {
			attrs = append(attrs, slog.Bool("websocket", true))
		}
		log.LogAttrs(r.Context(), requestLevel(r, status), "http request", attrs...)
	})
}

// requestLevel is warn for server errors, debug for noisy paths and info
// for the rest.
func requestLevel(r *http.Request, status int) slog.Level {
	p := r.URL.Path
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelWarn
	case p == "/healthz" || p == MetricsPath:
		return slog.LevelDebug
	case strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/"):
		return slog.LevelInfo
	default: // the web app and its assets
		return slog.LevelDebug
	}
}

// routeOf is the path part of a mux pattern ("GET /api/sessions/{sessionId}"
// → "/api/sessions/{sessionId}"), which keeps metric labels bounded.
func routeOf(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "unmatched"
	}
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return strings.TrimSpace(path)
	}
	return pattern
}

// wsEndpoint is "captions" for /ws/captions/…, and so on; "" off /ws/.
func wsEndpoint(path string) string {
	rest, ok := strings.CutPrefix(path, "/ws/")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	for _, e := range metrics.WSEndpoints {
		if name == e {
			return e
		}
	}
	return ""
}

func remoteHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// recorder captures the status and size of a response. A 101 on a /ws/
// path counts an open WebSocket until the handler returns.
type recorder struct {
	http.ResponseWriter
	status   int
	bytes    int64
	endpoint string
	m        *metrics.App
	upgraded bool
	closed   func()
}

func (rec *recorder) WriteHeader(code int) {
	if rec.status == 0 {
		rec.status = code
		if code == http.StatusSwitchingProtocols && rec.endpoint != "" {
			rec.upgraded = true
			if rec.m != nil {
				rec.closed = rec.m.WSOpened(rec.endpoint)
			}
		}
	}
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *recorder) Write(p []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	n, err := rec.ResponseWriter.Write(p)
	rec.bytes += int64(n)
	return n, err
}

func (rec *recorder) closeWS() {
	if rec.closed != nil {
		rec.closed()
	}
}

// Unwrap lets http.ResponseController reach Flush, deadlines and the rest.
func (rec *recorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

// Flush keeps streaming responses streaming.
func (rec *recorder) Flush() {
	_ = http.NewResponseController(rec.ResponseWriter).Flush()
}

// Hijack is what the WebSocket upgrade takes over.
func (rec *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(rec.ResponseWriter).Hijack()
}

// metricsHandler serves /metrics to admins (the ls_admin cookie or the
// LIVESUBS_ADMIN_TOKEN bearer token, like the admin API) and answers 404
// when m is nil (--metrics off).
func metricsHandler(m *metrics.App, a *auth.Service, log *slog.Logger) http.Handler {
	if m == nil {
		return http.NotFoundHandler()
	}
	serve := m.Handler(func(r *http.Request, err error) {
		if !errors.Is(err, context.Canceled) {
			log.WarnContext(r.Context(), "metrics: a source failed", "err", err)
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, err := a.Authenticated(r.Context(), r)
		if err != nil {
			log.ErrorContext(r.Context(), "authenticate request", "path", r.URL.Path, "err", err)
			handlers.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		if !admin {
			handlers.WriteError(w, http.StatusUnauthorized, "auth.required", "admin login required")
			return
		}
		serve.ServeHTTP(w, r)
	})
}
