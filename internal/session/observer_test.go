// SPDX-License-Identifier: Apache-2.0

package session

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
)

type fakeObserver struct {
	mu        sync.Mutex
	latencies map[string]int // "provider/track" → finals observed
	errors    []string       // "provider/code"
}

func (o *fakeObserver) CaptionLatency(p domain.ProviderKind, track string, ms int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if ms < 0 {
		panic("negative latency")
	}
	o.latencies[string(p)+"/"+track]++
}

func (o *fakeObserver) SessionError(p domain.ProviderKind, code string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.errors = append(o.errors, string(p)+"/"+code)
}

func TestObserver(t *testing.T) {
	for _, c := range []struct {
		name          string
		asr           domain.ASRProvider
		src           domain.AudioSource
		endState      api.SessionState
		wantLatencies []string
		wantErrors    []string
	}{
		{
			name:          "finals report their latency per track",
			asr:           &mock.ASR{},
			src:           &fake.Source{Duration: 10 * time.Second},
			endState:      api.SessionStateIdle,
			wantLatencies: []string{"mock/source", "mock/es"},
		},
		{
			name:       "provider crash",
			asr:        crashingASR{},
			src:        &fake.Source{Realtime: true},
			endState:   api.SessionStateError,
			wantErrors: []string{"mock/" + CodeProviderError},
		},
		{
			name:       "provider refuses its config",
			asr:        refusingASR{},
			src:        &fake.Source{},
			endState:   api.SessionStateError,
			wantErrors: []string{"mock/provider.model_english_only"},
		},
		{
			name:       "source unavailable",
			asr:        &mock.ASR{},
			src:        &failingSource{},
			endState:   api.SessionStateError,
			wantErrors: []string{"mock/" + CodeSourceUnavailable},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := &fakeObserver{latencies: map[string]int{}}
			e := newEnv(t, Options{Observer: o, Providers: map[domain.ProviderKind]Provider{
				api.ProviderKindMock: {ASR: c.asr, Translator: &mock.Translator{}},
			}}, sess("main", "es"))
			_, _ = e.m.Start(t.Context(), "main", c.src)
			waitState(t, e.m, "main", c.endState)

			o.mu.Lock()
			defer o.mu.Unlock()
			for _, k := range c.wantLatencies {
				if o.latencies[k] == 0 {
					t.Errorf("no latency for %s: %v", k, o.latencies)
				}
			}
			if len(c.wantLatencies) == 0 && len(o.latencies) > 0 {
				t.Errorf("unexpected latencies %v", o.latencies)
			}
			for _, k := range c.wantErrors {
				if !slices.Contains(o.errors, k) {
					t.Errorf("errors %v, want %s", o.errors, k)
				}
			}
			if len(c.wantErrors) == 0 && len(o.errors) > 0 {
				t.Errorf("unexpected errors %v", o.errors)
			}
		})
	}
}
