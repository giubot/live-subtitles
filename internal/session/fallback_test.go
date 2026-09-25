// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
)

// fallbackSetting is a SettingsStore with settings.providers.fallback.
type fallbackSetting bool

func (f fallbackSetting) Settings(context.Context) (api.Settings, error) {
	var s api.Settings
	on := bool(f)
	s.Providers.Fallback = &on
	return s, nil
}

func (fallbackSetting) PutSettings(context.Context, api.Settings) error { return nil }

// failingASR fails in a scripted way; with no failure set it is the mock.
type failingASR struct {
	mock.ASR
	// startErr fails every Start after the first startOK ones.
	startErr error
	startOK  int
	// err is reported every errEvery frames.
	err      error
	errEvery int
	// crashAfter > 0 ends every stream after that many frames.
	crashAfter int

	mu     sync.Mutex
	starts int
}

func (a *failingASR) Start(ctx context.Context, cfg domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	a.mu.Lock()
	a.starts++
	n := a.starts
	a.mu.Unlock()
	if a.startErr != nil && n > a.startOK {
		return nil, nil, a.startErr
	}
	if a.err == nil && a.crashAfter == 0 {
		return a.ASR.Start(ctx, cfg)
	}
	in, out := make(chan domain.AudioFrame), make(chan domain.ASREvent)
	go func() {
		defer close(out)
		for frames := 1; ; frames++ {
			select {
			case _, ok := <-in:
				if !ok {
					return
				}
			case <-ctx.Done():
				return
			}
			if a.crashAfter > 0 && frames >= a.crashAfter {
				return
			}
			if a.err != nil && frames%a.errEvery == 0 {
				select {
				case out <- domain.ASREvent{Err: a.err}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return in, out, nil
}

func (a *failingASR) startCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.starts
}

// failingTranslator fails every call with err.
type failingTranslator struct {
	mock.Translator
	err error
}

func (t *failingTranslator) Translate(context.Context, domain.TranslateRequest) (domain.TranslateResult, error) {
	return domain.TranslateResult{}, t.err
}

var (
	errQuota = &domain.CodedError{Code: domain.CodeProviderQuotaExhausted, Message: "gemini: RESOURCE_EXHAUSTED"}
	errAuth  = &domain.CodedError{Code: domain.CodeProviderAuthFailed, Message: "gemini: API key not valid"}
)

func providerSess(id string, p api.ProviderChoice) domain.Session {
	s := sess(id, "es")
	s.Provider = p
	return s
}

// fallbacks are the switches the published statuses reported, in order.
// Each status shows the provider switched to as the active one.
func (w *watch) fallbacks(t *testing.T) []api.ProviderFallback {
	t.Helper()
	statuses, _ := w.snapshot()
	var out []api.ProviderFallback
	for _, s := range statuses {
		if s.Fallback == nil {
			continue
		}
		if s.Provider == nil || *s.Provider != s.Fallback.To {
			t.Errorf("status provider %v while the fallback is to %s", s.Provider, s.Fallback.To)
		}
		if len(out) == 0 || out[len(out)-1].Switches != s.Fallback.Switches {
			out = append(out, *s.Fallback)
		}
	}
	return out
}

func TestProviderFallback(t *testing.T) {
	healthy := func() Provider { return Provider{ASR: &failingASR{}, Translator: &mock.Translator{}} }
	tests := []struct {
		name          string
		enabled       bool
		choice        api.ProviderChoice
		gemini, local Provider
		unavailable   bool // FallbackAvailable says no
		wantReason    string
		wantTo        domain.ProviderKind // "" = no switch
		wantGap       bool                // the captions after the switch mark lost audio
		atStart       bool                // it switched before going live
		wantState     api.SessionState    // once the audio ends
		wantErrCode   string              // in the final status
		startFails    bool
	}{
		{name: "quota mid-stream", enabled: true, choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{err: errQuota, errEvery: 25}, Translator: &mock.Translator{}}, local: healthy(),
			wantReason: domain.CodeProviderQuotaExhausted, wantTo: api.ProviderKindLocal, wantGap: true, wantState: api.SessionStateIdle},
		{name: "key rejected at start", enabled: true, choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{startErr: errAuth}, Translator: &mock.Translator{}}, local: healthy(),
			wantReason: domain.CodeProviderAuthFailed, wantTo: api.ProviderKindLocal, atStart: true, wantState: api.SessionStateIdle},
		{name: "translation quota", enabled: true, choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{}, Translator: &failingTranslator{err: errQuota}}, local: healthy(),
			wantReason: domain.CodeProviderQuotaExhausted, wantTo: api.ProviderKindLocal, wantState: api.SessionStateIdle},
		{name: "repeated errors", enabled: true, choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{err: errors.New("gemini: live connection lost, reconnecting"), errEvery: 5}, Translator: &mock.Translator{}}, local: healthy(),
			wantReason: CodeErrorsRepeated, wantTo: api.ProviderKindLocal, wantGap: true, wantState: api.SessionStateIdle},
		{name: "crash loop", enabled: true, choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{crashAfter: 5}, Translator: &mock.Translator{}}, local: healthy(),
			wantReason: CodeRestartsFailed, wantTo: api.ProviderKindLocal, wantGap: true, wantState: api.SessionStateIdle},
		{name: "local sidecar down at start", enabled: true, choice: api.ProviderChoiceLocal,
			gemini: healthy(), local: Provider{ASR: &failingASR{startErr: errors.New("whisper-server isn't reachable")}, Translator: &mock.Translator{}},
			wantReason: CodeProviderUnavailable, wantTo: api.ProviderKindGemini, atStart: true, wantState: api.SessionStateIdle},
		{name: "local sidecar dies mid-session", enabled: true, choice: api.ProviderChoiceLocal,
			gemini: healthy(), local: Provider{ASR: &failingASR{crashAfter: 25, startOK: 1, startErr: errors.New("whisper-server isn't reachable")}, Translator: &mock.Translator{}},
			wantReason: CodeRestartsFailed, wantTo: api.ProviderKindGemini, wantGap: true, wantState: api.SessionStateIdle},

		// No switch: disabled, or the other provider is unavailable.
		{name: "disabled: quota", choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{err: errQuota, errEvery: 25}, Translator: &mock.Translator{}}, local: healthy(),
			wantState: api.SessionStateIdle},
		{name: "disabled: crash loop", choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{crashAfter: 5}, Translator: &mock.Translator{}}, local: healthy(),
			wantState: api.SessionStateError, wantErrCode: CodeProviderFailed},
		{name: "disabled: key rejected at start", choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{startErr: errAuth}, Translator: &mock.Translator{}}, local: healthy(),
			wantState: api.SessionStateError, wantErrCode: domain.CodeProviderAuthFailed, startFails: true},
		{name: "other provider unavailable", enabled: true, unavailable: true, choice: api.ProviderChoiceGemini,
			gemini: Provider{ASR: &failingASR{crashAfter: 5}, Translator: &mock.Translator{}}, local: healthy(),
			wantState: api.SessionStateError, wantErrCode: CodeProviderFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var probes []domain.ProviderKind
			var mu sync.Mutex
			opts := Options{
				Restart:  fastRestart(4),
				Settings: fallbackSetting(tt.enabled),
				Providers: map[domain.ProviderKind]Provider{
					api.ProviderKindGemini: tt.gemini, api.ProviderKindLocal: tt.local,
				},
				FallbackAvailable: func(_ context.Context, kind domain.ProviderKind) (bool, string) {
					mu.Lock()
					probes = append(probes, kind)
					mu.Unlock()
					if tt.unavailable {
						return false, "provider.whisper_unreachable"
					}
					return true, ""
				},
			}
			e := newEnv(t, opts, providerSess("main", tt.choice))
			w := e.watch(t, "main")
			st, err := e.m.Start(t.Context(), "main", &fake.Source{Realtime: true, Duration: 4 * time.Second})
			if tt.startFails {
				if !errors.Is(err, ErrUnavailable) || st.Error == nil || st.Error.Code != tt.wantErrCode {
					t.Fatalf("start: %+v, %v; want %s", st.Error, err, tt.wantErrCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			waitState(t, e.m, "main", tt.wantState)
			final, _ := e.m.Status(t.Context(), "main")
			fbs := w.fallbacks(t)
			statuses, captions := w.snapshot()

			if tt.wantTo == "" {
				if len(fbs) != 0 || w.count(CodeProviderFallback) != 0 {
					t.Fatalf("switched: %+v", fbs)
				}
				if tt.wantErrCode != "" && (final.Error == nil || final.Error.Code != tt.wantErrCode) {
					t.Errorf("final error %+v, want %s", final.Error, tt.wantErrCode)
				}
				for _, s := range statuses {
					if s.Provider != nil && *s.Provider != domain.ProviderKind(tt.choice) {
						t.Fatalf("status on %s", *s.Provider)
					}
				}
				if tt.unavailable {
					mu.Lock()
					n := len(probes)
					mu.Unlock()
					if n != 1 {
						t.Errorf("%d availability probes, want 1 (rate-limited)", n)
					}
				}
				return
			}

			if len(fbs) != 1 || w.count(CodeProviderFallback) != 1 {
				t.Fatalf("switches %+v, %d %s logs; want one", fbs, w.count(CodeProviderFallback), CodeProviderFallback)
			}
			fb := fbs[0]
			from := domain.ProviderKind(tt.choice)
			if fb.From != from || fb.To != tt.wantTo || fb.ReasonCode != tt.wantReason || fb.Switches != 1 || fb.Error == nil {
				t.Errorf("fallback %+v, want %s → %s for %s", fb, from, tt.wantTo, tt.wantReason)
			}
			if final.State != tt.wantState || final.Error != nil {
				t.Errorf("final status %s, %+v", final.State, final.Error)
			}
			// Captions went on, on the same tracks, from the new provider.
			tracks := map[string]bool{}
			gapped := map[string]bool{}
			prefix := "r0-f1-" // the stream opened by the switch
			if tt.atStart {
				prefix = "r0-"
			}
			for _, c := range captions {
				if strings.HasPrefix(c.SegmentId, prefix) {
					tracks[c.Lang] = true
					if c.GapBeforeMs != nil && *c.GapBeforeMs > 0 {
						gapped[c.Lang] = true
					}
				}
			}
			if !tracks[domain.SourceTrack] || !tracks["es"] {
				t.Errorf("captions after the switch on %v, want source and es", tracks)
			}
			if tt.wantGap && (!gapped[domain.SourceTrack] || !gapped["es"]) {
				t.Errorf("gap marked on %v, want source and es", gapped)
			}
			stored := finals(t, e.store, "main", domain.SourceTrack)
			if len(stored) == 0 {
				t.Error("no final stored")
			}
		})
	}
}

