// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/audio/ingest"
	"github.com/iencodev/live-subtitles/internal/backoff"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
)

// fastRestart restarts after about 40 ms, so a restart loses a frame or two
// of real-time audio.
func fastRestart(maxAttempts int) RestartPolicy {
	return RestartPolicy{MaxAttempts: maxAttempts, Backoff: backoff.Policy{Base: 40 * time.Millisecond, Max: 40 * time.Millisecond}}
}

// flakyASR is the mock ASR whose first `crashes` streams close after
// crashAfter frames, and whose restarts fail to start `startErrs` times.
type flakyASR struct {
	mock.ASR
	crashes, startErrs, crashAfter int

	mu                             sync.Mutex
	starts, badStarts, crashedRuns int
}

func (a *flakyASR) Start(ctx context.Context, cfg domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	a.mu.Lock()
	a.starts++
	if a.starts > 1 && a.badStarts < a.startErrs {
		a.badStarts++
		a.mu.Unlock()
		return nil, nil, errors.New("connection refused")
	}
	crash := a.crashedRuns < a.crashes
	if crash {
		a.crashedRuns++
	}
	a.mu.Unlock()
	if !crash {
		return a.ASR.Start(ctx, cfg)
	}
	in, out := make(chan domain.AudioFrame), make(chan domain.ASREvent)
	go func() {
		defer close(out)
		for range a.crashAfter {
			select {
			case _, ok := <-in:
				if !ok {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return in, out, nil
}

// flakySource plays real-time silence. Its first `fails` runs end with an
// error after 200 ms, and its restarts fail to start `startErrs` times.
type flakySource struct {
	fails, startErrs int
	noRestart        bool
	last             time.Duration // length of the last, good run

	mu              sync.Mutex
	runs, badStarts int
	err             error
}

func (s *flakySource) Kind() api.AudioSourceKind { return api.AudioSourceKindSrt }

func (s *flakySource) Restartable() bool { return !s.noRestart }

func (s *flakySource) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *flakySource) Start(ctx context.Context) (<-chan domain.AudioFrame, error) {
	s.mu.Lock()
	if s.runs > 0 && s.badStarts < s.startErrs {
		s.badStarts++
		s.mu.Unlock()
		return nil, errors.New("listener busy")
	}
	s.runs++
	fail := s.runs <= s.fails
	s.err = nil
	s.mu.Unlock()
	d := s.last
	if fail {
		d = 200 * time.Millisecond
	}
	frames, _ := (&fake.Source{Realtime: true, Duration: d}).Start(ctx)
	out := make(chan domain.AudioFrame)
	go func() {
		defer close(out)
		for f := range frames {
			select {
			case out <- f:
			case <-ctx.Done():
				return
			}
		}
		if fail && ctx.Err() == nil {
			s.mu.Lock()
			s.err = errors.New("connection reset by peer")
			s.mu.Unlock()
		}
	}()
	return out, nil
}

// watch records the admin events and viewer captions of a session.
type watch struct {
	mu       sync.Mutex
	logs     []string // codes
	statuses []api.SessionStatus
	captions []api.Caption
}

func (e *env) watch(t *testing.T, id string) *watch {
	w := &watch{}
	evs := e.m.Events().Subscribe(t.Context())
	sub := e.bus.Subscribe(t.Context(), id, []string{domain.SourceTrack, "es"})
	go func() {
		for ev := range evs {
			w.mu.Lock()
			switch {
			case ev.Log != nil:
				w.logs = append(w.logs, ev.Log.Code)
			case ev.Status != nil && ev.Status.SessionId == id:
				w.statuses = append(w.statuses, *ev.Status)
			}
			w.mu.Unlock()
		}
	}()
	go func() {
		for msg := range sub {
			if msg.Caption != nil {
				w.mu.Lock()
				w.captions = append(w.captions, *msg.Caption)
				w.mu.Unlock()
			}
		}
	}()
	return w
}

func (w *watch) count(code string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, c := range w.logs {
		if c == code {
			n++
		}
	}
	return n
}

// check waits for the watched events to settle and returns a copy.
func (w *watch) snapshot() ([]api.SessionStatus, []api.Caption) {
	time.Sleep(50 * time.Millisecond) // the bus and the event stream are asynchronous
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]api.SessionStatus(nil), w.statuses...), append([]api.Caption(nil), w.captions...)
}

func TestProviderRestart(t *testing.T) {
	tests := []struct {
		name                string
		crashes, startErrs  int
		maxAttempts         int
		wantRestarts        int
		wantFailed          bool
		wantRestartingLogs  int
		wantRecoveryAttempt int // highest attempt seen in the status
	}{
		{"recovers", 1, 0, 3, 1, false, 1, 1},
		// The second crash comes before the stream counts as healthy.
		{"recovers twice", 2, 0, 3, 2, false, 2, 2},
		{"retries a failed start", 1, 2, 3, 1, false, 3, 3},
		{"gives up", 100, 0, 3, 3, true, 3, 3},
		{"gives up on failed starts", 1, 100, 2, 0, true, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			asr := &flakyASR{crashes: tt.crashes, startErrs: tt.startErrs, crashAfter: 10}
			e := newEnv(t, Options{Restart: fastRestart(tt.maxAttempts), Providers: map[domain.ProviderKind]Provider{
				api.ProviderKindMock: {ASR: asr, Translator: &mock.Translator{}},
			}}, sess("main", "es"))
			w := e.watch(t, "main")
			d := 1200 * time.Millisecond
			if tt.wantFailed {
				d = 10 * time.Second // it gives up long before
			}
			if _, err := e.m.Start(t.Context(), "main", &fake.Source{Realtime: true, Duration: d}); err != nil {
				t.Fatal(err)
			}
			want := api.SessionStateIdle
			if tt.wantFailed {
				want = api.SessionStateError
			}
			waitState(t, e.m, "main", want)
			statuses, captions := w.snapshot()

			st, _ := e.m.Status(t.Context(), "main")
			if tt.wantFailed {
				if st.Error == nil || st.Error.Code != CodeProviderFailed || (*st.Error.Params)["attempts"] != tt.maxAttempts {
					t.Errorf("status error %+v, want %s after %d attempts", st.Error, CodeProviderFailed, tt.maxAttempts)
				}
				if n := w.count(CodeProviderFailed); n != 1 {
					t.Errorf("%d %s logs, want 1", n, CodeProviderFailed)
				}
			} else if st.Error != nil {
				t.Errorf("recovered session ended with %+v", st.Error)
			}
			if n := w.count(CodeProviderRestarting); n != tt.wantRestartingLogs {
				t.Errorf("%d %s logs, want %d", n, CodeProviderRestarting, tt.wantRestartingLogs)
			}
			if n := w.count(CodeProviderRestarted); n != tt.wantRestarts {
				t.Errorf("%d %s logs, want %d", n, CodeProviderRestarted, tt.wantRestarts)
			}

			// The session stayed live while it recovered, and said so.
			maxAttempt, maxRestarts := 0, 0
			for _, s := range statuses {
				if s.State == api.SessionStateError && !tt.wantFailed {
					t.Fatalf("went to error: %+v", s.Error)
				}
				if s.Recovering != nil {
					if s.State != api.SessionStateLive || s.Recovering.Component != api.RecoveryComponentProvider ||
						s.Recovering.MaxAttempts != tt.maxAttempts || s.Recovering.RetryAt == nil || s.Recovering.Error == nil {
						t.Errorf("recovering status %+v in state %s", s.Recovering, s.State)
					}
					maxAttempt = max(maxAttempt, s.Recovering.Attempt)
				}
				if s.Restarts != nil {
					maxRestarts = max(maxRestarts, *s.Restarts)
				}
			}
			if maxAttempt != tt.wantRecoveryAttempt || maxRestarts != tt.wantRestarts {
				t.Errorf("status showed attempt %d and %d restarts, want %d and %d", maxAttempt, maxRestarts, tt.wantRecoveryAttempt, tt.wantRestarts)
			}
			if tt.wantFailed {
				return
			}

			// Captions after each restart have their own segment IDs and the
			// first ones mark the lost audio, on every track.
			if w.count(CodeAudioGap) != tt.wantRestarts {
				t.Errorf("%d %s logs, want %d", w.count(CodeAudioGap), CodeAudioGap, tt.wantRestarts)
			}
			marked := map[string]bool{}
			for _, c := range captions {
				if c.GapBeforeMs != nil && *c.GapBeforeMs > 0 {
					marked[c.Lang] = true
					if !strings.HasPrefix(c.SegmentId, "r0-1-") && !strings.HasPrefix(c.SegmentId, "r0-2-") {
						t.Errorf("gap marker on %s %q, before any restart", c.Lang, c.SegmentId)
					}
				}
			}
			if !marked[domain.SourceTrack] || !marked["es"] {
				t.Errorf("gap marked on tracks %v, want source and es", marked)
			}
			stored := finals(t, e.store, "main", domain.SourceTrack)
			last := stored[len(stored)-1]
			if want := "r0-" + string(rune('0'+tt.wantRestarts)) + "-"; !strings.HasPrefix(last.SegmentId, want) {
				t.Errorf("last final %q, want prefix %q", last.SegmentId, want)
			}
		})
	}
}

