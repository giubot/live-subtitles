// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fakeConn is a Live connection the test scripts.
type fakeConn struct {
	msgs   chan *genai.LiveServerMessage
	errs   chan error
	closed chan struct{}
	once   sync.Once

	mu        sync.Mutex
	audio     int // bytes received
	streamEnd bool
	sendErr   error
}

func newFakeConn() *fakeConn {
	return &fakeConn{msgs: make(chan *genai.LiveServerMessage, 16), errs: make(chan error, 1), closed: make(chan struct{})}
}

func (c *fakeConn) SendAudio(pcm []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sendErr != nil {
		return c.sendErr
	}
	c.audio += len(pcm)
	return nil
}

func (c *fakeConn) SendAudioStreamEnd() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamEnd = true
	return nil
}

func (c *fakeConn) Receive() (*genai.LiveServerMessage, error) {
	select {
	case m := <-c.msgs:
		return m, nil
	case err := <-c.errs:
		return nil, err
	case <-c.closed:
		return nil, io.EOF
	}
}

func (c *fakeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *fakeConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *fakeConn) audioBytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.audio
}

func (c *fakeConn) ended() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streamEnd
}

// fakeDialer hands out scripted connections (nil entry: the dial fails).
type fakeDialer struct {
	mu    sync.Mutex
	conns []*fakeConn
	cfgs  []dialConfig
}

func (d *fakeDialer) dial(_ context.Context, dc dialConfig) (liveConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cfgs = append(d.cfgs, dc)
	if len(d.conns) == 0 {
		return nil, errors.New("no more connections")
	}
	c := d.conns[0]
	d.conns = d.conns[1:]
	if c == nil {
		return nil, errors.New("dial failed")
	}
	return c, nil
}

func (d *fakeDialer) configs() []dialConfig {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]dialConfig(nil), d.cfgs...)
}

func testASR(d *fakeDialer) *ASR {
	return &ASR{
		APIKey:    func(context.Context) (string, error) { return "test-key", nil },
		SendEvery: 20 * time.Millisecond, IdleFinal: time.Hour, MaxRetries: 2,
		Backoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond, DrainTimeout: 300 * time.Millisecond,
		dial: d.dial, tick: 5 * time.Millisecond,
	}
}

func frame(t time.Duration) domain.AudioFrame {
	return domain.AudioFrame{PCM: make([]int16, domain.FrameSamples), T: t}
}

func transcript(text string) *genai.LiveServerMessage {
	return &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{InputTranscription: &genai.Transcription{Text: text}}}
}

func turnComplete() *genai.LiveServerMessage {
	return &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{TurnComplete: true}}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// next returns the next event with text or an error, summing usage.
func next(t *testing.T, out <-chan domain.ASREvent, usage *domain.Usage) domain.ASREvent {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				t.Fatal("stream closed")
			}
			if usage != nil {
				*usage = usage.Add(ev.Usage)
			}
			if ev.Text != "" || ev.Err != nil {
				return ev
			}
		case <-timeout:
			t.Fatal("no event")
		}
	}
}

// drain reads until out closes and returns the text events.
func drain(t *testing.T, out <-chan domain.ASREvent, usage *domain.Usage) []domain.ASREvent {
	t.Helper()
	var evs []domain.ASREvent
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				return evs
			}
			if usage != nil {
				*usage = usage.Add(ev.Usage)
			}
			if ev.Text != "" || ev.Err != nil {
				evs = append(evs, ev)
			}
		case <-timeout:
			t.Fatal("stream did not close")
		}
	}
}

func TestStartErrors(t *testing.T) {
	boom := errors.New("keychain locked")
	cases := []struct {
		name   string
		key    func(context.Context) (string, error)
		conns  []*fakeConn
		wantIs error
		want   string
	}{
		{"no key func", nil, nil, ErrNoAPIKey, ""},
		{"empty key", func(context.Context) (string, error) { return "  ", nil }, nil, ErrNoAPIKey, ""},
		{"not set", func(context.Context) (string, error) { return "", ErrNoAPIKey }, nil, ErrNoAPIKey, ""},
		{"store error", func(context.Context) (string, error) { return "", boom }, nil, boom, "read the Google API key"},
		{"dial fails", func(context.Context) (string, error) { return "k", nil }, []*fakeConn{nil}, nil, "connect to the Live API"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testASR(&fakeDialer{conns: tc.conns})
			a.APIKey = tc.key
			_, _, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: api.Auto})
			if err == nil {
				t.Fatal("Start succeeded")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Errorf("err = %v, want %v", err, tc.wantIs)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "test-key") {
				t.Errorf("err leaks the key: %v", err)
			}
		})
	}
}

