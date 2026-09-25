// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
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
	receiving bool // the stream reads it
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
	c.mu.Lock()
	c.receiving = true
	c.mu.Unlock()
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

func (c *fakeConn) read() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.receiving
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

// fakeClock is the wall clock plus an offset the test advances.
type fakeClock struct {
	mu  sync.Mutex
	off time.Duration
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.off)
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.off += d
}

// testASR never flushes, rotates or times out by itself unless a test sets
// a short value or advances the clock: rotation is due after 1 h and forced
// 30 min later; the safety flushes are 100 h out.
func testASR(d *fakeDialer) *ASR {
	return &ASR{
		APIKey:    func(context.Context) (string, error) { return "test-key", nil },
		SendEvery: 20 * time.Millisecond, IdleFinal: 100 * time.Hour, TurnGrace: 100 * time.Hour,
		RotateAfter: time.Hour, RotateGrace: 30 * time.Minute, MaxRetries: 2,
		Backoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond, DrainTimeout: 300 * time.Millisecond,
		dial: d.dial, tick: 5 * time.Millisecond,
	}
}

func frame(t time.Duration) domain.AudioFrame {
	return domain.AudioFrame{PCM: make([]int16, domain.FrameSamples), T: t}
}

func interim(text string) *genai.LiveServerMessage {
	return &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{InterimInputTranscription: &genai.Transcription{Text: text}}}
}

func final(text string) *genai.LiveServerMessage {
	return &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{InputTranscription: &genai.Transcription{Text: text, Finished: true}}}
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

// end closes the input and lets the last connection finish.
func end(t *testing.T, in chan<- domain.AudioFrame, out <-chan domain.ASREvent, last *fakeConn) []domain.ASREvent {
	t.Helper()
	close(in)
	waitFor(t, "stream end", last.ended)
	last.msgs <- turnComplete()
	return drain(t, out, nil)
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
	var many []api.GlossaryTerm
	var manyWant []string
	for i := range maxVocabulary + 20 {
		many = append(many, api.GlossaryTerm{Term: fmt.Sprintf("term%d", i)})
		if i < maxVocabulary {
			manyWant = append(manyWant, fmt.Sprintf("term%d", i))
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
		{"vocabulary capped", nil, api.Auto, &domain.Glossary{Terms: many, DoNotTranslate: []string{"late"}}, DefaultLiveModel, "", manyWant},
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
			if dc.Model != tc.wantModel || dc.Language != tc.wantLang || dc.APIKey != "test-key" {
				t.Errorf("dial config = %+v", dc)
			}
			if !slices.Equal(dc.Vocabulary, tc.wantTerms) {
				t.Errorf("vocabulary = %q, want %q", dc.Vocabulary, tc.wantTerms)
			}
		})
	}
}

