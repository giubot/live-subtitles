// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Buckets of the caption latency histogram, in seconds. The target is
// under 2 s for the source track and 3 s for translations (5.1).
var LatencyBuckets = []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4, 6, 10}

// Buckets of the HTTP request duration histogram, in seconds.
var RequestBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// WebSocket endpoints counted by livesubs_ws_clients.
var WSEndpoints = []string{"admin", "captions", "ingest"}

// Sources are read-only views of the services, read at scrape time. Each
// is optional.
type Sources struct {
	// Sessions is the status of every session (session.Manager.Snapshot).
	Sessions func(ctx context.Context) ([]api.SessionStatus, error)
	// Recordings is the recordings' disk usage (recording.Recorder.Usage).
	Recordings func(ctx context.Context) (api.StorageUsage, error)
}

// App is the server's metric set, served at /metrics (P3-13, ADM-5). The
// session manager reports latencies and errors to it as they happen
// (session.Observer), the HTTP middleware reports requests and WebSocket
// clients, and the gauges are read from Sources at scrape time.
type App struct {
	reg      *Registry
	requests *Counter
	duration *Histogram
	latency  *Histogram
	errors   *Counter

	mu sync.Mutex
	ws map[string]int
}

// NewApp registers the metrics.
func NewApp(src Sources) *App {
	reg := NewRegistry()
	a := &App{reg: reg, ws: map[string]int{}}
	reg.GaugeFunc("livesubs_sessions", "Sessions by runtime state.", []string{"state"},
		func(ctx context.Context, emit func(float64, ...string)) error {
			if src.Sessions == nil {
				return nil
			}
			list, err := src.Sessions(ctx)
			if err != nil {
				return err
			}
			count := map[api.SessionState]int{}
			for _, st := range list {
				count[st.State]++
			}
			for _, state := range []api.SessionState{api.SessionStateIdle, api.SessionStateStarting, api.SessionStateLive,
				api.SessionStatePaused, api.SessionStateStopping, api.SessionStateError} {
				emit(float64(count[state]), string(state))
			}
			return nil
		})
	reg.GaugeFunc("livesubs_session_viewers", "Caption viewers (WebSocket subscribers) per session, for sessions that are running or watched.",
		[]string{"session"}, func(ctx context.Context, emit func(float64, ...string)) error {
			if src.Sessions == nil {
				return nil
			}
			list, err := src.Sessions(ctx)
			if err != nil {
				return err
			}
			for _, st := range list {
				if st.Viewers > 0 || st.State != api.SessionStateIdle {
					emit(float64(st.Viewers), st.SessionId)
				}
			}
			return nil
		})
	reg.GaugeFunc("livesubs_ws_clients", "Open WebSocket connections by endpoint.", []string{"endpoint"},
		func(_ context.Context, emit func(float64, ...string)) error {
			a.mu.Lock()
			defer a.mu.Unlock()
			for _, e := range WSEndpoints {
				emit(float64(a.ws[e]), e)
			}
			return nil
		})
	a.latency = reg.Histogram("livesubs_caption_latency_seconds",
		"Time from the end of a final caption's audio reaching the server to the caption being emitted, per provider and track.",
		LatencyBuckets, "provider", "track")
	a.errors = reg.Counter("livesubs_session_errors_total",
		"Errors reported by running sessions, by provider and error code (provider.*, translation.failed, source.*, or a provider's own code).",
		"provider", "code")
	reg.GaugeFunc("livesubs_recordings_bytes", "Disk space taken by recordings.", nil,
		func(ctx context.Context, emit func(float64, ...string)) error {
			return a.storage(ctx, src, func(u api.StorageUsage) { emit(float64(u.UsedBytes)) })
		})
	reg.GaugeFunc("livesubs_recordings", "Stored recordings.", nil,
		func(ctx context.Context, emit func(float64, ...string)) error {
			return a.storage(ctx, src, func(u api.StorageUsage) { emit(float64(u.Recordings)) })
		})
	reg.GaugeFunc("livesubs_recordings_free_bytes", "Free space on the recordings' disk.", nil,
		func(ctx context.Context, emit func(float64, ...string)) error {
			return a.storage(ctx, src, func(u api.StorageUsage) {
				if u.FreeBytes != nil {
					emit(float64(*u.FreeBytes))
				}
			})
		})
	a.requests = reg.Counter("livesubs_http_requests_total", "HTTP requests by route pattern, method and status code.", "route", "method", "code")
	a.duration = reg.Histogram("livesubs_http_request_duration_seconds", "HTTP request duration by route pattern (WebSockets excluded).",
		RequestBuckets, "route")
	return a
}

func (a *App) storage(ctx context.Context, src Sources, emit func(api.StorageUsage)) error {
	if src.Recordings == nil {
		return nil
	}
	u, err := src.Recordings(ctx)
	if err != nil {
		return err
	}
	emit(u)
	return nil
}

// Registry is the underlying registry.
func (a *App) Registry() *Registry { return a.reg }

// Handler serves /metrics.
func (a *App) Handler(onError func(*http.Request, error)) http.Handler { return a.reg.Handler(onError) }

// CaptionLatency records the latency of a final caption (session.Observer).
func (a *App) CaptionLatency(provider domain.ProviderKind, track string, ms int) {
	a.latency.Observe(float64(ms)/1000, string(provider), track)
}

// SessionError counts an error of a running session (session.Observer).
func (a *App) SessionError(provider domain.ProviderKind, code string) {
	a.errors.Inc(string(provider), code)
}

// Request records a finished HTTP request. WebSocket connections are
// counted but not timed: they last as long as the client stays.
func (a *App) Request(route, method string, code int, d time.Duration, websocket bool) {
	a.requests.Inc(route, method, strconv.Itoa(code))
	if !websocket {
		a.duration.Observe(d.Seconds(), route)
	}
}

// WSOpened counts an open WebSocket on endpoint; call the returned
// function when it closes.
func (a *App) WSOpened(endpoint string) (closed func()) {
	a.mu.Lock()
	a.ws[endpoint]++
	a.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.ws[endpoint]--
			a.mu.Unlock()
		})
	}
}
