// SPDX-License-Identifier: Apache-2.0

package selector

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestRule(t *testing.T) {
	tests := []struct {
		key        keyState
		wantKind   domain.ProviderKind
		wantReason string // "" means omitted
	}{
		{keyUnknown, api.ProviderKindLocal, string(api.NoGoogleApiKey)},
		{keyNone, api.ProviderKindLocal, string(api.NoGoogleApiKey)},
		{keyValid, api.ProviderKindGemini, string(api.GoogleApiKeyValid)},
		{keyInvalid, api.ProviderKindLocal, string(api.GoogleApiKeyInvalid)},
		{keyUnverified, api.ProviderKindLocal, ""},
	}
	for _, tt := range tests {
		t.Run(tt.key.String(), func(t *testing.T) {
			d := rule(tt.key)
			if d.Kind != tt.wantKind {
				t.Errorf("kind %s, want %s", d.Kind, tt.wantKind)
			}
			got := ""
			if d.Reason != nil {
				got = string(*d.Reason)
			}
			if got != tt.wantReason {
				t.Errorf("reason %q, want %q", got, tt.wantReason)
			}
			if d.Kind == api.ProviderKindMock {
				t.Error("mock is never the default")
			}
		})
	}
}

func TestFallbackWarning(t *testing.T) {
	tests := []struct {
		prev, cur keyState
		want      string
	}{
		{keyUnknown, keyNone, ""}, // never had a key: local is simply the default
		{keyUnknown, keyValid, ""},
		{keyUnknown, keyInvalid, CodeFallbackKeyInvalid},
		{keyUnknown, keyUnverified, CodeFallbackKeyUnverified},
		{keyValid, keyNone, CodeFallbackKeyRemoved},
		{keyValid, keyInvalid, CodeFallbackKeyInvalid},
		{keyValid, keyUnverified, CodeFallbackKeyUnverified},
		{keyInvalid, keyInvalid, ""}, // warned already
		{keyInvalid, keyNone, ""},    // was local already
		{keyInvalid, keyValid, ""},
		{keyUnverified, keyNone, ""},
		{keyNone, keyInvalid, CodeFallbackKeyInvalid},
		{keyNone, keyValid, ""},
	}
	for _, tt := range tests {
		t.Run(tt.prev.String()+"→"+tt.cur.String(), func(t *testing.T) {
			if got := fallbackWarning(tt.prev, tt.cur); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// fakeValidator answers from a map of key → verdict; keys missing from it
// can't be checked.
type fakeValidator struct {
	mu      sync.Mutex
	verdict map[string]bool
	calls   int
	block   chan struct{} // if set, each call waits for it
}

func (f *fakeValidator) ValidateKey(_ context.Context, key string) (bool, error) {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	v, ok := f.verdict[key]
	if !ok {
		return false, errors.New("unreachable")
	}
	return v, nil
}

func (f *fakeValidator) set(key string, valid bool) {
	f.mu.Lock()
	f.verdict[key] = valid
	f.mu.Unlock()
}

func (f *fakeValidator) unset(key string) {
	f.mu.Lock()
	delete(f.verdict, key)
	f.mu.Unlock()
}

func (f *fakeValidator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fixture is a selector over a settable key, a fake clock and a record of
// admin events.
type fixture struct {
	*Selector
	v      *fakeValidator
	mu     sync.Mutex
	key    string
	keyErr error
	now    time.Time
	events []api.AdminEvent
}

func newFixture(t *testing.T, key string, verdicts map[string]bool) *fixture {
	t.Helper()
	f := &fixture{v: &fakeValidator{verdict: verdicts}, key: key, now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
	f.Selector = New(Options{
		APIKey: func(context.Context) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.key, f.keyErr
		},
		Validator: f.v,
		Local:     func(context.Context) (bool, string) { return false, CodeWhisperUnreachable },
		Publish: func(ev api.AdminEvent) {
			f.mu.Lock()
			f.events = append(f.events, ev)
			f.mu.Unlock()
		},
		Logger: slog.New(slog.DiscardHandler),
		Now: func() time.Time {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.now
		},
	})
	return f
}

func (f *fixture) setKey(k string) {
	f.mu.Lock()
	f.key = k
	f.mu.Unlock()
	f.Forget()
}

func (f *fixture) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *fixture) warnings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, ev := range f.events {
		if ev.Type == api.AdminEventTypeLog && ev.Log != nil && ev.Log.Level == api.AdminEventLogLevelWarn {
			out = append(out, ev.Log.Code)
		}
	}
	return out
}

// waitIdle waits for background checks to finish.
func (f *fixture) waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.Selector.mu.Lock()
		busy := f.inflight != nil
		f.Selector.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("background check didn't finish")
}

func TestResolve(t *testing.T) {
	const good, bad, offline = "AIzaGoodKey0001", "AIzaBadKey0002", "AIzaOfflineKey3"
	verdicts := map[string]bool{good: true, bad: false}
	tests := []struct {
		name       string
		key        string
		storeErr   error
		wantKind   domain.ProviderKind
		wantGemini bool
		wantCode   string // gemini reasonCode
		wantWarn   []string
	}{
		{"no key: local", "", nil, api.ProviderKindLocal, false, CodeNoAPIKey, nil},
		{"valid key: gemini", good, nil, api.ProviderKindGemini, true, "", nil},
		{"invalid key: local with a warning", bad, nil, api.ProviderKindLocal, false, CodeKeyInvalid, []string{CodeFallbackKeyInvalid}},
		{"unverifiable key: local with a warning", offline, nil, api.ProviderKindLocal, false, CodeKeyUnverified, []string{CodeFallbackKeyUnverified}},
		{"store error: local with a warning", "", errors.New("keychain locked"), api.ProviderKindLocal, false, CodeKeyUnverified, []string{CodeFallbackKeyUnverified}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.key, verdicts)
			f.keyErr = tt.storeErr
			if got := f.DefaultProvider(t.Context()); got != tt.wantKind {
				t.Errorf("DefaultProvider %s, want %s", got, tt.wantKind)
			}
			res := f.Providers(t.Context())
			if res.DefaultProvider != tt.wantKind {
				t.Errorf("defaultProvider %s, want %s", res.DefaultProvider, tt.wantKind)
			}
			byKind := map[api.ProviderKind]api.ProviderInfo{}
			for _, p := range res.Providers {
				byKind[p.Kind] = p
			}
			g := byKind[api.ProviderKindGemini]
			if g.Available != tt.wantGemini {
				t.Errorf("gemini available %v, want %v", g.Available, tt.wantGemini)
			}
			if code := deref(g.ReasonCode); code != tt.wantCode {
				t.Errorf("gemini reasonCode %q, want %q", code, tt.wantCode)
			}
			if l := byKind[api.ProviderKindLocal]; l.Available || deref(l.ReasonCode) != CodeWhisperUnreachable {
				t.Errorf("local %+v", l)
			}
			if m := byKind[api.ProviderKindMock]; !m.Available {
				t.Errorf("mock %+v", m)
			}
			f.waitIdle(t)
			if got := f.warnings(); !equal(got, tt.wantWarn) {
				t.Errorf("warnings %v, want %v", got, tt.wantWarn)
			}
		})
	}
}

