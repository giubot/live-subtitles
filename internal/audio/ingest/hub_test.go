// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func verify(_ context.Context, sessionID, token string) error {
	if sessionID == "missing" {
		return domain.ErrNotFound
	}
	if token != "good" {
		return errors.New("bad token")
	}
	return nil
}

type harness struct {
	hub   *Hub
	srv   *httptest.Server
	clock *fakeClock
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)}
	opts.Clock = clock
	opts.Logger = slog.New(slog.DiscardHandler)
	if opts.LevelInterval == 0 {
		opts.LevelInterval = time.Hour // keep level messages out of the way
	}
	hub := NewHub(verify, opts)
	mux := http.NewServeMux()
	mux.Handle("GET /ws/ingest/{sessionId}", hub)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return &harness{hub: hub, srv: srv, clock: clock}
}

func (h *harness) url(session, token string) string {
	return "ws" + strings.TrimPrefix(h.srv.URL, "http") + "/ws/ingest/" + session + "?token=" + token
}

func ctxT(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

const goodHello = `{"type":"hello","format":"s16le","sampleRate":16000,"channels":1,"source":"browser"}`

// connect dials, sends the hello and waits for ready.
func (h *harness) connect(t *testing.T, session string) *websocket.Conn {
	t.Helper()
	ctx := ctxT(t)
	ws, _, err := websocket.Dial(ctx, h.url(session, "good"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	if err := ws.Write(ctx, websocket.MessageText, []byte(goodHello)); err != nil {
		t.Fatal(err)
	}
	if msg := readMsg(t, ws); msg.Type != api.IngestServerMessageTypeReady {
		t.Fatalf("got %+v, want ready", msg)
	}
	return ws
}

func readMsg(t *testing.T, ws *websocket.Conn) api.IngestServerMessage {
	t.Helper()
	typ, data, err := ws.Read(ctxT(t))
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("got %v message, want text", typ)
	}
	var msg api.IngestServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}

// pcmBytes encodes samples as s16le.
func pcmBytes(samples []int16) []byte {
	b := make([]byte, 2*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(s))
	}
	return b
}

// ramp returns n samples counting up from start, so frame content is checkable.
func ramp(start, n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16(start + i)
	}
	return s
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func collect(t *testing.T, ch <-chan domain.AudioFrame, n int) []domain.AudioFrame {
	t.Helper()
	var out []domain.AudioFrame
	timeout := time.After(5 * time.Second)
	for len(out) < n {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed after %d frames, want %d", len(out), n)
			}
			out = append(out, f)
		case <-timeout:
			t.Fatalf("got %d frames, want %d", len(out), n)
		}
	}
	return out
}

func TestRejectsBeforeUpgrade(t *testing.T) {
	h := newHarness(t, Options{})
	tests := []struct {
		name, session, token string
		status               int
		code                 string
	}{
		{"bad token", "main", "nope", http.StatusUnauthorized, CodeUnauthorized},
		{"missing token", "main", "", http.StatusUnauthorized, CodeUnauthorized},
		{"unknown session", "missing", "good", http.StatusNotFound, CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, resp, err := websocket.Dial(ctxT(t), h.url(tt.session, tt.token), nil)
			if err == nil {
				t.Fatal("dial succeeded")
			}
			if resp == nil || resp.StatusCode != tt.status {
				t.Fatalf("got %v, want HTTP %d", resp, tt.status)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("content type %q", ct)
			}
			body, _ := io.ReadAll(resp.Body)
			var e api.Error
			if err := json.Unmarshal(body, &e); err != nil || e.Code != tt.code {
				t.Errorf("body %s, want code %s", body, tt.code)
			}
		})
	}
}