func TestStartConfig(t *testing.T) {
	settings := func(model string) func(context.Context) (api.Settings, error) {
		return func(context.Context) (api.Settings, error) {
			var s api.Settings
			s.Providers.Gemini.LiveModel = model
			return s, nil
		}
	}
	cases := []struct {
		name      string
		settings  func(context.Context) (api.Settings, error)
		source    api.SourceLanguage
		glossary  *domain.Glossary
		wantModel string
		wantLang  domain.LanguageCode
		wantTerms []string
	}{
		{"defaults", nil, api.Auto, nil, DefaultLiveModel, "", nil},
		{"no settings yet", func(context.Context) (api.Settings, error) { return api.Settings{}, domain.ErrNotFound }, api.Auto, nil, DefaultLiveModel, "", nil},
		{"empty model", settings(""), api.Auto, nil, DefaultLiveModel, "", nil},
		{"model from settings", settings("gemini-live-x"), api.Es, nil, "gemini-live-x", "es", nil},
		{"glossary", nil, api.En, &domain.Glossary{Terms: []api.GlossaryTerm{{Term: "Kubernetes"}, {Term: " "}}, DoNotTranslate: []string{"Nerdearla", "Kubernetes"}},
			DefaultLiveModel, "en", []string{"Kubernetes", "Nerdearla"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDialer{conns: []*fakeConn{newFakeConn()}}
			a := testASR(d)
			a.Settings = tc.settings
			in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: tc.source, Glossary: tc.glossary})
			if err != nil {
				t.Fatal(err)
			}
			close(in)
			drain(t, out, nil)
			dc := d.configs()[0]
			if dc.Model != tc.wantModel || dc.Language != tc.wantLang || dc.APIKey != "test-key" || dc.Handle != "" {
				t.Errorf("dial config = %+v", dc)
			}
			if strings.Join(dc.Glossary, "|") != strings.Join(tc.wantTerms, "|") {
				t.Errorf("glossary = %q, want %q", dc.Glossary, tc.wantTerms)
			}
		})
	}
}

func TestTranscriptionToEvents(t *testing.T) {
	conn := newFakeConn()
	a := testASR(&fakeDialer{conns: []*fakeConn{conn}})
	in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: api.Auto})
	if err != nil {
		t.Fatal(err)
	}
	// 1 s of audio starting at session clock 10 s.
	base := 10 * time.Second
	for i := range 50 {
		in <- frame(base + time.Duration(i)*domain.FrameDuration)
	}
	waitFor(t, "audio", func() bool { return conn.audioBytes() == 50*domain.FrameSamples*2 })

	var usage domain.Usage
	conn.msgs <- transcript(" Hello and welcome")
	ev := next(t, out, &usage)
	if ev.Final || ev.Text != "Hello and welcome" || ev.Lang != "en" || ev.SegmentID != "g000001" {
		t.Errorf("interim = %+v", ev)
	}
	if ev.End != 11*time.Second || ev.Start < base || ev.Start >= ev.End {
		t.Errorf("interim times = %v..%v", ev.Start, ev.End)
	}
	conn.msgs <- transcript(" to the conference. Today we")
	fin := next(t, out, &usage)
	if !fin.Final || fin.Text != "Hello and welcome to the conference." || fin.SegmentID != "g000001" || fin.Start != ev.Start {
		t.Errorf("sentence final = %+v", fin)
	}
	rest := next(t, out, &usage)
	if rest.Final || rest.Text != "Today we" || rest.SegmentID != "g000002" || rest.Start != fin.End {
		t.Errorf("next interim = %+v (previous end %v)", rest, fin.End)
	}
	conn.msgs <- &genai.LiveServerMessage{UsageMetadata: &genai.UsageMetadata{PromptTokenCount: 120, ResponseTokenCount: 2}}
	conn.msgs <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{ModelTurn: genai.NewContentFromText("es", genai.RoleModel)}}
	conn.msgs <- transcript(" talk")
	next(t, out, &usage)
	conn.msgs <- turnComplete()
	last := next(t, out, &usage)
	if !last.Final || last.Text != "Today we talk" || last.SegmentID != "g000002" {
		t.Errorf("turn final = %+v", last)
	}
	if last.Lang != "es" { // the model's tag outranks the guess from the words
		t.Errorf("lang = %q, want the model's tag", last.Lang)
	}

	close(in)
	waitFor(t, "stream end", conn.ended)
	conn.msgs <- turnComplete()
	drain(t, out, &usage)
	if math.Abs(usage.AudioSeconds-1) > 1e-9 || usage.InputTokens != 120 || usage.OutputTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
	if !conn.isClosed() {
		t.Error("connection left open")
	}
}

