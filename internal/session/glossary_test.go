// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
)

// glossarySpy records the glossary each ASR stream and translation got.
type glossarySpy struct {
	mock.ASR
	tr mock.Translator

	mu         sync.Mutex
	asr        []*domain.Glossary
	translated []*domain.Glossary
}

func (s *glossarySpy) Start(ctx context.Context, cfg domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	s.mu.Lock()
	s.asr = append(s.asr, cfg.Glossary)
	s.mu.Unlock()
	return s.ASR.Start(ctx, cfg)
}

func (s *glossarySpy) Translate(ctx context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	s.mu.Lock()
	s.translated = append(s.translated, req.Glossary)
	s.mu.Unlock()
	return s.tr.Translate(ctx, req)
}

type spyTranslator struct{ *glossarySpy }

func (spyTranslator) Kind() domain.ProviderKind { return api.ProviderKindMock }

func TestSessionGlossary(t *testing.T) {
	g := domain.Glossary{Id: "talks", Name: "Talks", UpdatedAt: time.Now(),
		Terms: []api.GlossaryTerm{{Term: "cluster"}}, DoNotTranslate: []string{"Kubernetes"}}
	tests := []struct {
		name     string
		glossary *string
		want     string // glossary id the pipeline gets; "" for none
	}{
		{"session glossary", ptr("talks"), "talks"},
		{"no glossary", nil, ""},
		{"deleted glossary", ptr("gone"), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spy := &glossarySpy{}
			s := sess("main", "es")
			s.GlossaryId = tc.glossary
			e := newEnv(t, Options{Providers: map[domain.ProviderKind]Provider{
				api.ProviderKindMock: {ASR: spy, Translator: spyTranslator{spy}},
			}}, s)
			e.m.opts.Glossaries = e.store
			if err := e.store.CreateGlossary(t.Context(), g); err != nil {
				t.Fatal(err)
			}
			if _, err := e.m.Start(t.Context(), "main", &fake.Source{Duration: 5 * time.Second}); err != nil {
				t.Fatal(err)
			}
			waitState(t, e.m, "main", api.SessionStateIdle)

			spy.mu.Lock()
			defer spy.mu.Unlock()
			if len(spy.asr) != 1 || len(spy.translated) == 0 {
				t.Fatalf("%d ASR streams, %d translations", len(spy.asr), len(spy.translated))
			}
			for _, got := range append(spy.asr, spy.translated...) {
				id := ""
				if got != nil {
					id = got.Id
				}
				if id != tc.want {
					t.Errorf("pipeline got glossary %q, want %q", id, tc.want)
				}
			}
		})
	}
}