// A stream that ran long enough counts as recovered: its crash starts
// counting attempts from 1 again.
func TestProviderRestartResetsAfterHealthy(t *testing.T) {
	asr := &flakyASR{crashes: 3, crashAfter: 10}
	pol := fastRestart(1)
	pol.HealthyAfter = time.Millisecond
	e := newEnv(t, Options{Restart: pol, Providers: map[domain.ProviderKind]Provider{
		api.ProviderKindMock: {ASR: asr, Translator: &mock.Translator{}},
	}}, sess("main"))
	if _, err := e.m.Start(t.Context(), "main", &fake.Source{Realtime: true, Duration: 1500 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.m, "main", api.SessionStateIdle)
	if st, _ := e.m.Status(t.Context(), "main"); st.Error != nil {
		t.Errorf("status error %+v: three crashes of healthy streams, one attempt each", st.Error)
	}
}

func TestStopWhileRecovering(t *testing.T) {
	asr := &flakyASR{crashes: 1, crashAfter: 5}
	pol := RestartPolicy{Backoff: backoff.Policy{Base: time.Hour}}
	e := newEnv(t, Options{Restart: pol, Providers: map[domain.ProviderKind]Provider{
		api.ProviderKindMock: {ASR: asr, Translator: &mock.Translator{}},
	}}, sess("main"))
	if _, err := e.m.Start(t.Context(), "main", &fake.Source{Realtime: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, _ := e.m.Status(t.Context(), "main")
		if st.Recovering != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never recovering")
		}
		time.Sleep(5 * time.Millisecond)
	}
	st, err := e.m.Stop(t.Context(), "main")
	if err != nil || st.State != api.SessionStateIdle || st.Error != nil {
		t.Errorf("stop: %+v, %v", st, err)
	}
}

func TestSourceRestart(t *testing.T) {
	tests := []struct {
		name               string
		src                *flakySource
		maxAttempts        int
		wantRestarts       int
		wantFailed         bool
		wantAttempts       any // error param
		wantRestartingLogs int
	}{
		{"recovers", &flakySource{fails: 1}, 3, 1, false, nil, 1},
		{"retries a failed start", &flakySource{fails: 1, startErrs: 1}, 3, 1, false, nil, 2},
		{"gives up", &flakySource{fails: 100}, 2, 2, true, 2, 2},
		{"not restartable", &flakySource{fails: 1, noRestart: true}, 3, 0, true, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.src.last = time.Second
			e := newEnv(t, Options{Restart: fastRestart(tt.maxAttempts)}, sess("main", "es"))
			w := e.watch(t, "main")
			if _, err := e.m.Start(t.Context(), "main", tt.src); err != nil {
				t.Fatal(err)
			}
			want := api.SessionStateIdle
			if tt.wantFailed {
				want = api.SessionStateError
			}
			waitState(t, e.m, "main", want)
			statuses, captions := w.snapshot()
			st, _ := e.m.Status(t.Context(), "main")
			if tt.wantFailed {
				if st.Error == nil || st.Error.Code != CodeSourceFailed || (*st.Error.Params)["attempts"] != tt.wantAttempts {
					t.Errorf("status error %+v, want %s with attempts %v", st.Error, CodeSourceFailed, tt.wantAttempts)
				}
			} else if st.Error != nil {
				t.Errorf("recovered session ended with %+v", st.Error)
			}
			if n := w.count(CodeSourceRestarting); n != tt.wantRestartingLogs {
				t.Errorf("%d %s logs, want %d", n, CodeSourceRestarting, tt.wantRestartingLogs)
			}
			if n := w.count(CodeSourceRestarted); n != tt.wantRestarts {
				t.Errorf("%d %s logs, want %d", n, CodeSourceRestarted, tt.wantRestarts)
			}
			for _, s := range statuses {
				if s.Recovering != nil && (s.State != api.SessionStateLive || s.Recovering.Component != api.RecoveryComponentSource) {
					t.Errorf("recovering status %+v in state %s", s.Recovering, s.State)
				}
			}
			if tt.wantFailed {
				return
			}
			// The restarted source continues the session clock after the
			// lost time, and the next captions mark the gap.
			var gapped bool
			var prevEnd float32
			for _, c := range captions {
				if c.GapBeforeMs != nil && *c.GapBeforeMs >= 20 {
					gapped = true
				}
				if c.Lang == domain.SourceTrack && c.Final {
					if c.Start < prevEnd {
						t.Errorf("caption %q starts at %.2f, before the previous end %.2f", c.SegmentId, c.Start, prevEnd)
					}
					prevEnd = c.End
				}
			}
			if !gapped || w.count(CodeAudioGap) != 1 {
				t.Errorf("gap marked: %v, %d %s logs", gapped, w.count(CodeAudioGap), CodeAudioGap)
			}
			if prevEnd < 1.1 {
				t.Errorf("last caption ends at %.2f s, want after both runs", prevEnd)
			}
		})
	}
}

