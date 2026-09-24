// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/bus"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
	"github.com/iencodev/live-subtitles/internal/store"
)

type env struct {
	m     *Manager
	store *store.Store
	bus   *bus.Bus
}

func newEnv(t *testing.T, opts Options, sessions ...domain.Session) *env {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now()
	for _, s := range sessions {
		s.CreatedAt, s.UpdatedAt = now, now
		if err := st.CreateSession(t.Context(), s); err != nil {
			t.Fatal(err)
		}
	}
	b := bus.New()
	opts.Sessions, opts.Captions, opts.Bus = st, st, b
	if opts.Providers == nil {
		opts.Providers = map[domain.ProviderKind]Provider{
			api.ProviderKindMock: {ASR: &mock.ASR{}, Translator: &mock.Translator{}},
		}
	}
	if opts.IngestSource == nil {
		opts.IngestSource = func(string) domain.AudioSource { return &fake.Source{} }
	}
	opts.Logger = slog.New(slog.DiscardHandler)
	m := New(opts)
	t.Cleanup(m.Close)
	return &env{m: m, store: st, bus: b}
}

func sess(id string, targets ...string) domain.Session {
	return domain.Session{Id: id, Name: id, Provider: api.ProviderChoiceDefault, SourceLanguage: api.Auto, TargetLanguages: targets}
}