// Both providers fail: the run switches once and stays within the
// cooldown; with a short cooldown it switches back, at most once per
// cooldown.
func TestProviderFallbackNoFlapping(t *testing.T) {
	tests := []struct {
		name          string
		cooldown      time.Duration
		minSw, maxSw  int
		wantLastState api.SessionState
	}{
		{"long cooldown", time.Hour, 1, 1, api.SessionStateIdle},
		{"short cooldown", 600 * time.Millisecond, 2, 4, api.SessionStateIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gemini := &failingASR{err: errQuota, errEvery: 10}
			local := &failingASR{err: errQuota, errEvery: 10}
			e := newEnv(t, Options{
				Restart:  fastRestart(4),
				Settings: fallbackSetting(true),
				Fallback: FallbackPolicy{Cooldown: tt.cooldown},
				Providers: map[domain.ProviderKind]Provider{
					api.ProviderKindGemini: {ASR: gemini, Translator: &mock.Translator{}},
					api.ProviderKindLocal:  {ASR: local, Translator: &mock.Translator{}},
				},
			}, providerSess("main", api.ProviderChoiceGemini))
			w := e.watch(t, "main")
			if _, err := e.m.Start(t.Context(), "main", &fake.Source{Realtime: true, Duration: 2 * time.Second}); err != nil {
				t.Fatal(err)
			}
			waitState(t, e.m, "main", tt.wantLastState)
			fbs := w.fallbacks(t)
			n := w.count(CodeProviderFallback)
			if n < tt.minSw || n > tt.maxSw || len(fbs) != n {
				t.Fatalf("%d switches (%+v), want %d..%d", n, fbs, tt.minSw, tt.maxSw)
			}
			for i, fb := range fbs {
				if fb.Switches != i+1 {
					t.Errorf("switch %d counted %d", i+1, fb.Switches)
				}
				if i > 0 {
					if d := fb.At.Sub(fbs[i-1].At); d < tt.cooldown {
						t.Errorf("switch %d came %v after the previous, within the %v cooldown", i+1, d, tt.cooldown)
					}
					if fb.From != fbs[i-1].To {
						t.Errorf("switch %d from %s, want %s", i+1, fb.From, fbs[i-1].To)
					}
				}
			}
			if n == 1 && local.startCount() != 1 {
				t.Errorf("local started %d times, want 1", local.startCount())
			}
		})
	}
}