func TestLiveConfig(t *testing.T) {
	cases := []struct {
		name      string
		dc        dialConfig
		wantCodes []string
	}{
		{"auto detects", dialConfig{Model: DefaultLiveModel}, nil},
		{"pinned English", dialConfig{Model: DefaultLiveModel, Language: "en"}, []string{"en-US"}},
		{"pinned Spanish", dialConfig{Model: DefaultLiveModel, Language: "es", Vocabulary: []string{"Kubernetes", "gRPC"}}, []string{"es-419"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := liveConfig(tc.dc)
			if !slices.Equal(cfg.ResponseModalities, []genai.Modality{genai.ModalityText}) {
				t.Errorf("modalities = %v", cfg.ResponseModalities)
			}
			tr := cfg.InputAudioTranscription
			if tr == nil {
				t.Fatal("no input transcription")
			}
			if !slices.Equal(tr.LanguageCodes, tc.wantCodes) || !slices.Equal(tr.CustomVocabulary, tc.dc.Vocabulary) ||
				tr.Mode != genai.AudioTranscriptionConfigModeSmart {
				t.Errorf("transcription = %+v", tr)
			}
			if cfg.SystemInstruction != nil || cfg.SessionResumption != nil || cfg.ContextWindowCompression != nil || cfg.OutputAudioTranscription != nil {
				t.Errorf("setup has fields the transcription model doesn't take: %+v", cfg)
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
	conn.msgs <- interim(" Hello and")
	ev := next(t, out, &usage)
	if ev.Final || ev.Text != "Hello and" || ev.Lang != "en" || ev.SegmentID != "g000001" {
		t.Errorf("interim = %+v", ev)
	}
	if ev.End != 11*time.Second || ev.Start < base || ev.Start >= ev.End {
		t.Errorf("interim times = %v..%v", ev.Start, ev.End)
	}
	conn.msgs <- interim("Hello and welcome\n")
	conn.msgs <- interim("Hello and  welcome") // unchanged: no event
	if ev2 := next(t, out, &usage); ev2.Final || ev2.Text != "Hello and welcome" || ev2.SegmentID != "g000001" || ev2.Start != ev.Start {
		t.Errorf("replaced interim = %+v", ev2)
	}
	conn.msgs <- &genai.LiveServerMessage{UsageMetadata: &genai.UsageMetadata{PromptTokenCount: 120, ResponseTokenCount: 7}}
	conn.msgs <- final("Hello and welcome to the conference.\n\nToday we talk about Go.")
	first := next(t, out, &usage)
	if !first.Final || first.Text != "Hello and welcome to the conference." || first.SegmentID != "g000001" || first.Start != ev.Start {
		t.Errorf("first final = %+v", first)
	}
	second := next(t, out, &usage)
	if !second.Final || second.Text != "Today we talk about Go." || second.SegmentID != "g000002" ||
		second.Start != first.End || second.End != 11*time.Second || first.End <= first.Start {
		t.Errorf("second final = %+v (first %v..%v)", second, first.Start, first.End)
	}
	conn.msgs <- interim("Next")
	if ev := next(t, out, &usage); ev.SegmentID != "g000003" || ev.Start < second.End || ev.Final {
		t.Errorf("next interim = %+v", ev)
	}

	close(in)
	waitFor(t, "stream end", conn.ended)
	conn.msgs <- turnComplete()
	tail := drain(t, out, &usage)
	if len(tail) != 1 || !tail[0].Final || tail[0].Text != "Next" || tail[0].SegmentID != "g000003" {
		t.Errorf("tail = %+v, want the open interim final", tail)
	}
	if math.Abs(usage.AudioSeconds-1) > 1e-9 || usage.InputTokens != 0 || usage.OutputTokens != 7 {
		t.Errorf("usage = %+v, want 1 s of audio and the 7 output tokens", usage)
	}
	if !conn.isClosed() {
		t.Error("connection left open")
	}
}

func TestLanguage(t *testing.T) {
	withCode := func(m *genai.LiveServerMessage, code string) *genai.LiveServerMessage {
		if t := m.ServerContent.InputTranscription; t != nil {
			t.LanguageCode = code
		}
		if t := m.ServerContent.InterimInputTranscription; t != nil {
			t.LanguageCode = code
		}
		return m
	}
	cases := []struct {
		name   string
		source api.SourceLanguage
		msgs   []*genai.LiveServerMessage // the last final is checked
		want   domain.LanguageCode
	}{
		{"pinned wins over the words", api.Es, []*genai.LiveServerMessage{final("the talk is about the cloud")}, "es"},
		{"API code on the final", api.Auto, []*genai.LiveServerMessage{withCode(final("the talk"), "es-419")}, "es"},
		{"API code on the interim", api.Auto, []*genai.LiveServerMessage{withCode(interim("the"), "es-US"), final("the talk")}, "es"},
		{"words: Spanish", api.Auto, []*genai.LiveServerMessage{final("¿Qué es la nube y cómo funciona?")}, "es"},
		{"words: English", api.Auto, []*genai.LiveServerMessage{final("What is the cloud and how does it work?")}, "en"},
		{"previous segment when unclear", api.Auto, []*genai.LiveServerMessage{final("¿Qué tal, cómo están?"), final("Kubernetes")}, "es"},
		{"unknown", api.Auto, []*genai.LiveServerMessage{final("Kubernetes")}, ""},
		{"other language ignored", api.Auto, []*genai.LiveServerMessage{withCode(final("the cloud and the edge"), "fr-FR")}, "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := newFakeConn()
			a := testASR(&fakeDialer{conns: []*fakeConn{conn}})
			in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: tc.source})
			if err != nil {
				t.Fatal(err)
			}
			finals := 0
			for _, m := range tc.msgs {
				if m.ServerContent.InputTranscription != nil {
					finals++
				}
				conn.msgs <- m
			}
			var ev domain.ASREvent
			for finals > 0 {
				if ev = next(t, out, nil); ev.Final {
					finals--
				}
			}
			if ev.Lang != tc.want {
				t.Errorf("lang = %q, want %q", ev.Lang, tc.want)
			}
			end(t, in, out, conn)
		})
	}
}

func TestSafetyFlush(t *testing.T) {
	cases := []struct {
		name string
		tune func(a *ASR)
		// after the interim and before the flush
		msgs []*genai.LiveServerMessage
	}{
		{"no update for IdleFinal", func(a *ASR) { a.IdleFinal = 30 * time.Millisecond }, nil},
		{"turn complete without a final", func(a *ASR) { a.TurnGrace = 30 * time.Millisecond }, []*genai.LiveServerMessage{turnComplete()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := newFakeConn()
			a := testASR(&fakeDialer{conns: []*fakeConn{conn}})
			tc.tune(a)
			in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: api.Auto})
			if err != nil {
				t.Fatal(err)
			}
			conn.msgs <- interim("a sentence with no end")
			if ev := next(t, out, nil); ev.Final {
				t.Fatalf("first event is final: %+v", ev)
			}
			for _, m := range tc.msgs {
				conn.msgs <- m
			}
			if ev := next(t, out, nil); !ev.Final || ev.Text != "a sentence with no end" || ev.SegmentID != "g000001" {
				t.Errorf("flushed = %+v", ev)
			}
			// The server's late final of the flushed utterance isn't a new caption.
			conn.msgs <- final("A sentence with no end.")
			conn.msgs <- interim("next words")
			if ev := next(t, out, nil); ev.Final || ev.Text != "next words" || ev.SegmentID != "g000002" {
				t.Errorf("after the late final = %+v", ev)
			}
			end(t, in, out, conn)
		})
	}
}