// A key's verdict is cached; a stale one is used while a background check
// refreshes it, and a failed refresh keeps the last definite verdict.
func TestResolveCaching(t *testing.T) {
	const key = "AIzaCachedKey01"
	f := newFixture(t, key, map[string]bool{key: true})
	ctx := t.Context()
	for range 5 {
		if got := f.DefaultProvider(ctx); got != api.ProviderKindGemini {
			t.Fatalf("got %s", got)
		}
	}
	if n := f.v.count(); n != 1 {
		t.Fatalf("%d checks, want 1", n)
	}

	// Stale, and Google is unreachable now: still Gemini, one background check.
	f.v.unset(key)
	f.advance(DefaultValidTTL + time.Second)
	if got := f.DefaultProvider(ctx); got != api.ProviderKindGemini {
		t.Fatalf("stale: got %s", got)
	}
	f.waitIdle(t)
	if n := f.v.count(); n != 2 {
		t.Fatalf("%d checks, want 2", n)
	}
	if got := f.DefaultProvider(ctx); got != api.ProviderKindGemini {
		t.Fatalf("after a failed refresh: got %s", got)
	}

	// Google now rejects the key: after the retry interval the default falls back.
	f.v.set(key, false)
	f.advance(DefaultUnverifiedTTL + time.Second)
	f.DefaultProvider(ctx) // starts the refresh
	f.waitIdle(t)
	if got := f.DefaultProvider(ctx); got != api.ProviderKindLocal {
		t.Fatalf("revoked: got %s", got)
	}
	if got := f.warnings(); !equal(got, []string{CodeFallbackKeyInvalid}) {
		t.Errorf("warnings %v", got)
	}
}