// Stop and start again: the new run is back on the session's own provider.
func TestProviderFallbackResetsOnRestart(t *testing.T) {
	gemini := &failingASR{err: errQuota, errEvery: 10}
	e := newEnv(t, Options{
		Settings: fallbackSetting(true),
		Providers: map[domain.ProviderKind]Provider{
			api.ProviderKindGemini: {ASR: gemini, Translator: &mock.Translator{}},
			api.ProviderKindLocal:  {ASR: &failingASR{}, Translator: &mock.Translator{}},
		},
	}, providerSess("main", api.ProviderChoiceGemini))
	for run := range 2 {
		if _, err := e.m.Start(t.Context(), "main", &fake.Source{Realtime: true}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			st, _ := e.m.Status(t.Context(), "main")
			if st.Fallback != nil {
				if *st.Provider != api.ProviderKindLocal || st.Fallback.Switches != 1 {
					t.Fatalf("run %d: %+v on %s", run, st.Fallback, *st.Provider)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("run %d never fell back", run)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if st, err := e.m.Stop(t.Context(), "main"); err != nil || st.Fallback != nil {
			t.Fatalf("stop: %+v, %v", st, err)
		}
	}
	if n := gemini.startCount(); n != 2 {
		t.Errorf("gemini started %d times, want once per run", n)
	}
}
