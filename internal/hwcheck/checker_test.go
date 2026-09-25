// SPDX-License-Identifier: Apache-2.0

package hwcheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fakeClock advances by step on every Now.
type fakeClock struct {
	mu   sync.Mutex
	t    time.Time
	step time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(c.step)
	return c.t
}

// fakeASR emits one final per second of audio it receives, or fails.
type fakeASR struct {
	startErr error
	eventErr error
	silent   bool
	block    chan struct{} // if set, Start waits on it
	frames   int
}

func (f *fakeASR) Kind() domain.ProviderKind { return api.ProviderKindLocal }

func (f *fakeASR) Start(ctx context.Context, _ domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	if f.block != nil {
		<-f.block
	}
	if f.startErr != nil {
		return nil, nil, f.startErr
	}
	in := make(chan domain.AudioFrame)
	out := make(chan domain.ASREvent)
	go func() {
		defer close(out)
		var n int
		for {
			select {
			case _, ok := <-in:
				if !ok {
					if f.eventErr != nil {
						out <- domain.ASREvent{Err: f.eventErr}
					}
					f.frames = n
					return
				}
				n++
				if n%50 == 0 && !f.silent {
					lang := domain.LanguageCode("en")
					if n%100 == 0 {
						lang = "es"
					}
					out <- domain.ASREvent{SegmentID: "s", Final: true, Text: "hola", Lang: lang}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return in, out, nil
}

type fakeTranslator struct {
	mu      sync.Mutex
	calls   []domain.TranslateRequest
	err     error
	warmErr error
	warmed  bool
}

func (f *fakeTranslator) Kind() domain.ProviderKind { return api.ProviderKindLocal }

func (f *fakeTranslator) Translate(_ context.Context, r domain.TranslateRequest) (domain.TranslateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r)
	return domain.TranslateResult{Text: "x"}, f.err
}

func (f *fakeTranslator) Warm(context.Context) error { f.warmed = true; return f.warmErr }

// noNet fails every request, so tests never reach real sidecars.
var noNet = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
	return nil, errors.New("no network in tests")
})}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClip(t *testing.T) {
	clip, err := Clip()
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Duration(len(clip)) * time.Second / domain.SampleRate; d < minClip || d > minClip+20*time.Second {
		t.Errorf("clip is %v, want about %v", d, minClip)
	}
}