// waitState waits until the session reaches state.
func waitState(t *testing.T, m *Manager, id string, state api.SessionState) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for m.State(id) != state {
		if time.Now().After(deadline) {
			t.Fatalf("session %s is %s, want %s", id, m.State(id), state)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func finals(t *testing.T, st *store.Store, id, track string) []api.Caption {
	t.Helper()
	items, _, err := st.ListCaptions(t.Context(), domain.CaptionQuery{SessionID: id, Track: track, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// The walking skeleton: fake audio → mock ASR → mock translation → bus
// and store, for the source track and both targets.
func TestPipelineEndToEnd(t *testing.T) {
	e := newEnv(t, Options{}, sess("main", "es", "en"))
	ctx := t.Context()
	sub := e.bus.Subscribe(ctx, "main", []string{"source", "es", "en"})
	<-sub // history

	// 40 s of audio at 300 ms per word is the whole script and a bit more.
	st, err := e.m.Start(ctx, "main", &fake.Source{Duration: 40 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != api.SessionStateLive || st.Provider == nil || *st.Provider != api.ProviderKindMock {
		t.Errorf("start status %+v", st)
	}
	waitState(t, e.m, "main", api.SessionStateIdle)

	src := finals(t, e.store, "main", "source")
	if len(src) < len(mock.DefaultScript) {
		t.Fatalf("%d source finals, want at least %d", len(src), len(mock.DefaultScript))
	}
	// The script loops; the last sentence is cut by the end of the audio.
	for i, c := range src[:len(mock.DefaultScript)] {
		line := mock.DefaultScript[i]
		// `auto` alternates three English lines, then three Spanish ones.
		wantLang, wantText := "en", line.EN
		if (i/3)%2 == 1 {
			wantLang, wantText = "es", line.ES
		}
		if c.SourceLang != wantLang || c.Text != wantText {
			t.Errorf("source %d = %s %q, want %s %q", i, c.SourceLang, c.Text, wantLang, wantText)
		}
		if !strings.HasPrefix(c.SegmentId, "r0-") {
			t.Errorf("segment %q lacks the run prefix", c.SegmentId)
		}
	}
	for _, track := range []string{"es", "en"} {
		got := finals(t, e.store, "main", track)
		if len(got) != len(src) {
			t.Fatalf("%s: %d finals, want %d", track, len(got), len(src))
		}
		for i, c := range got[:len(mock.DefaultScript)] {
			line := mock.DefaultScript[i]
			want := line.EN
			if track == "es" {
				want = line.ES
			}
			if c.Text != want || c.SegmentId != src[i].SegmentId || c.Start != src[i].Start {
				t.Errorf("%s %d = %q (%s @%.1f), want %q", track, i, c.Text, c.SegmentId, c.Start, want)
			}
		}
	}

	// Viewers saw interims, finals and the state changes.
	var states []api.SessionState
	interims := 0
	timeout := time.After(5 * time.Second)
	for len(states) == 0 || states[len(states)-1] != api.SessionStateIdle {
		select {
		case msg := <-sub:
			switch msg.Type {
			case api.CaptionsServerMessageTypeState:
				if len(states) == 0 || states[len(states)-1] != *msg.State {
					states = append(states, *msg.State)
				}
			case api.CaptionsServerMessageTypeCaption:
				if !msg.Caption.Final {
					interims++
				}
			}
		case <-timeout:
			t.Fatalf("states %v", states)
		}
	}
	if want := []api.SessionState{"starting", "live", "idle"}; fmt.Sprint(states) != fmt.Sprint(want) {
		t.Errorf("states %v, want %v", states, want)
	}
	if interims == 0 {
		t.Error("no interim captions reached viewers")
	}
}

func TestPauseResumeStop(t *testing.T) {
	e := newEnv(t, Options{StopTimeout: 5 * time.Second}, sess("main", "es"))
	ctx := t.Context()

	if _, err := e.m.Pause(ctx, "main"); !errors.Is(err, ErrState) {
		t.Errorf("pause while idle: %v, want ErrState", err)
	}
	// An endless source paced in real time.
	if _, err := e.m.Start(ctx, "main", &fake.Source{Realtime: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Start(ctx, "main", nil); !errors.Is(err, ErrState) || !errors.Is(err, domain.ErrConflict) {
		t.Errorf("start while live: %v, want ErrState", err)
	}
	time.Sleep(200 * time.Millisecond)
	st, err := e.m.Pause(ctx, "main")
	if err != nil || st.State != api.SessionStatePaused {
		t.Fatalf("pause: %+v, %v", st, err)
	}
	sent := *st.Usage.AudioSeconds
	time.Sleep(200 * time.Millisecond)
	if st, _ := e.m.Status(ctx, "main"); *st.Usage.AudioSeconds > sent+0.021 {
		t.Errorf("audio still sent while paused: %.2f → %.2f s", sent, *st.Usage.AudioSeconds)
	}
	if _, err := e.m.Pause(ctx, "main"); !errors.Is(err, ErrState) {
		t.Errorf("pause while paused: %v", err)
	}
	if st, err := e.m.Start(ctx, "main", nil); err != nil || st.State != api.SessionStateLive {
		t.Fatalf("resume: %+v, %v", st, err)
	}
	time.Sleep(800 * time.Millisecond) // a few words
	st, err = e.m.Stop(ctx, "main")
	if err != nil || st.State != api.SessionStateIdle {
		t.Fatalf("stop: %+v, %v", st, err)
	}
	// The sentence cut by the stop was flushed as a final and translated.
	src, es := finals(t, e.store, "main", "source"), finals(t, e.store, "main", "es")
	if len(src) == 0 || len(es) != len(src) {
		t.Errorf("%d source finals, %d es finals", len(src), len(es))
	}
	if st, err := e.m.Stop(ctx, "main"); err != nil || st.State != api.SessionStateIdle {
		t.Errorf("stopping an idle session: %+v, %v", st, err)
	}
	for _, op := range []func(context.Context, string) (api.SessionStatus, error){
		e.m.Pause, e.m.Stop, e.m.Status,
		func(ctx context.Context, id string) (api.SessionStatus, error) { return e.m.Start(ctx, id, nil) },
	} {
		if _, err := op(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("unknown session: %v, want ErrNotFound", err)
		}
	}
}

func TestProviderUnavailable(t *testing.T) {
	s := sess("main", "es")
	s.Provider = api.ProviderChoiceGemini
	e := newEnv(t, Options{}, s)
	ctx := t.Context()
	st, err := e.m.Start(ctx, "main", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("start: %v, want ErrUnavailable", err)
	}
	if st.State != api.SessionStateError || st.Error == nil || st.Error.Code != CodeProviderUnavailable {
		t.Errorf("status %+v", st)
	}
	if e.m.State("main") != api.SessionStateError {
		t.Errorf("state %s", e.m.State("main"))
	}
	if st, _ := e.m.Stop(ctx, "main"); st.State != api.SessionStateIdle || st.Error != nil {
		t.Errorf("stop did not clear the error: %+v", st)
	}
}

func TestDefaultProviderRule(t *testing.T) {
	e := newEnv(t, Options{
		DefaultProvider: func(context.Context) domain.ProviderKind { return api.ProviderKindLocal },
	}, sess("main", "es"))
	if _, err := e.m.Start(t.Context(), "main", nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("default resolved to a missing provider: %v", err)
	}
}

// failingSource can't start.
type failingSource struct{ fake.Source }

func (failingSource) Start(context.Context) (<-chan domain.AudioFrame, error) {
	return nil, errors.New("device busy")
}

func TestSourceUnavailable(t *testing.T) {
	e := newEnv(t, Options{}, sess("main", "es"))
	st, err := e.m.Start(t.Context(), "main", &failingSource{})
	if !errors.Is(err, ErrUnavailable) || st.Error == nil || st.Error.Code != CodeSourceUnavailable {
		t.Errorf("start: %+v, %v", st, err)
	}
}

// crashingASR closes its event stream right away.
type crashingASR struct{}

func (crashingASR) Kind() domain.ProviderKind { return api.ProviderKindMock }

func (crashingASR) Start(ctx context.Context, _ domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	in, out := make(chan domain.AudioFrame, 1), make(chan domain.ASREvent)
	close(out)
	return in, out, nil
}

func TestProviderCrash(t *testing.T) {
	e := newEnv(t, Options{Providers: map[domain.ProviderKind]Provider{
		api.ProviderKindMock: {ASR: crashingASR{}, Translator: &mock.Translator{}},
	}}, sess("main", "es"))
	if _, err := e.m.Start(t.Context(), "main", &fake.Source{Realtime: true}); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.m, "main", api.SessionStateError)
	st, _ := e.m.Status(t.Context(), "main")
	if st.Error == nil || st.Error.Code != CodeProviderError {
		t.Errorf("status %+v", st)
	}
}

func TestSessionClockAcrossRuns(t *testing.T) {
	e := newEnv(t, Options{}, sess("main", "es"))
	ctx := t.Context()
	for range 2 {
		if _, err := e.m.Start(ctx, "main", &fake.Source{Duration: 10 * time.Second}); err != nil {
			t.Fatal(err)
		}
		waitState(t, e.m, "main", api.SessionStateIdle)
	}
	src := finals(t, e.store, "main", "source")
	var first, second []api.Caption
	for _, c := range src {
		if strings.HasPrefix(c.SegmentId, "r0-") {
			first = append(first, c)
		} else if strings.HasPrefix(c.SegmentId, "r11-") {
			second = append(second, c)
		} else {
			t.Errorf("segment %q from an unexpected run", c.SegmentId)
		}
	}
	if len(first) == 0 || len(second) != len(first) {
		t.Fatalf("%d captions in run 1, %d in run 2", len(first), len(second))
	}
	if second[0].Start < 11 || first[len(first)-1].End > 10 {
		t.Errorf("run 2 starts at %.1f s, run 1 ends at %.1f s", second[0].Start, first[len(first)-1].End)
	}

	// After a restart the clock continues from the stored captions.
	m2 := New(Options{Sessions: e.store, Captions: e.store, Bus: e.bus, Logger: slog.New(slog.DiscardHandler),
		Providers:    map[domain.ProviderKind]Provider{api.ProviderKindMock: {ASR: &mock.ASR{}, Translator: &mock.Translator{}}},
		IngestSource: func(string) domain.AudioSource { return &fake.Source{} }})
	defer m2.Close()
	if got := m2.clockOrigin(ctx, "main"); got < 21*time.Second || got > 23*time.Second {
		t.Errorf("clock origin after restart %v", got)
	}
}

// slowTranslator counts calls and takes its time.
type slowTranslator struct {
	mock.Translator
	interims, finals atomic.Int32
}

func (s *slowTranslator) Translate(ctx context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	if req.Final {
		s.finals.Add(1)
	} else {
		s.interims.Add(1)
	}
	time.Sleep(20 * time.Millisecond)
	return s.Translator.Translate(ctx, req)
}

func TestInterimsAreDebounced(t *testing.T) {
	tr := &slowTranslator{}
	s := sess("main", "fr")
	s.SourceLanguage = api.En
	e := newEnv(t, Options{Providers: map[domain.ProviderKind]Provider{
		// Unpaced audio at 20 ms per word: interims come much faster than translations.
		api.ProviderKindMock: {ASR: &mock.ASR{WordEvery: 20 * time.Millisecond}, Translator: tr},
	}}, s)
	if _, err := e.m.Start(t.Context(), "main", &fake.Source{Duration: 12 * time.Second}); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.m, "main", api.SessionStateIdle)
	src := finals(t, e.store, "main", "source")
	words := 0
	for _, c := range src {
		words += len(strings.Fields(c.Text))
	}
	if int(tr.finals.Load()) != len(src) {
		t.Errorf("%d final translations for %d finals", tr.finals.Load(), len(src))
	}
	if n := int(tr.interims.Load()); n == 0 || n >= words-len(src) {
		t.Errorf("%d interim translations for %d interims: not debounced", n, words-len(src))
	}
	if got := finals(t, e.store, "main", "fr"); len(got) != len(src) || !strings.HasPrefix(got[0].Text, "[fr] ") {
		t.Errorf("fr finals %+v", got)
	}
}

func TestAdminEvents(t *testing.T) {
	e := newEnv(t, Options{StatusInterval: 50 * time.Millisecond}, sess("main", "es"), sess("side", "en"))
	srv := httptest.NewServer(e.m.AdminHandler(slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	read := func() api.AdminEvent {
		t.Helper()
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ev api.AdminEvent
		if err := json.Unmarshal(b, &ev); err != nil {
			t.Fatal(err)
		}
		return ev
	}
	// Snapshot: one idle status per session.
	seen := map[string]api.SessionState{}
	for range 2 {
		ev := read()
		if ev.Type != api.AdminEventTypeSessionStatus || ev.Status == nil {
			t.Fatalf("snapshot event %+v", ev)
		}
		seen[ev.Status.SessionId] = ev.Status.State
	}
	if seen["main"] != api.SessionStateIdle || seen["side"] != api.SessionStateIdle {
		t.Errorf("snapshot %v", seen)
	}

	if _, err := e.m.Start(ctx, "main", &fake.Source{Realtime: true}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	states := map[api.SessionState]int{}
	for states[api.SessionStateLive] < 3 { // the change plus periodic updates
		ev := read()
		if ev.Type == api.AdminEventTypeSessionStatus && ev.Status.SessionId == "main" {
			mu.Lock()
			states[ev.Status.State]++
			mu.Unlock()
		}
	}
	if states[api.SessionStateStarting] != 1 {
		t.Errorf("states seen %v", states)
	}
}