func TestKeyRemovedWarns(t *testing.T) {
	const key = "AIzaRemovedKey1"
	f := newFixture(t, key, map[string]bool{key: true})
	if got := f.DefaultProvider(t.Context()); got != api.ProviderKindGemini {
		t.Fatalf("got %s", got)
	}
	f.setKey("")
	if got := f.DefaultProvider(t.Context()); got != api.ProviderKindLocal {
		t.Fatalf("removed: got %s", got)
	}
	f.DefaultProvider(t.Context())
	if got := f.warnings(); !equal(got, []string{CodeFallbackKeyRemoved}) {
		t.Errorf("warnings %v", got)
	}
	ev := f.events[0]
	if ev.Log.Params == nil || (*ev.Log.Params)["provider"] != "local" {
		t.Errorf("params %+v", ev.Log.Params)
	}
}

// A new key is checked at once, without waiting for the old verdict to expire.
func TestNewKeyIsChecked(t *testing.T) {
	const bad, good = "AIzaBadKey00001", "AIzaGoodKey0002"
	f := newFixture(t, bad, map[string]bool{bad: false, good: true})
	if got := f.DefaultProvider(t.Context()); got != api.ProviderKindLocal {
		t.Fatalf("got %s", got)
	}
	f.setKey(good)
	if got := f.DefaultProvider(t.Context()); got != api.ProviderKindGemini {
		t.Fatalf("new key: got %s", got)
	}
}

func TestValidate(t *testing.T) {
	const good, bad, offline = "AIzaGoodKey0001", "AIzaBadKey0002", "AIzaOfflineKey3"
	tests := []struct {
		name      string
		secret    string
		key       string
		wantErr   error
		wantValid bool
		wantCode  string
	}{
		{"valid", string(api.GoogleApiKey), good, nil, true, ""},
		{"invalid", string(api.GoogleApiKey), bad, nil, false, CodeKeyInvalid},
		{"unverifiable", string(api.GoogleApiKey), offline, nil, false, CodeKeyUnverified},
		{"not set", string(api.GoogleApiKey), "", ErrNotSet, false, ""},
		{"no check for the OBS password", string(api.ObsWebsocketPassword), good, ErrUnsupported, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.key, map[string]bool{good: true, bad: false})
			got, err := f.Validate(t.Context(), tt.secret)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Valid != tt.wantValid || deref(got.Code) != tt.wantCode {
				t.Errorf("got %+v (code %q), want valid %v code %q", got, deref(got.Code), tt.wantValid, tt.wantCode)
			}
			last := f.LastValid(t.Context(), tt.secret)
			switch {
			case tt.wantCode == CodeKeyUnverified && last != nil:
				t.Errorf("LastValid %v after an unverifiable check, want nil", *last)
			case tt.wantCode != CodeKeyUnverified && (last == nil || *last != tt.wantValid):
				t.Errorf("LastValid %v, want %v", last, tt.wantValid)
			}
		})
	}
}

