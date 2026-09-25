// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
)

func TestApp(t *testing.T) {
	free := int64(1 << 30)
	sessions := []api.SessionStatus{
		{SessionId: "main", State: api.SessionStateLive, Viewers: 12},
		{SessionId: "room-b", State: api.SessionStatePaused},
		{SessionId: "old", State: api.SessionStateIdle},
		{SessionId: "watched", State: api.SessionStateIdle, Viewers: 3},
	}
	for _, c := range []struct {
		name    string
		src     Sources
		do      func(a *App)
		want    []string
		notWant []string
		wantErr bool
	}{
		{
			name: "sessions and viewers",
			src:  Sources{Sessions: func(context.Context) ([]api.SessionStatus, error) { return sessions, nil }},
			want: []string{
				`livesubs_sessions{state="live"} 1`,
				`livesubs_sessions{state="paused"} 1`,
				`livesubs_sessions{state="idle"} 2`,
				`livesubs_sessions{state="error"} 0`,
				`livesubs_session_viewers{session="main"} 12`,
				`livesubs_session_viewers{session="room-b"} 0`,
				`livesubs_session_viewers{session="watched"} 3`,
			},
			notWant: []string{`session="old"`},
		},
		{
			name: "recordings",
			src: Sources{Recordings: func(context.Context) (api.StorageUsage, error) {
				return api.StorageUsage{Recordings: 2, UsedBytes: 5000, FreeBytes: &free}, nil
			}},
			want: []string{"livesubs_recordings_bytes 5000", "livesubs_recordings 2", "livesubs_recordings_free_bytes 1.073741824e+09"},
		},
		{
			name: "no sources",
			want: []string{"# TYPE livesubs_sessions gauge", `livesubs_ws_clients{endpoint="captions"} 0`},
			notWant: []string{
				"\nlivesubs_sessions{", "\nlivesubs_recordings_bytes ",
			},
		},
		{
			name:    "failing source",
			src:     Sources{Sessions: func(context.Context) ([]api.SessionStatus, error) { return nil, errors.New("db") }},
			want:    []string{"# TYPE livesubs_caption_latency_seconds histogram"},
			wantErr: true,
		},
		{
			name: "latency, errors, requests and websockets",
			do: func(a *App) {
				a.CaptionLatency(api.ProviderKindGemini, "source", 800)
				a.CaptionLatency(api.ProviderKindGemini, "source", 2500)
				a.SessionError(api.ProviderKindLocal, "provider.error")
				a.Request("/api/sessions", "GET", 200, 30*time.Millisecond, false)
				a.Request("/ws/captions/{sessionId}", "GET", 101, time.Hour, true)
				done := a.WSOpened("captions")
				a.WSOpened("captions")
				done()
				done() // idempotent
			},
			want: []string{
				`livesubs_caption_latency_seconds_bucket{provider="gemini",track="source",le="1"} 1`,
				`livesubs_caption_latency_seconds_bucket{provider="gemini",track="source",le="3"} 2`,
				`livesubs_caption_latency_seconds_count{provider="gemini",track="source"} 2`,
				`livesubs_session_errors_total{provider="local",code="provider.error"} 1`,
				`livesubs_http_requests_total{route="/api/sessions",method="GET",code="200"} 1`,
				`livesubs_http_requests_total{route="/ws/captions/{sessionId}",method="GET",code="101"} 1`,
				`livesubs_http_request_duration_seconds_count{route="/api/sessions"} 1`,
				`livesubs_ws_clients{endpoint="captions"} 1`,
			},
			notWant: []string{`livesubs_http_request_duration_seconds_count{route="/ws/captions/{sessionId}"}`},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := NewApp(c.src)
			if c.do != nil {
				c.do(a)
			}
			var b strings.Builder
			err := a.Registry().Write(context.Background(), &b)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			out := b.String()
			for _, w := range c.want {
				if !strings.Contains(out, w+"\n") {
					t.Errorf("missing %q in\n%s", w, out)
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(out, w) {
					t.Errorf("unexpected %q in\n%s", w, out)
				}
			}
		})
	}
}
