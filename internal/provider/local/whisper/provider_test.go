// SPDX-License-Identifier: Apache-2.0

package whisper

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/backoff"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// call is one POST /inference the fake server received.
type call struct {
	language, prompt string
	audio            time.Duration
	silent           bool // all samples zero (the Start probe)
}

// fakeServer mimics whisper-server. respond answers the non-probe calls;
// probes get a multilingual answer unless englishOnly is set.
type fakeServer struct {
	t           *testing.T
	englishOnly bool
	health      int // status of GET /health; 0 is 200
	respond     func(c call) (int, any)

	mu    sync.Mutex
	calls []call
}

func (f *fakeServer) start() string {
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	f.t.Cleanup(srv.Close)
	return srv.URL
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		if f.health != 0 {
			w.WriteHeader(f.health)
			return
		}
		_, _ = io.WriteString(w, `{"status":"ok"}`)
		return
	case "/inference":
	default:
		http.NotFound(w, r)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil || r.FormValue("response_format") != "verbose_json" {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	data, _ := io.ReadAll(file)
	if len(data) < 44 || string(data[:4]) != "RIFF" || binary.LittleEndian.Uint32(data[24:]) != domain.SampleRate {
		http.Error(w, "bad wav", http.StatusBadRequest)
		return
	}
	pcm := data[44:]
	c := call{language: r.FormValue("language"), prompt: r.FormValue("prompt"), audio: samplesDur(len(pcm) / 2), silent: true}
	for _, b := range pcm {
		if b != 0 {
			c.silent = false
			break
		}
	}
	if c.silent && c.audio == time.Second { // the Start probe
		lang := "spanish"
		if f.englishOnly {
			lang = "english"
		}
		writeJSON(w, http.StatusOK, response{Language: lang, Segments: []segment{seg(" [BLANK_AUDIO]")}})
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	status, body := http.StatusOK, any(response{Language: "english", Segments: []segment{seg(" Hello.")}})
	if f.respond != nil {
		status, body = f.respond(c)
	}
	writeJSON(w, status, body)
}

func (f *fakeServer) seen() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if s, ok := v.(string); ok {
		_, _ = io.WriteString(w, s)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func settingsFor(url, model string) func(context.Context) (api.Settings, error) {
	return func(context.Context) (api.Settings, error) {
		var s api.Settings
		s.Providers.Local.WhisperUrl, s.Providers.Local.WhisperModel = url, model
		return s, nil
	}
}

// run streams audio through a provider and collects every event until the
// output closes.
func run(t *testing.T, p *Provider, cfg domain.ASRConfig, pcm []int16) []domain.ASREvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	in, out, err := p.Start(ctx, cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() {
		defer close(in)
		for _, f := range frames(pcm, 0) {
			select {
			case in <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	var evs []domain.ASREvent
	for ev := range out {
		evs = append(evs, ev)
	}
	if ctx.Err() != nil {
		t.Fatal("the stream didn't close")
	}
	return evs
}

// langResponse answers in language lang with the given probabilities.
func langResponse(lang, text string, probs map[string]float64) response {
	return response{Language: lang, DetectedLanguage: lang, LanguageProbabilities: probs, Segments: []segment{seg(text)}}
}

func TestStreamInterimsAndFinal(t *testing.T) {
	f := &fakeServer{t: t, respond: func(c call) (int, any) {
		// The text grows with the audio, like a real transcription.
		words := strings.Fields("hola a todos bienvenidos a la charla")
		n := min(len(words), int(c.audio/(400*ms)))
		return http.StatusOK, langResponse("spanish", " "+strings.Join(words[:n], " "), map[string]float64{"en": 0.2, "es": 0.7, "pt": 0.1})
	}}
	p := &Provider{Settings: settingsFor(f.start(), "large-v3-turbo")}
	evs := run(t, p, domain.ASRConfig{SessionID: "s1", SourceLanguage: api.Auto},
		synth(silence(500*ms), tone(2500*ms), silence(time.Second), tone(1500*ms), silence(time.Second)))

	var finals []domain.ASREvent
	interims := map[string]int{}
	for _, ev := range evs {
		if ev.Err != nil {
			t.Fatalf("error event: %v", ev.Err)
		}
		if ev.Lang != "es" {
			t.Errorf("%s: lang %q, want es", ev.SegmentID, ev.Lang)
		}
		if ev.Final {
			finals = append(finals, ev)
			if interims[ev.SegmentID] < 0 {
				t.Errorf("%s: two finals", ev.SegmentID)
			}
			interims[ev.SegmentID] = -1
		} else {
			if interims[ev.SegmentID] < 0 {
				t.Errorf("%s: interim after its final", ev.SegmentID)
			}
			interims[ev.SegmentID]++
		}
	}
	if len(finals) != 2 || finals[0].SegmentID != "w-000001" || finals[1].SegmentID != "w-000002" {
		t.Fatalf("finals %+v", finals)
	}
	if !near(finals[0].Start, 200*ms) || !near(finals[0].End, 3200*ms) {
		t.Errorf("first final covers %v–%v, want 0.2 s–3.2 s", finals[0].Start, finals[0].End)
	}
	if finals[0].Text != "hola a todos bienvenidos a la charla" || finals[0].Usage.AudioSeconds < 2.9 {
		t.Errorf("first final %+v", finals[0])
	}
	if n := len(evs) - len(finals); n == 0 {
		t.Error("no interim events")
	}
	// Auto detection asks with language=auto, except interims locked
	// after 2 s of an utterance.
	for _, c := range f.seen() {
		if c.language != "auto" && (c.language != "es" || c.audio >= 3*time.Second) {
			t.Errorf("request with language %q for %v of audio", c.language, c.audio)
		}
	}
}

func TestLanguagePick(t *testing.T) {
	tests := []struct {
		name     string
		source   api.SourceLanguage
		respond  func(c call) (int, any)
		want     domain.LanguageCode
		wantLang []string // languages of the requests, in order
	}{
		{
			name:   "english wins",
			source: api.Auto,
			respond: func(call) (int, any) {
				return 200, langResponse("english", " Hello everyone.", map[string]float64{"en": 0.8, "es": 0.1})
			},
			want: "en", wantLang: []string{"auto"},
		},
		{
			name:   "a third language is transcribed again in the likelier of en/es",
			source: api.Auto,
			respond: func(c call) (int, any) {
				if c.language == "es" {
					return 200, langResponse("spanish", " Hola a todos.", nil)
				}
				return 200, langResponse("portuguese", " Olá a todos.", map[string]float64{"pt": 0.6, "es": 0.3, "en": 0.05})
			},
			want: "es", wantLang: []string{"auto", "es"},
		},
		{
			name:   "no probabilities: the detected language",
			source: api.Auto,
			respond: func(call) (int, any) {
				return 200, response{Language: "spanish", DetectedLanguage: "spanish", Segments: []segment{seg(" Hola.")}}
			},
			want: "es", wantLang: []string{"auto"},
		},
		{
			name:   "pinned language skips detection",
			source: api.En,
			respond: func(c call) (int, any) {
				return 200, langResponse("english", " Hello.", nil)
			},
			want: "en", wantLang: []string{"en"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeServer{t: t, respond: tt.respond}
			p := &Provider{Settings: settingsFor(f.start(), ""), VAD: VADConfig{InterimEvery: time.Hour}}
			evs := run(t, p, domain.ASRConfig{SourceLanguage: tt.source}, synth(tone(1500*ms), silence(time.Second)))
			if len(evs) != 1 || !evs[0].Final || evs[0].Lang != tt.want {
				t.Fatalf("events %+v, want one final in %s", evs, tt.want)
			}
			var langs []string
			for _, c := range f.seen() {
				langs = append(langs, c.language)
			}
			if strings.Join(langs, ",") != strings.Join(tt.wantLang, ",") {
				t.Errorf("request languages %v, want %v", langs, tt.wantLang)
			}
		})
	}
}

func TestHallucinationsAreDropped(t *testing.T) {
	for _, text := range []string{" [Música]", " Subtítulos realizados por la comunidad de Amara.org", " Thanks for watching!"} {
		f := &fakeServer{t: t, respond: func(call) (int, any) {
			return 200, langResponse("spanish", text, map[string]float64{"es": 0.9})
		}}
		p := &Provider{Settings: settingsFor(f.start(), "")}
		if evs := run(t, p, domain.ASRConfig{}, synth(tone(2500*ms), silence(time.Second))); len(evs) != 0 {
			t.Errorf("%q: events %+v", text, evs)
		}
	}
}

func TestSilenceNeverReachesTheServer(t *testing.T) {
	f := &fakeServer{t: t}
	p := &Provider{Settings: settingsFor(f.start(), "")}
	evs := run(t, p, domain.ASRConfig{}, synth(silence(5*time.Second), part{3 * time.Second, -65}))
	if len(evs) != 0 || len(f.seen()) != 0 {
		t.Errorf("%d events, %d requests for silence", len(evs), len(f.seen()))
	}
}

func TestPrompt(t *testing.T) {
	f := &fakeServer{t: t}
	p := &Provider{Settings: settingsFor(f.start(), ""), VAD: VADConfig{InterimEvery: time.Hour}}
	g := &domain.Glossary{Terms: []api.GlossaryTerm{{Term: "Kubernetes"}, {Term: "kubernetes"}}, DoNotTranslate: []string{"Nerdearla"}}
	run(t, p, domain.ASRConfig{SourceLanguage: api.En, Glossary: g}, synth(tone(time.Second), silence(time.Second)))
	if c := f.seen(); len(c) != 1 || c[0].prompt != "Kubernetes, Nerdearla." {
		t.Errorf("calls %+v", c)
	}
}

func TestServerErrors(t *testing.T) {
	t.Run("a failing final is retried once", func(t *testing.T) {
		var mu sync.Mutex
		n := 0
		f := &fakeServer{t: t, respond: func(call) (int, any) {
			mu.Lock()
			defer mu.Unlock()
			if n++; n == 1 {
				return http.StatusInternalServerError, `{"error":"boom"}`
			}
			return 200, langResponse("english", " Hello.", map[string]float64{"en": 0.9})
		}}
		p := &Provider{Settings: settingsFor(f.start(), ""), VAD: VADConfig{InterimEvery: time.Hour}}
		evs := run(t, p, domain.ASRConfig{}, synth(tone(time.Second), silence(time.Second)))
		if len(evs) != 1 || evs[0].Err != nil || evs[0].Text != "Hello." {
			t.Errorf("events %+v", evs)
		}
	})
	t.Run("a lost final keeps the interim text", func(t *testing.T) {
		f := &fakeServer{t: t, respond: func(c call) (int, any) {
			if c.audio >= 2*time.Second {
				return http.StatusInternalServerError, "boom"
			}
			return 200, langResponse("english", " Hello.", map[string]float64{"en": 0.9})
		}}
		p := &Provider{Settings: settingsFor(f.start(), ""), FinalBackoff: fastRetry}
		evs := run(t, p, domain.ASRConfig{}, synth(tone(2500*ms), silence(time.Second), tone(time.Second), silence(time.Second)))
		var errs, finals int
		for _, ev := range evs {
			switch {
			case ev.Err != nil:
				errs++
			case ev.Final:
				finals++
				if ev.Text != "Hello." || ev.Lang != "en" {
					t.Errorf("final %+v", ev)
				}
			}
		}
		// The stream went on to the second utterance.
		if errs != 1 || finals != 2 {
			t.Errorf("%d errors, %d finals: %+v", errs, finals, evs)
		}
	})
	t.Run("a failed interim is skipped", func(t *testing.T) {
		f := &fakeServer{t: t, respond: func(c call) (int, any) {
			if c.audio < 2*time.Second {
				return http.StatusInternalServerError, "boom"
			}
			return 200, langResponse("english", " Hello there.", map[string]float64{"en": 0.9})
		}}
		p := &Provider{Settings: settingsFor(f.start(), "")}
		evs := run(t, p, domain.ASRConfig{}, synth(tone(2500*ms), silence(time.Second)))
		last := evs[len(evs)-1]
		for _, ev := range evs {
			if ev.Err != nil {
				t.Errorf("error event for an interim: %v", ev.Err)
			}
		}
		if !last.Final || last.Text != "Hello there." {
			t.Errorf("events %+v", evs)
		}
	})
}

// fastRetry keeps the final retries of the tests short.
var fastRetry = backoff.Policy{Base: time.Millisecond, Max: 2 * time.Millisecond}

// whisper-server restarting (connection errors, then 503 while it loads
// its model) doesn't lose the final that was in flight.
func TestFinalRetries(t *testing.T) {
	tests := []struct {
		name      string
		failures  int
		status    int
		retries   int
		wantCalls int
		wantErr   bool
	}{
		{"no failure", 0, 0, 3, 1, false},
		{"loading after a restart", 4, http.StatusServiceUnavailable, 6, 5, false},
		{"server error", 2, http.StatusInternalServerError, 3, 3, false},
		{"down for longer than the retries", 100, http.StatusServiceUnavailable, 2, 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			n := 0
			f := &fakeServer{t: t, respond: func(call) (int, any) {
				mu.Lock()
				defer mu.Unlock()
				if n++; n <= tt.failures {
					return tt.status, `{"error":"model loading"}`
				}
				return 200, langResponse("english", " Hello.", map[string]float64{"en": 0.9})
			}}
			p := &Provider{Settings: settingsFor(f.start(), ""), VAD: VADConfig{InterimEvery: time.Hour},
				FinalRetries: tt.retries, FinalBackoff: fastRetry}
			evs := run(t, p, domain.ASRConfig{}, synth(tone(time.Second), silence(time.Second)))
			if got := len(f.seen()); got != tt.wantCalls {
				t.Errorf("%d inference calls, want %d", got, tt.wantCalls)
			}
			if tt.wantErr {
				if len(evs) != 1 || evs[0].Err == nil {
					t.Errorf("events %+v, want one error", evs)
				}
				return
			}
			if len(evs) != 1 || evs[0].Err != nil || !evs[0].Final || evs[0].Text != "Hello." {
				t.Errorf("events %+v, want the final", evs)
			}
		})
	}
}

func TestStartRefuses(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		server    *fakeServer
		url       string
		wantCoded string // "" means a plain (provider.unavailable) error
	}{
		{name: "english-only model name", model: "base.en", server: &fakeServer{}, wantCoded: CodeEnglishOnlyModel},
		{name: "english-only model on the server", model: "large-v3-turbo", server: &fakeServer{englishOnly: true}, wantCoded: CodeEnglishOnlyModel},
		{name: "unreachable", model: "small", url: "http://127.0.0.1:1"},
		{name: "loading", model: "small", server: &fakeServer{health: http.StatusServiceUnavailable}},
		{name: "server error", model: "small", server: &fakeServer{health: http.StatusInternalServerError}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.url
			if tt.server != nil {
				tt.server.t = t
				url = tt.server.start()
			}
			p := &Provider{Settings: settingsFor(url, tt.model)}
			_, _, err := p.Start(t.Context(), domain.ASRConfig{})
			if err == nil {
				t.Fatal("started")
			}
			var coded *domain.CodedError
			switch {
			case tt.wantCoded == "" && errors.As(err, &coded):
				t.Errorf("coded error %v, want a plain one", coded.Code)
			case tt.wantCoded != "" && (!errors.As(err, &coded) || coded.Code != tt.wantCoded || coded.Params["model"] != tt.model):
				t.Errorf("error %v, want code %s", err, tt.wantCoded)
			}
		})
	}
}

func TestConfig(t *testing.T) {
	tests := []struct {
		name     string
		settings func(context.Context) (api.Settings, error)
		want     config
		wantErr  bool
	}{
		{"nil settings", nil, config{DefaultURL, DefaultModel}, false},
		{"not stored yet", func(context.Context) (api.Settings, error) { return api.Settings{}, domain.ErrNotFound }, config{DefaultURL, DefaultModel}, false},
		{"empty fields", settingsFor("", " "), config{DefaultURL, DefaultModel}, false},
		{"configured", settingsFor("http://gpu-box:8178/", "medium"), config{"http://gpu-box:8178", "medium"}, false},
		{"store error", func(context.Context) (api.Settings, error) { return api.Settings{}, errors.New("disk") }, config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&Provider{Settings: tt.settings}).config(t.Context())
			if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
				t.Errorf("config = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestCancelClosesTheStream(t *testing.T) {
	f := &fakeServer{t: t}
	p := &Provider{Settings: settingsFor(f.start(), "")}
	ctx, cancel := context.WithCancel(t.Context())
	in, out, err := p.Start(ctx, domain.ASRConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, fr := range frames(synth(tone(time.Second)), 0) {
		in <- fr
	}
	cancel()
	select {
	case <-drainAll(out):
	case <-time.After(5 * time.Second):
		t.Fatal("output not closed after cancel")
	}
}

func drainAll(ch <-chan domain.ASREvent) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	return done
}

func TestWAV(t *testing.T) {
	b := wav([]int16{1, -2, 32767})
	if len(b) != 44+6 || string(b[:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || string(b[36:40]) != "data" {
		t.Fatalf("header % x", b[:44])
	}
	if got := int16(binary.LittleEndian.Uint16(b[46:])); got != -2 {
		t.Errorf("sample 1 = %d", got)
	}
}
