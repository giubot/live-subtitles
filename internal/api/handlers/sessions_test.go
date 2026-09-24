// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/bus"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
	"github.com/iencodev/live-subtitles/internal/session"
	"github.com/iencodev/live-subtitles/internal/store"
)

func TestSessionControl(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now()
	for id, provider := range map[string]api.ProviderChoice{"main": api.ProviderChoiceMock, "gem": api.ProviderChoiceGemini} {
		if err := st.CreateSession(t.Context(), domain.Session{Id: id, CreatedAt: now, UpdatedAt: now,
			Provider: provider, SourceLanguage: api.Auto, TargetLanguages: []string{"es"}}); err != nil {
			t.Fatal(err)
		}
	}
	m := session.New(session.Options{
		Sessions: st, Captions: st, Bus: bus.New(), Logger: slog.New(slog.DiscardHandler),
		Providers: map[domain.ProviderKind]session.Provider{
			api.ProviderKindMock: {ASR: &mock.ASR{}, Translator: &mock.Translator{}},
		},
		IngestSource: func(string) domain.AudioSource { return &fake.Source{Realtime: true} },
	})
	t.Cleanup(m.Close)
	s := New()
	s.Manager = m
	h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))

	for _, c := range []call{
		{"POST", "/api/sessions/nope/start", "", "", "", 404, `"code":"session.not_found"`},
		{"POST", "/api/sessions/nope/pause", "", "", "", 404, `"code":"session.not_found"`},
		{"POST", "/api/sessions/nope/stop", "", "", "", 404, `"code":"session.not_found"`},
		{"GET", "/api/sessions/nope/status", "", "", "", 404, `"code":"session.not_found"`},
		{"POST", "/api/sessions/gem/start", "", "", "", 422, `"code":"provider.unavailable"`},
		{"GET", "/api/sessions/gem/status", "", "", "", 200, `"state":"error"`},
		{"POST", "/api/sessions/main/start", `{"source":"srt"}`, "", "", 422, `"code":"source.unsupported"`},
		{"POST", "/api/sessions/main/pause", "", "", "", 409, `"code":"session.state_conflict"`},
		{"POST", "/api/sessions/main/start", `{"source":"browser"}`, "", "", 200, `"state":"live"`},
		{"POST", "/api/sessions/main/start", "", "", "", 409, `"params":{"state":"live"}`},
		{"POST", "/api/sessions/main/pause", "", "", "", 200, `"state":"paused"`},
		{"POST", "/api/sessions/main/start", "", "", "", 200, `"state":"live"`},
		{"POST", "/api/sessions/main/stop", "", "", "", 200, `"state":"idle"`},
		{"GET", "/ws/captions/main?lang=es", "", "", "", 501, `"code":"not_implemented"`},
	} {
		c.do(t, h)
	}
}
