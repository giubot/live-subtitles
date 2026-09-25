// SPDX-License-Identifier: Apache-2.0

package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
)

func newServer(t testing.TB, b *Bus, opts ...HandlerOption) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /ws/captions/{sessionId}", NewCaptionsHandler(b, slog.New(slog.DiscardHandler), opts...))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func dial(ctx context.Context, srv *httptest.Server, path string) (*websocket.Conn, error) {
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(1 << 20)
	return c, nil
}

func read(ctx context.Context, c *websocket.Conn) (domain.BusMessage, error) {
	var m domain.BusMessage
	typ, b, err := c.Read(ctx)
	if err != nil {
		return m, err
	}
	if typ != websocket.MessageText {
		return m, fmt.Errorf("frame type %v, want text", typ)
	}
	return m, json.Unmarshal(b, &m)
}

// script turns the mock provider's script into realistic caption traffic:
// one interim per word, then a final, per line, on the source and es tracks.
func script() []domain.BusMessage {
	var msgs []domain.BusMessage
	for i, line := range mock.DefaultScript {
		seg := fmt.Sprintf("s-%06d", i)
		start := float32(i * 4)
		for track, text := range map[string]string{domain.SourceTrack: line.EN, "es": line.ES} {
			words := strings.Fields(text)
			for w := 1; w < len(words); w++ {
				msgs = append(msgs, capt(track, seg, strings.Join(words[:w], " "), false, start))
			}
			msgs = append(msgs, capt(track, seg, text, true, start))
		}
	}
	return msgs
}

func TestCaptionsHandlerErrors(t *testing.T) {
	lookup := func(_ context.Context, id string) error {
		switch id {
		case "main":
			return nil
		case "broken":
			return errors.New("db down")
		}
		return fmt.Errorf("session %s: %w", id, domain.ErrNotFound)
	}
	srv := newServer(t, New(), WithSessionLookup(lookup))
	tests := []struct {
		path   string
		status int
		code   string
	}{
		{"/ws/captions/main", http.StatusBadRequest, "request.invalid"},
		{"/ws/captions/main?lang=", http.StatusBadRequest, "request.invalid"},
		{"/ws/captions/main?lang=es&history=x", http.StatusBadRequest, "request.invalid"},
		{"/ws/captions/main?lang=es&history=501", http.StatusBadRequest, "request.invalid"},
		{"/ws/captions/main?lang=es&history=-1", http.StatusBadRequest, "request.invalid"},
		{"/ws/captions/nope?lang=es", http.StatusNotFound, "session.not_found"},
		{"/ws/captions/broken?lang=es", http.StatusInternalServerError, "internal"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			res, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			var e api.Error
			if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tt.status || e.Code != tt.code {
				t.Errorf("got %d %q, want %d %q", res.StatusCode, e.Code, tt.status, tt.code)
			}
		})
	}
}

func TestCaptionsHandlerStream(t *testing.T) {
	b := New(WithViewersInterval(time.Millisecond))
	b.Publish("main", capt("es", "s1", "Uno.", true, 1))
	b.Publish("main", capt("es", "s2", "Dos.", true, 2))
	b.Publish("main", capt("es", "s3", "Tr", false, 3))
	b.Publish("main", capt("en", "s1", "One.", true, 1))
	srv := newServer(t, b, WithPingInterval(10*time.Millisecond))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := dial(ctx, srv, "/ws/captions/main?lang=es&history=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()

	m, err := read(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != api.CaptionsServerMessageTypeHistory || m.Captions == nil {
		t.Fatalf("first frame = %+v, want history", m)
	}
	if got := strings.Join(summary(*m.Captions), ","); got != "s2:Dos.,s3:Tr~" {
		t.Errorf("history = %s", got)
	}

	time.Sleep(50 * time.Millisecond) // a few pings go by
	b.Publish("main", capt("en", "s3", "Three.", true, 3))
	b.Publish("main", capt("es", "s3", "Tres.", true, 3))
	for {
		m, err := read(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		if m.Type == api.CaptionsServerMessageTypeViewers {
			if *m.Viewers != 1 {
				t.Errorf("viewers = %d, want 1", *m.Viewers)
			}
			continue
		}
		if m.Caption == nil || m.Caption.Text != "Tres." || !m.Caption.Final {
			t.Fatalf("got %+v, want final Tres.", m)
		}
		break
	}

	// Closing the session drops the subscriber: 1013 so the client retries.
	b.CloseSession("main")
	for {
		_, err = read(ctx, c)
		if err != nil {
			break
		}
	}
	if s := websocket.CloseStatus(err); s != websocket.StatusTryAgainLater {
		t.Errorf("close status = %v (%v), want 1013", s, err)
	}
}

func TestCaptionsHandlerClientGone(t *testing.T) {
	b := New()
	srv := newServer(t, b)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := dial(ctx, srv, "/ws/captions/main?lang=es")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := read(ctx, c); err != nil {
		t.Fatal(err)
	}
	if v := b.Viewers("main"); v != 1 {
		t.Fatalf("Viewers = %d, want 1", v)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
	waitFor(t, func() bool { return b.Viewers("main") == 0 })
}

func waitFor(t testing.TB, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCaptionsLoad connects 500 WebSocket viewers and checks that every one
// receives the whole burst of captions of its track, in order.
func TestCaptionsLoad(t *testing.T) {
	const clients = 500
	b := New() // default buffer (256) holds the whole burst
	srv := newServer(t, b)
	msgs := script()
	want := map[string][]string{}
	for _, m := range msgs {
		c := m.Caption
		want[c.Lang] = append(want[c.Lang], c.SegmentId+":"+c.Text)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	ready := make(chan struct{}, clients)
	// Dial in batches: 500 connects at once overflow the listen backlog
	// (128 on macOS) and the kernel resets some of them.
	dialing := make(chan struct{}, 64)
	for i := range clients {
		track := []string{domain.SourceTrack, "es"}[i%2]
		wg.Go(func() {
			dialing <- struct{}{}
			c, err := dial(ctx, srv, "/ws/captions/main?lang="+track)
			<-dialing
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = c.CloseNow() }()
			if m, err := read(ctx, c); err != nil || m.Type != api.CaptionsServerMessageTypeHistory {
				errs <- fmt.Errorf("history: %+v %v", m, err)
				return
			}
			ready <- struct{}{}
			var got []string
			for len(got) < len(want[track]) {
				m, err := read(ctx, c)
				if err != nil {
					errs <- fmt.Errorf("client %d after %d captions: %w", i, len(got), err)
					return
				}
				if m.Caption != nil {
					got = append(got, m.Caption.SegmentId+":"+m.Caption.Text)
				}
			}
			if strings.Join(got, "|") != strings.Join(want[track], "|") {
				errs <- fmt.Errorf("client %d (%s) got a different caption sequence", i, track)
				return
			}
			_ = c.Close(websocket.StatusNormalClosure, "")
		})
	}
	for range clients {
		select {
		case <-ready:
		case err := <-errs:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("clients didn't connect")
		}
	}
	if v := b.Viewers("main"); v != clients {
		t.Fatalf("Viewers = %d, want %d", v, clients)
	}

	start := time.Now()
	for _, m := range msgs {
		b.Publish("main", m)
	}
	publish := time.Since(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	t.Logf("%d clients × %d messages: publish %v, delivered in %v", clients, len(msgs), publish, time.Since(start))
	waitFor(t, func() bool { return b.Viewers("main") == 0 })
}