// Explicit checks of the same key are rate-limited.
func TestValidateRateLimit(t *testing.T) {
	const key = "AIzaRateLimited"
	f := newFixture(t, key, map[string]bool{key: true})
	for range 3 {
		if _, err := f.Validate(t.Context(), string(api.GoogleApiKey)); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.v.count(); n != 1 {
		t.Fatalf("%d checks, want 1", n)
	}
	f.advance(DefaultMinRecheck + time.Second)
	if _, err := f.Validate(t.Context(), string(api.GoogleApiKey)); err != nil {
		t.Fatal(err)
	}
	if n := f.v.count(); n != 2 {
		t.Fatalf("%d checks, want 2", n)
	}
	if got := f.LastValid(t.Context(), string(api.ObsWebsocketPassword)); got != nil {
		t.Errorf("LastValid of the OBS password %v", *got)
	}
}

// Concurrent resolutions share one check.
func TestConcurrentResolveSharesCheck(t *testing.T) {
	const key = "AIzaConcurrent1"
	f := newFixture(t, key, map[string]bool{key: true})
	f.v.block = make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if got := f.DefaultProvider(context.Background()); got != api.ProviderKindGemini {
				t.Errorf("got %s", got)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(f.v.block)
	wg.Wait()
	if n := f.v.count(); n != 1 {
		t.Fatalf("%d checks, want 1", n)
	}
}

func TestLocalProbe(t *testing.T) {
	up := func(status int) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		t.Cleanup(s.Close)
		return s
	}
	down := func() string {
		s := httptest.NewServer(http.NotFoundHandler())
		s.Close() // nothing listens there any more
		return s.URL
	}
	tests := []struct {
		name            string
		whisper, ollama func() string
		wantOK          bool
		wantCode        string
	}{
		{"both up", func() string { return up(200).URL }, func() string { return up(200).URL }, true, ""},
		{"whisper loading counts as up", func() string { return up(503).URL }, func() string { return up(200).URL }, true, ""},
		{"whisper down", down, func() string { return up(200).URL }, false, CodeWhisperUnreachable},
		{"ollama down", func() string { return up(200).URL }, down, false, CodeOllamaUnreachable},
		{"ollama error", func() string { return up(200).URL }, func() string { return up(500).URL }, false, CodeOllamaUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var st api.Settings
			st.Providers.Local.WhisperUrl, st.Providers.Local.OllamaUrl = tt.whisper(), tt.ollama()
			p := LocalProbe{Settings: func(context.Context) (api.Settings, error) { return st, nil }}
			ok, code := p.Probe(t.Context())
			if ok != tt.wantOK || code != tt.wantCode {
				t.Errorf("Probe = %v, %q; want %v, %q", ok, code, tt.wantOK, tt.wantCode)
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Available says where a running session may fall back to (AI-8).
func TestAvailable(t *testing.T) {
	const good, bad, offline = "AIzaGoodKey0001", "AIzaBadKey0002", "AIzaOfflineKey3"
	verdicts := map[string]bool{good: true, bad: false}
	tests := []struct {
		name     string
		key      string
		kind     domain.ProviderKind
		want     bool
		wantCode string
	}{
		{"gemini with a valid key", good, api.ProviderKindGemini, true, ""},
		{"gemini with a rejected key", bad, api.ProviderKindGemini, false, CodeKeyInvalid},
		{"gemini with an unverifiable key", offline, api.ProviderKindGemini, false, CodeKeyUnverified},
		{"gemini without a key", "", api.ProviderKindGemini, false, CodeNoAPIKey},
		{"local with its sidecars down", good, api.ProviderKindLocal, false, CodeWhisperUnreachable},
		{"never mock", good, api.ProviderKindMock, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.key, verdicts)
			ok, code := f.Available(t.Context(), tt.kind)
			if ok != tt.want || code != tt.wantCode {
				t.Errorf("Available(%s) = %v, %q; want %v, %q", tt.kind, ok, code, tt.want, tt.wantCode)
			}
			f.waitIdle(t)
		})
	}
	f := newFixture(t, good, verdicts)
	f.opts.Local = nil
	if ok, _ := f.Available(t.Context(), api.ProviderKindLocal); !ok {
		t.Error("local without a probe is unavailable")
	}
}