func TestHandshake(t *testing.T) {
	h := newHarness(t, Options{HelloTimeout: 500 * time.Millisecond})
	tests := []struct {
		name  string
		typ   websocket.MessageType
		hello string // empty: send nothing (timeout)
		ok    bool
	}{
		{"valid", websocket.MessageText, goodHello, true},
		{"valid minimal", websocket.MessageText, `{"type":"hello","format":"s16le","sampleRate":16000,"channels":1}`, true},
		{"wrong type", websocket.MessageText, `{"type":"start","format":"s16le","sampleRate":16000,"channels":1}`, false},
		{"wrong format", websocket.MessageText, `{"type":"hello","format":"f32le","sampleRate":16000,"channels":1}`, false},
		{"wrong rate", websocket.MessageText, `{"type":"hello","format":"s16le","sampleRate":48000,"channels":1}`, false},
		{"stereo", websocket.MessageText, `{"type":"hello","format":"s16le","sampleRate":16000,"channels":2}`, false},
		{"missing fields", websocket.MessageText, `{"type":"hello"}`, false},
		{"unknown source", websocket.MessageText, `{"type":"hello","format":"s16le","sampleRate":16000,"channels":1,"source":"tape"}`, false},
		{"not json", websocket.MessageText, `hello`, false},
		{"binary first", websocket.MessageBinary, "\x00\x01", false},
		{"no hello", websocket.MessageText, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ctxT(t)
			ws, _, err := websocket.Dial(ctx, h.url("hs-"+strings.ReplaceAll(tt.name, " ", "-"), "good"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ws.CloseNow() }()
			if tt.hello != "" {
				if err := ws.Write(ctx, tt.typ, []byte(tt.hello)); err != nil {
					t.Fatal(err)
				}
			}
			if tt.hello == "" {
				// A read timeout closes the socket without a message.
				if _, _, err := ws.Read(ctx); err == nil {
					t.Fatal("connection still open after the hello timeout")
				}
				return
			}
			msg := readMsg(t, ws)
			if tt.ok {
				if msg.Type != api.IngestServerMessageTypeReady {
					t.Fatalf("got %+v, want ready", msg)
				}
				return
			}
			if msg.Type != api.IngestServerMessageTypeError || msg.Error == nil || msg.Error.Code != CodeBadHello {
				t.Fatalf("got %+v, want %s error", msg, CodeBadHello)
			}
			_, _, err = ws.Read(ctx)
			if s := websocket.CloseStatus(err); s != websocket.StatusPolicyViolation {
				t.Errorf("close status %v (%v), want policy violation", s, err)
			}
		})
	}
}

func TestRechunking(t *testing.T) {
	h := newHarness(t, Options{})
	ws := h.connect(t, "main")
	ctx := ctxT(t)

	// 10 frames' worth of samples, split at awkward byte boundaries
	// (including odd ones that split a sample).
	all := pcmBytes(ramp(0, 10*domain.FrameSamples))
	for _, n := range []int{1, 639, 641, 3, 1277, 100, 2000} {
		if err := ws.Write(ctx, websocket.MessageBinary, all[:n]); err != nil {
			t.Fatal(err)
		}
		all = all[n:]
	}
	if err := ws.Write(ctx, websocket.MessageBinary, all); err != nil {
		t.Fatal(err)
	}

	ch, err := h.hub.Source("main").Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	frames := collect(t, ch, 10)
	for i, f := range frames {
		if f.T != time.Duration(i)*domain.FrameDuration {
			t.Errorf("frame %d T=%v", i, f.T)
		}
		want := ramp(i*domain.FrameSamples, domain.FrameSamples)
		if len(f.PCM) != len(want) || f.PCM[0] != want[0] || f.PCM[len(want)-1] != want[len(want)-1] {
			t.Fatalf("frame %d has wrong samples", i)
		}
	}
	if _, err := h.hub.Source("main").Start(ctx); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second Start: %v, want ErrAlreadyStarted", err)
	}
	if st := h.hub.Status("main"); !st.Connected || st.LastGapMs != nil {
		t.Errorf("status %+v, want connected with no gap", st)
	}
}

func TestReconnectReplacesAndKeepsClock(t *testing.T) {
	h := newHarness(t, Options{})
	src := h.hub.Source("main")
	ctx := ctxT(t)
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	first := h.connect(t, "main")
	if err := first.Write(ctx, websocket.MessageBinary, pcmBytes(ramp(0, 5*domain.FrameSamples))); err != nil {
		t.Fatal(err)
	}
	a := collect(t, ch, 5)

	h.clock.Advance(2*time.Second + 100*time.Millisecond)
	second := h.connect(t, "main")

	// The old connection is told why and closed.
	msg := readMsg(t, first)
	if msg.Type != api.IngestServerMessageTypeError || msg.Error == nil || msg.Error.Code != CodeReplaced {
		t.Fatalf("old connection got %+v, want %s", msg, CodeReplaced)
	}
	if _, _, err := first.Read(ctx); websocket.CloseStatus(err) != StatusReplaced {
		t.Errorf("old connection closed with %v, want %d", err, StatusReplaced)
	}

	if err := second.Write(ctx, websocket.MessageBinary, pcmBytes(ramp(0, 5*domain.FrameSamples))); err != nil {
		t.Fatal(err)
	}
	b := collect(t, ch, 5)

	if got := a[4].End(); got != 100*time.Millisecond {
		t.Fatalf("first connection ended at %v", got)
	}
	// 100 ms of audio captured over the last 100 ms → the gap is 2 s.
	if got, want := b[0].T, 2100*time.Millisecond; got != want {
		t.Errorf("first frame after reconnect T=%v, want %v", got, want)
	}
	for i := 1; i < len(b); i++ {
		if b[i].T != b[i-1].End() {
			t.Errorf("frame %d not contiguous: %v after %v", i, b[i].T, b[i-1].End())
		}
	}
	st := h.hub.Status("main")
	if !st.Connected || st.LastGapMs == nil || *st.LastGapMs != 2000 {
		t.Errorf("status %+v, want connected with lastGapMs=2000", st)
	}

	// Closing the active connection leaves the source running but disconnected.
	_ = second.Close(websocket.StatusNormalClosure, "")
	waitFor(t, "disconnect", func() bool { return !h.hub.Status("main").Connected })
}