func TestLanguage(t *testing.T) {
	cases := []struct {
		name   string
		source api.SourceLanguage
		msgs   []*genai.LiveServerMessage
		want   domain.LanguageCode
	}{
		{"pinned wins over the words", api.Es, []*genai.LiveServerMessage{transcript("the talk is about the cloud")}, "es"},
		{"API language code", api.Auto, []*genai.LiveServerMessage{{ServerContent: &genai.LiveServerContent{
			InputTranscription: &genai.Transcription{Text: "the talk", LanguageCode: "es-419"}}}}, "es"},
		{"model tag when the words are unclear", api.Auto, []*genai.LiveServerMessage{
			{ServerContent: &genai.LiveServerContent{OutputTranscription: &genai.Transcription{Text: "Spanish."}}},
			transcript("Kubernetes")}, "es"},
		{"words: Spanish", api.Auto, []*genai.LiveServerMessage{transcript("¿Qué es la nube y cómo funciona?")}, "es"},
		{"words: English", api.Auto, []*genai.LiveServerMessage{transcript("What is the cloud and how does it work?")}, "en"},
		{"unknown", api.Auto, []*genai.LiveServerMessage{transcript("Kubernetes")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := newFakeConn()
			a := testASR(&fakeDialer{conns: []*fakeConn{conn}})
			in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: tc.source})
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range tc.msgs {
				conn.msgs <- m
			}
			conn.msgs <- turnComplete()
			var final domain.ASREvent
			for !final.Final {
				final = next(t, out, nil)
			}
			if final.Lang != tc.want {
				t.Errorf("lang = %q, want %q", final.Lang, tc.want)
			}
			close(in)
			drain(t, out, nil)
		})
	}
}

func TestIdleFinal(t *testing.T) {
	conn := newFakeConn()
	a := testASR(&fakeDialer{conns: []*fakeConn{conn}})
	a.IdleFinal = 30 * time.Millisecond
	in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: api.Auto})
	if err != nil {
		t.Fatal(err)
	}
	conn.msgs <- transcript("a sentence with no end")
	if ev := next(t, out, nil); ev.Final {
		t.Fatalf("first event is final: %+v", ev)
	}
	if ev := next(t, out, nil); !ev.Final || ev.Text != "a sentence with no end" {
		t.Errorf("after silence = %+v", ev)
	}
	close(in)
	drain(t, out, nil)
}

func TestGoAwayRotatesWithResumption(t *testing.T) {
	c1, c2 := newFakeConn(), newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{c1, c2}}
	a := testASR(d)
	in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: api.Auto})
	if err != nil {
		t.Fatal(err)
	}
	c1.msgs <- &genai.LiveServerMessage{SessionResumptionUpdate: &genai.LiveServerSessionResumptionUpdate{NewHandle: "h1", Resumable: true}}
	c1.msgs <- &genai.LiveServerMessage{SessionResumptionUpdate: &genai.LiveServerSessionResumptionUpdate{Resumable: false}}
	c1.msgs <- transcript("we were talking")
	next(t, out, nil)
	c1.msgs <- &genai.LiveServerMessage{GoAway: &genai.LiveServerGoAway{TimeLeft: 10 * time.Second}}
	if ev := next(t, out, nil); !ev.Final || ev.Text != "we were talking" {
		t.Errorf("rotation final = %+v", ev)
	}
	waitFor(t, "old connection closed", c1.isClosed)
	cfgs := d.configs()
	if len(cfgs) != 2 || cfgs[1].Handle != "h1" {
		t.Fatalf("dials = %+v, want the second to resume h1", cfgs)
	}
	in <- frame(0)
	waitFor(t, "audio on the new connection", func() bool { return c2.audioBytes() > 0 })
	c2.msgs <- transcript("about Go")
	if ev := next(t, out, nil); ev.Text != "about Go" || ev.SegmentID != "g000002" {
		t.Errorf("after rotation = %+v", ev)
	}
	close(in)
	waitFor(t, "stream end", c2.ended)
	c2.msgs <- turnComplete()
	evs := drain(t, out, nil)
	if len(evs) != 1 || !evs[0].Final {
		t.Errorf("tail = %+v", evs)
	}
}