// ingestClock is the ingest hub's clock, advanced by hand.
type ingestClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *ingestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *ingestClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// The capture station dropping its WebSocket and coming back doesn't stop
// the session; the lost time is a gap in the captions.
func TestIngestReconnectKeepsSessionLive(t *testing.T) {
	clk := &ingestClock{t: time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)}
	hub := ingest.NewHub(func(context.Context, string, string) error { return nil },
		ingest.Options{Clock: clk, Logger: slog.New(slog.DiscardHandler), LevelInterval: time.Hour})
	mux := http.NewServeMux()
	mux.Handle("GET /ws/ingest/{sessionId}", hub)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	e := newEnv(t, Options{
		IngestSource:  func(id string) domain.AudioSource { return hub.Source(id) },
		IngestStatus:  hub.Status,
		ReleaseIngest: hub.Remove,
	}, sess("main", "es"))
	w := e.watch(t, "main")
	if _, err := e.m.Start(t.Context(), "main", nil); err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	speak := func(d time.Duration) {
		t.Helper()
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/ingest/main?token=x", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ws.CloseNow() }()
		hello := `{"type":"hello","format":"s16le","sampleRate":16000,"channels":1,"source":"browser"}`
		if err := ws.Write(ctx, websocket.MessageText, []byte(hello)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.Read(ctx); err != nil { // ready
			t.Fatal(err)
		}
		frame := make([]byte, 2*domain.FrameSamples)
		for i := range domain.FrameSamples {
			binary.LittleEndian.PutUint16(frame[2*i:], uint16(int16(1000)))
		}
		for range d / domain.FrameDuration {
			clk.advance(domain.FrameDuration)
			before := hub.Source("main").Stats().Frames
			if err := ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
				t.Fatal(err)
			}
			// The hub reads the clock as each frame arrives: advancing it
			// before the hub took the last frame would stamp that frame
			// late and show up as an extra gap on a slow runner.
			for hub.Source("main").Stats().Frames == before {
				time.Sleep(time.Millisecond)
			}
		}
		time.Sleep(100 * time.Millisecond) // let the session take the audio
		_ = ws.Close(websocket.StatusGoingAway, "network drop")
	}
	speak(time.Second)
	clk.advance(3 * time.Second) // the station is away for 3 s
	if st := e.m.State("main"); st != api.SessionStateLive {
		t.Fatalf("state %s after the drop, want live", st)
	}
	speak(2 * time.Second)
	if st := e.m.State("main"); st != api.SessionStateLive {
		t.Fatalf("state %s after the reconnect, want live", st)
	}
	if _, err := e.m.Stop(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	_, captions := w.snapshot()
	var gap int
	for _, c := range captions {
		if c.GapBeforeMs != nil {
			gap = *c.GapBeforeMs
			if c.End <= 4 {
				t.Errorf("gap marked on %q ending at %.2f s, before the reconnect", c.SegmentId, c.End)
			}
		}
	}
	if gap != 3000 || w.count(CodeAudioGap) != 1 {
		t.Errorf("caption gap %d ms, %d %s logs; want 3000 ms, 1", gap, w.count(CodeAudioGap), CodeAudioGap)
	}
	if w.count(CodeSourceRestarting) != 0 || w.count(CodeProviderRestarting) != 0 {
		t.Error("a reconnect restarted the pipeline")
	}
}
