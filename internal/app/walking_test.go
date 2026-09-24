// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/config"
	"github.com/iencodev/live-subtitles/internal/domain"
)

const walkToken = "walking-skeleton-admin-token"

// The M1 walking skeleton over real HTTP and WebSockets: browser-style
// audio on /ws/ingest → mock provider → /ws/captions, driven by the
// session API, with the admin event stream watching.
func TestWalkingSkeleton(t *testing.T) {
	a := newTestApp(t, config.Config{AdminToken: walkToken}, testDist)
	now := time.Now()
	if err := a.store.CreateSession(t.Context(), domain.Session{Id: "main", Name: "Main", CreatedAt: now, UpdatedAt: now,
		Provider: api.ProviderChoiceDefault, SourceLanguage: api.En, TargetLanguages: []string{"es"}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	adminCall := func(method, path string, wantStatus int) []byte {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+walkToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != wantStatus {
			t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, body)
		}
		return body
	}

	// Admin events need credentials.
	if _, res, err := websocket.Dial(ctx, ws+"/ws/admin", nil); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("anonymous /ws/admin: %v", err)
	}
	admin, _, err := websocket.Dial(ctx, ws+"/ws/admin", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + walkToken}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.CloseNow() }()
	var ev api.AdminEvent
	readJSON(ctx, t, admin, &ev)
	if ev.Status == nil || ev.Status.SessionId != "main" || ev.Status.State != api.SessionStateIdle {
		t.Fatalf("admin snapshot %+v", ev)
	}

	// A viewer on the Spanish and source tracks.
	viewer, _, err := websocket.Dial(ctx, ws+"/ws/captions/main?lang=es&lang=source", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = viewer.CloseNow() }()
	viewer.SetReadLimit(1 << 20)
	var msg api.CaptionsServerMessage
	readJSON(ctx, t, viewer, &msg)
	if msg.Type != api.CaptionsServerMessageTypeHistory {
		t.Fatalf("first viewer message %+v", msg)
	}
	if _, res, err := websocket.Dial(ctx, ws+"/ws/captions/nope?lang=es", nil); err == nil || res.StatusCode != 404 {
		t.Errorf("viewer of an unknown session: %v", err)
	}

	// The capture page: token, hello, then PCM.
	var tok api.IngestToken
	if err := json.Unmarshal(adminCall("POST", "/api/sessions/main/ingest-token", 200), &tok); err != nil {
		t.Fatal(err)
	}
	if _, res, err := websocket.Dial(ctx, ws+"/ws/ingest/main?token=wrong", nil); err == nil || res.StatusCode != 401 {
		t.Errorf("ingest with a wrong token: %v", err)
	}
	capture, _, err := websocket.Dial(ctx, ws+"/ws/ingest/main?token="+tok.IngestToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = capture.CloseNow() }()
	hello, _ := json.Marshal(api.IngestHello{Type: "hello", Format: api.S16le, SampleRate: 16000, Channels: 1})
	if err := capture.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	var ready api.IngestServerMessage
	readJSON(ctx, t, capture, &ready)
	if ready.Type != api.IngestServerMessageTypeReady {
		t.Fatalf("ingest answered %+v", ready)
	}

	var st api.SessionStatus
	if err := json.Unmarshal(adminCall("POST", "/api/sessions/main/start", 200), &st); err != nil {
		t.Fatal(err)
	}
	if st.State != api.SessionStateLive {
		t.Fatalf("start: %+v", st)
	}
	adminCall("POST", "/api/sessions/main/start", 409)

	// 10 s of silence in 100 ms chunks, as fast as the socket takes it: the
	// mock ASR speaks one word per 300 ms of audio.
	go func() {
		chunk := make([]byte, 3200)
		for range 100 {
			if capture.Write(ctx, websocket.MessageBinary, chunk) != nil {
				return
			}
		}
	}()

	var es, source *api.Caption
	for es == nil || source == nil {
		readJSON(ctx, t, viewer, &msg)
		if msg.Type != api.CaptionsServerMessageTypeCaption || !msg.Caption.Final {
			continue
		}
		c := *msg.Caption
		switch c.Lang {
		case "es":
			es = &c
		case "source":
			source = &c
		}
	}
	if source.Text != "Welcome everyone, and thanks for joining this session." ||
		es.Text != "Bienvenidos a todos, y gracias por sumarse a esta sesión." || es.SegmentId != source.SegmentId {
		t.Errorf("captions: source %q, es %q", source.Text, es.Text)
	}

	adminCall("POST", "/api/sessions/main/pause", 200)
	if err := json.Unmarshal(adminCall("POST", "/api/sessions/main/stop", 200), &st); err != nil {
		t.Fatal(err)
	}
	if st.State != api.SessionStateIdle {
		t.Errorf("after stop: %+v", st)
	}
	body := adminCall("GET", "/api/sessions/main/status", 200)
	if !strings.Contains(string(body), `"state":"idle"`) {
		t.Errorf("status %s", body)
	}
	// The final captions were stored for exports.
	res, err := http.Get(srv.URL + "/api/public/sessions/main/subtitles?lang=es&format=vtt")
	if err != nil {
		t.Fatal(err)
	}
	vtt, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !strings.Contains(string(vtt), "Bienvenidos a todos") {
		t.Errorf("VTT export:\n%s", vtt)
	}
}

func readJSON(ctx context.Context, t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
}