func TestReconnect(t *testing.T) {
	cases := []struct {
		name  string
		conns []*fakeConn // after the first
		// wantErrs: error events; wantClosed: the stream ends by itself.
		wantErrs   int
		wantClosed bool
	}{
		{"reconnects", []*fakeConn{newFakeConn()}, 1, false},
		{"retries a failed dial", []*fakeConn{nil, newFakeConn()}, 1, false},
		{"gives up", []*fakeConn{nil, nil, nil}, 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c1 := newFakeConn()
			d := &fakeDialer{conns: append([]*fakeConn{c1}, tc.conns...)}
			a := testASR(d)
			in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: api.Auto})
			if err != nil {
				t.Fatal(err)
			}
			c1.msgs <- &genai.LiveServerMessage{SessionResumptionUpdate: &genai.LiveServerSessionResumptionUpdate{NewHandle: "h1", Resumable: true}}
			c1.msgs <- transcript("half a sentence")
			next(t, out, nil)
			c1.errs <- errors.New("websocket: close 1011")
			if ev := next(t, out, nil); !ev.Final || ev.Text != "half a sentence" {
				t.Errorf("after drop = %+v, want the pending text final", ev)
			}
			if ev := next(t, out, nil); ev.Err == nil || !strings.Contains(ev.Err.Error(), "1011") {
				t.Errorf("error event = %+v", ev)
			}
			if tc.wantClosed {
				evs := drain(t, out, nil)
				if len(evs) != tc.wantErrs-1 || evs[0].Err == nil || !strings.Contains(evs[0].Err.Error(), "giving up") {
					t.Errorf("tail = %+v", evs)
				}
				return
			}
			last := tc.conns[len(tc.conns)-1]
			in <- frame(0)
			waitFor(t, "audio on the new connection", func() bool { return last.audioBytes() > 0 })
			if cfgs := d.configs(); cfgs[1].Handle != "h1" {
				t.Errorf("reconnect handle = %q", cfgs[1].Handle)
			}
			close(in)
			waitFor(t, "stream end", last.ended)
			last.msgs <- turnComplete()
			drain(t, out, nil)
		})
	}
}

func TestBufferWhileDown(t *testing.T) {
	a := testASR(&fakeDialer{})
	o := a.withDefaults()
	o.MaxBuffer = 100 * time.Millisecond
	s := newStream(t.Context(), o, domain.ASRConfig{SessionID: "s1"}, dialConfig{})
	for i := range 10 {
		s.frame(frame(time.Duration(i) * domain.FrameDuration))
	}
	if s.bufDur != 100*time.Millisecond || len(s.buffered) != 5 {
		t.Errorf("buffered %v in %d frames", s.bufDur, len(s.buffered))
	}
	if s.dropped != 100*time.Millisecond || s.gapFrom != 0 || s.gapTo != 100*time.Millisecond {
		t.Errorf("gap %v..%v dropped %v", s.gapFrom, s.gapTo, s.dropped)
	}
	conn := newFakeConn()
	s.attach(conn)
	s.send()
	if conn.audioBytes() != 5*domain.FrameSamples*2 || s.buffered != nil {
		t.Errorf("flushed %d bytes, left %d frames", conn.audioBytes(), len(s.buffered))
	}
	close(s.done)
	_ = conn.Close()
}

func TestCancelClosesStream(t *testing.T) {
	conn := newFakeConn()
	a := testASR(&fakeDialer{conns: []*fakeConn{conn}})
	ctx, cancel := context.WithCancel(t.Context())
	_, out, err := a.Start(ctx, domain.ASRConfig{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	drain(t, out, nil)
	if !conn.isClosed() {
		t.Error("connection left open")
	}
}

func TestLiveConfig(t *testing.T) {
	cases := []struct {
		name     string
		dc       dialConfig
		modality genai.Modality
		prompt   []string
	}{
		{"native audio, auto", dialConfig{Model: DefaultLiveModel}, genai.ModalityAudio, []string{"English or Spanish", `"en"`, `"es"`}},
		{"text model, pinned", dialConfig{Model: "gemini-live-2.5-flash-preview", Language: "es", Handle: "h"}, genai.ModalityText, []string{"speaks Spanish"}},
		{"glossary", dialConfig{Model: "m", Glossary: []string{"Kubernetes", "gRPC"}}, genai.ModalityText, []string{"Kubernetes, gRPC"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := liveConfig(tc.dc)
			if len(cfg.ResponseModalities) != 1 || cfg.ResponseModalities[0] != tc.modality {
				t.Errorf("modalities = %v", cfg.ResponseModalities)
			}
			if cfg.InputAudioTranscription == nil || cfg.ContextWindowCompression == nil || cfg.ContextWindowCompression.SlidingWindow == nil {
				t.Error("transcription or context window compression missing")
			}
			if cfg.SessionResumption == nil || cfg.SessionResumption.Handle != tc.dc.Handle {
				t.Errorf("resumption = %+v", cfg.SessionResumption)
			}
			prompt := cfg.SystemInstruction.Parts[0].Text
			for _, p := range tc.prompt {
				if !strings.Contains(prompt, p) {
					t.Errorf("prompt %q lacks %q", prompt, p)
				}
			}
		})
	}
}