func TestLevelMessagesAndStatus(t *testing.T) {
	h := newHarness(t, Options{LevelInterval: 20 * time.Millisecond})
	ws := h.connect(t, "main")
	ctx := ctxT(t)

	pcm := make([]int16, 10*domain.FrameSamples) // 200 ms full-scale sine
	for i := range pcm {
		pcm[i] = int16(math.Round(math.MaxInt16 * math.Sin(2*math.Pi*1000*float64(i)/domain.SampleRate)))
	}
	if err := ws.Write(ctx, websocket.MessageBinary, pcmBytes(pcm)); err != nil {
		t.Fatal(err)
	}
	for {
		msg := readMsg(t, ws)
		if msg.Type != api.IngestServerMessageTypeLevel {
			t.Fatalf("got %+v, want level", msg)
		}
		if msg.LevelDbfs == nil || msg.PeakDbfs == nil || msg.Clipping == nil || msg.Silent == nil {
			t.Fatalf("incomplete level message %+v", msg)
		}
		if !*msg.Clipping {
			continue // not processed yet
		}
		if math.Abs(float64(*msg.LevelDbfs)+3.01) > 0.1 || *msg.PeakDbfs < -0.01 || *msg.Silent {
			t.Errorf("level %v/%v silent=%v, want -3/0 dBFS", *msg.LevelDbfs, *msg.PeakDbfs, *msg.Silent)
		}
		break
	}
	st := h.hub.Status("main")
	if st.Clipping == nil || !*st.Clipping || st.Source == nil || *st.Source != api.AudioSourceKindBrowser {
		t.Errorf("status %+v", st)
	}
}

func TestJitterBufferDropsOldest(t *testing.T) {
	h := newHarness(t, Options{BufferFrames: 5})
	ws := h.connect(t, "main")
	ctx := ctxT(t)
	if err := ws.Write(ctx, websocket.MessageBinary, pcmBytes(ramp(0, 10*domain.FrameSamples))); err != nil {
		t.Fatal(err)
	}
	src := h.hub.Source("main")
	waitFor(t, "frames", func() bool { return src.Stats().Frames == 10 })
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	frames := collect(t, ch, 5)
	if frames[0].T != 5*domain.FrameDuration {
		t.Errorf("oldest kept frame T=%v, want 100ms", frames[0].T)
	}
	if d := src.Stats().Dropped; d != 5 {
		t.Errorf("dropped %d, want 5", d)
	}
}

func TestStallCountsFrameGap(t *testing.T) {
	opts := Options{Clock: &fakeClock{t: time.Unix(0, 0)}}.withDefaults()
	clock := opts.Clock.(*fakeClock)
	src := newSource("s", &opts)
	c := &conn{}
	if _, err := src.attach(c, api.AudioSourceKindBrowser); err != nil {
		t.Fatal(err)
	}
	chunk := pcmBytes(make([]int16, 5*domain.FrameSamples)) // 100 ms
	for range 5 {
		src.write(c, chunk, clock.Now())
		clock.Advance(100 * time.Millisecond)
	}
	if g := src.Stats().FrameGaps; g != 0 {
		t.Fatalf("real-time stream counted %d gaps", g)
	}
	clock.Advance(time.Second)
	src.write(c, chunk, clock.Now())
	clock.Advance(100 * time.Millisecond)
	src.write(c, chunk, clock.Now())
	st := src.Stats()
	if st.FrameGaps != 1 || st.NextT != 700*time.Millisecond {
		t.Errorf("got %+v, want 1 gap and T unchanged by the stall", st)
	}
}

func TestRemoveEndsStreamAndConnection(t *testing.T) {
	h := newHarness(t, Options{})
	ws := h.connect(t, "main")
	ch, err := h.hub.Source("main").Start(ctxT(t))
	if err != nil {
		t.Fatal(err)
	}
	h.hub.Remove("main")
	for range ch {
	}
	msg := readMsg(t, ws)
	if msg.Error == nil || msg.Error.Code != CodeClosed {
		t.Errorf("got %+v, want %s", msg, CodeClosed)
	}
}