func TestRotation(t *testing.T) {
	cases := []struct {
		name string
		// trigger starts the rotation while "we were talking" is open.
		trigger func(clk *fakeClock, c1 *fakeConn)
		// force: the switch happens mid-utterance, without waiting for a pause.
		force bool
	}{
		{"at a pause", func(clk *fakeClock, _ *fakeConn) { clk.advance(time.Hour) }, false},
		{"forced mid-utterance", func(clk *fakeClock, _ *fakeConn) { clk.advance(time.Hour) }, true},
		{"go-away", func(_ *fakeClock, c1 *fakeConn) {
			c1.msgs <- &genai.LiveServerMessage{GoAway: &genai.LiveServerGoAway{TimeLeft: 500 * time.Millisecond}}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c1, c2 := newFakeConn(), newFakeConn()
			d := &fakeDialer{conns: []*fakeConn{c1, c2}}
			clk := &fakeClock{}
			a := testASR(d)
			a.now = clk.now
			a.DrainTimeout = time.Second // room for the old connection's last final
			in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: api.En})
			if err != nil {
				t.Fatal(err)
			}
			in <- frame(0)
			waitFor(t, "audio on the first connection", func() bool { return c1.audioBytes() > 0 })
			c1.msgs <- interim("we were talking")
			next(t, out, nil)

			tc.trigger(clk, c1)
			waitFor(t, "the next connection", c2.read)
			if tc.name == "forced mid-utterance" {
				clk.advance(30 * time.Minute) // RotateGrace is over
			}
			if !tc.force {
				time.Sleep(20 * time.Millisecond)
				if c1.ended() {
					t.Fatal("switched mid-utterance")
				}
				c1.msgs <- final("we were talking about Go.")
				if ev := next(t, out, nil); !ev.Final || ev.SegmentID != "g000001" {
					t.Errorf("final before the switch = %+v", ev)
				}
			}
			waitFor(t, "the switch", c1.ended)
			before := c1.audioBytes()
			in <- frame(domain.FrameDuration)
			waitFor(t, "audio on the next connection", func() bool { return c2.audioBytes() > 0 })
			if c1.audioBytes() != before {
				t.Error("audio went to both connections")
			}
			if tc.force {
				// The old connection still delivers its last utterance.
				c1.msgs <- final("we were talking about Go.")
				if ev := next(t, out, nil); !ev.Final || ev.Text != "we were talking about Go." || ev.SegmentID != "g000001" {
					t.Errorf("drained final = %+v", ev)
				}
			}
			waitFor(t, "old connection closed", c1.isClosed) // after DrainTimeout
			c2.msgs <- interim("and now")
			if ev := next(t, out, nil); ev.Text != "and now" || ev.SegmentID != "g000002" || ev.Final {
				t.Errorf("after rotation = %+v", ev)
			}
			tail := end(t, in, out, c2)
			if len(tail) != 1 || tail[0].Text != "and now" || !tail[0].Final {
				t.Errorf("tail = %+v", tail)
			}
			if n := len(d.configs()); n != 2 {
				t.Errorf("%d dials, want 2", n)
			}
		})
	}
}

func TestRotationConnectionLostTakesOver(t *testing.T) {
	c1, c2 := newFakeConn(), newFakeConn()
	clk := &fakeClock{}
	a := testASR(&fakeDialer{conns: []*fakeConn{c1, c2}})
	a.now = clk.now
	in, out, err := a.Start(t.Context(), domain.ASRConfig{SessionID: "s1", SourceLanguage: api.En})
	if err != nil {
		t.Fatal(err)
	}
	c1.msgs <- interim("half a sentence")
	next(t, out, nil)
	clk.advance(time.Hour)
	waitFor(t, "the next connection", c2.read)
	c1.errs <- errors.New("websocket: close 1011")
	if ev := next(t, out, nil); !ev.Final || ev.Text != "half a sentence" {
		t.Errorf("after drop = %+v, want the pending text final", ev)
	}
	in <- frame(0)
	waitFor(t, "audio on the next connection", func() bool { return c2.audioBytes() > 0 })
	c2.msgs <- interim("the rest")
	if ev := next(t, out, nil); ev.Err != nil || ev.Text != "the rest" {
		t.Errorf("event = %+v, want no error: the next connection was ready", ev)
	}
	end(t, in, out, c2)
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
			c1.msgs <- interim("half a sentence")
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
			last.msgs <- interim("more")
			if ev := next(t, out, nil); ev.Text != "more" || ev.SegmentID != "g000002" {
				t.Errorf("after reconnect = %+v", ev)
			}
			end(t, in, out, last)
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
	s.attach(s.newConn(conn))
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