func TestDecodeWAV(t *testing.T) {
	good, err := clipFS.ReadFile("clip/en.wav")
	if err != nil {
		t.Fatal(err)
	}
	stereo := append([]byte(nil), good...)
	stereo[22] = 2
	for _, c := range []struct {
		name    string
		b       []byte
		wantErr string
	}{
		{"fixture", good, ""},
		{"not wav", []byte("hello world, not audio"), "not a WAV"},
		{"stereo", stereo, "want 16 kHz mono"},
		{"no data", good[:36], "no data chunk"},
	} {
		t.Run(c.name, func(t *testing.T) {
			pcm, err := decodeWAV(c.b)
			if c.wantErr == "" {
				if err != nil || len(pcm) < domain.SampleRate {
					t.Fatalf("decodeWAV = %d samples, %v", len(pcm), err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want %q", err, c.wantErr)
			}
		})
	}
}

func TestBenchmark(t *testing.T) {
	clip := make([]int16, 10*domain.SampleRate) // 10 s, 500 frames
	coded := &domain.CodedError{Code: "provider.model_english_only", Message: "en only"}
	for _, c := range []struct {
		name     string
		asr      *fakeASR
		tr       *fakeTranslator
		step     time.Duration // per clock read
		wantCode string
		wantOK   bool
	}{
		{name: "fast", asr: &fakeASR{}, tr: &fakeTranslator{}, step: time.Second, wantOK: true},
		{name: "slow", asr: &fakeASR{}, tr: &fakeTranslator{}, step: 5 * time.Second, wantOK: false},
		{name: "whisper down", asr: &fakeASR{startErr: errors.New("connection refused")}, tr: &fakeTranslator{},
			step: time.Second, wantCode: CodeBenchmarkUnavailable},
		{name: "english-only model keeps its code", asr: &fakeASR{startErr: coded}, tr: &fakeTranslator{},
			step: time.Second, wantCode: "provider.model_english_only"},
		{name: "ollama down", asr: &fakeASR{}, tr: &fakeTranslator{warmErr: errors.New("refused")},
			step: time.Second, wantCode: CodeBenchmarkUnavailable},
		{name: "asr error", asr: &fakeASR{eventErr: errors.New("500")}, tr: &fakeTranslator{},
			step: time.Second, wantCode: CodeBenchmarkFailed},
		{name: "no speech", asr: &fakeASR{silent: true}, tr: &fakeTranslator{},
			step: time.Second, wantCode: CodeBenchmarkNoSpeech},
		{name: "translation error", asr: &fakeASR{}, tr: &fakeTranslator{err: errors.New("boom")},
			step: time.Second, wantCode: CodeBenchmarkFailed},
	} {
		t.Run(c.name, func(t *testing.T) {
			clock := &fakeClock{t: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), step: c.step}
			res, err := benchmark(t.Context(), c.asr, c.tr, clip, clock.Now)
			if c.wantCode != "" {
				var ce *domain.CodedError
				if !errors.As(err, &ce) || ce.Code != c.wantCode {
					t.Fatalf("err = %v, want code %s", err, c.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// Two clock reads per stage: asr and translation take one step each.
			wantRTF := float32(2 * c.step.Seconds() / 10)
			if res.RealTimeFactor != wantRTF || res.AsrMs != int(c.step.Milliseconds()) || res.TranslationMs != int(c.step.Milliseconds()) {
				t.Errorf("result = %+v, want rtf %v", res, wantRTF)
			}
			if res.Ok != c.wantOK || *res.AudioMs != 10000 || *res.MaxRealTimeFactor != MaxRealTimeFactor {
				t.Errorf("result = %+v, want ok %v", res, c.wantOK)
			}
			if c.asr.frames != 500 || !c.tr.warmed {
				t.Errorf("fed %d frames (want 500), warmed %v", c.asr.frames, c.tr.warmed)
			}
			// 10 finals, alternating en/es; each goes to the other language.
			if len(c.tr.calls) != 10 {
				t.Fatalf("%d translations, want 10", len(c.tr.calls))
			}
			for _, r := range c.tr.calls {
				if r.From == r.To || !r.Final {
					t.Errorf("translation %+v", r)
				}
			}
		})
	}
}

func TestCheckerBenchmarkBusyAndPersisted(t *testing.T) {
	dir := t.TempDir()
	block := make(chan struct{})
	asr := &fakeASR{block: block}
	c := New(Options{ASR: asr, Translator: &fakeTranslator{}, DataDir: dir, FFmpeg: &ffmpeg.Prober{Binary: "/nonexistent"}, HTTPClient: noNet})
	done := make(chan error)
	go func() {
		_, err := c.Benchmark(t.Context())
		done <- err
	}()
	// Wait for the first run to hold the lock.
	for c.bench.TryLock() {
		c.bench.Unlock()
		time.Sleep(time.Millisecond)
	}
	if _, err := c.Benchmark(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second benchmark: %v, want ErrBusy", err)
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rep := c.Report(t.Context())
	if rep.LastBenchmark == nil || *rep.LastBenchmark.WhisperModel != "large-v3-turbo" || *rep.LastBenchmark.GemmaModel != "gemma3:4b" {
		t.Fatalf("lastBenchmark = %+v", rep.LastBenchmark)
	}
	// A new checker reads it back, and it decides localRealtimeLikely.
	c2 := New(Options{DataDir: dir, FFmpeg: &ffmpeg.Prober{Binary: "/nonexistent"}, HTTPClient: noNet})
	rep2 := c2.Report(t.Context())
	if rep2.LastBenchmark == nil || rep2.LastBenchmark.RealTimeFactor != rep.LastBenchmark.RealTimeFactor {
		t.Fatalf("reloaded lastBenchmark = %+v", rep2.LastBenchmark)
	}
	if rep2.Recommendation.LocalRealtimeLikely != rep.LastBenchmark.Ok {
		t.Errorf("localRealtimeLikely = %v, want the benchmark's ok %v", rep2.Recommendation.LocalRealtimeLikely, rep.LastBenchmark.Ok)
	}
}

func TestCheckerBenchmarkNotConfigured(t *testing.T) {
	c := New(Options{})
	var ce *domain.CodedError
	if _, err := c.Benchmark(t.Context()); !errors.As(err, &ce) || ce.Code != CodeBenchmarkUnavailable {
		t.Fatalf("err = %v", err)
	}
}

func TestRuntimes(t *testing.T) {
	whisperUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
		}
	}))
	defer whisperUp.Close()
	whisperLoading := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer whisperLoading.Close()
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = w.Write([]byte(`{"version":"0.34.1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ollama.Close()
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close() // nothing listens there any more

	for _, c := range []struct {
		name                    string
		whisperURL, ollamaURL   string
		wantWhisper, wantOllama bool
		wantWhisperDetail       string
		wantOllamaVersion       string
	}{
		{"both up", whisperUp.URL + "/", ollama.URL, true, true, "", "0.34.1"},
		{"whisper loading", whisperLoading.URL, ollama.URL, true, true, "loading", "0.34.1"},
		{"both down", down.URL, down.URL, false, false, "not reachable", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			settings := func(context.Context) (api.Settings, error) {
				var s api.Settings
				s.Providers.Local.WhisperUrl, s.Providers.Local.OllamaUrl = c.whisperURL, c.ollamaURL
				return s, nil
			}
			ch := New(Options{Settings: settings, FFmpeg: &ffmpeg.Prober{Binary: "/nonexistent/ffmpeg"}})
			r := ch.Runtimes(t.Context())
			if r.Whisper.Reachable != c.wantWhisper || r.Ollama.Reachable != c.wantOllama {
				t.Fatalf("runtimes = %+v", r)
			}
			if c.wantWhisperDetail != "" && (r.Whisper.Detail == nil || !strings.Contains(*r.Whisper.Detail, c.wantWhisperDetail)) {
				t.Errorf("whisper detail = %v, want %q", r.Whisper.Detail, c.wantWhisperDetail)
			}
			if c.wantOllamaVersion != "" && (r.Ollama.Version == nil || *r.Ollama.Version != c.wantOllamaVersion) {
				t.Errorf("ollama version = %v", r.Ollama.Version)
			}
			if strings.HasSuffix(*r.Whisper.Url, "/") {
				t.Errorf("whisper url %q keeps its trailing slash", *r.Whisper.Url)
			}
			if r.FFmpeg.Reachable || r.SRT {
				t.Errorf("ffmpeg = %+v, SRT %v with a missing binary", r.FFmpeg, r.SRT)
			}
		})
	}
}

func TestReportShape(t *testing.T) {
	c := New(Options{FFmpeg: &ffmpeg.Prober{Binary: "/nonexistent"}, HTTPClient: noNet})
	c.sys = fakeSystem("linux", "amd64", 8, map[string]string{"nvidia-smi": "NVIDIA A10, 23028\n"},
		map[string]string{"/proc/meminfo": "MemTotal: 33554432 kB\n"})
	rep := c.Report(t.Context())
	if rep.Os != "linux" || rep.Cpu.Cores != 8 || rep.MemoryBytes != 32*gib || len(rep.Gpus) != 1 ||
		rep.Gpus[0].Backend != api.Cuda || *rep.Gpus[0].MemoryBytes != 23028<<20 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Recommendation.WhisperModel != "large-v3-turbo" || rep.Recommendation.GemmaModel != "gemma3:4b" || !rep.Recommendation.LocalRealtimeLikely {
		t.Errorf("recommendation = %+v", rep.Recommendation)
	}
	if rep.LastBenchmark != nil {
		t.Errorf("lastBenchmark = %+v without a benchmark", rep.LastBenchmark)
	}
	if rep.ModelsDir != nil {
		t.Errorf("modelsDir = %q without a models directory", *rep.ModelsDir)
	}
}

func TestReportModelsDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	abs := t.TempDir()
	tests := []struct{ name, dir, want string }{
		{"relative is made absolute", "models", filepath.Join(cwd, "models")},
		{"absolute is kept", abs, abs},
		{"cleaned", abs + "/sub/../", abs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(Options{ModelsDir: tt.dir, FFmpeg: &ffmpeg.Prober{Binary: "/nonexistent"}, HTTPClient: noNet})
			c.sys = fakeSystem("linux", "amd64", 8, nil, nil)
			rep := c.Report(t.Context())
			if rep.ModelsDir == nil || *rep.ModelsDir != tt.want {
				t.Errorf("modelsDir = %v, want %q", rep.ModelsDir, tt.want)
			}
		})
	}
}
