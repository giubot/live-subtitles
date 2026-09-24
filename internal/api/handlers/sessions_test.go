// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
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

func TestFileSource(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now()
	if err := st.CreateSession(t.Context(), domain.Session{Id: "main", CreatedAt: now, UpdatedAt: now,
		Provider: api.ProviderChoiceMock, SourceLanguage: api.En, TargetLanguages: []string{"es"}}); err != nil {
		t.Fatal(err)
	}
	m := session.New(session.Options{
		Sessions: st, Captions: st, Bus: bus.New(), Logger: slog.New(slog.DiscardHandler),
		Providers: map[domain.ProviderKind]session.Provider{
			api.ProviderKindMock: {ASR: &mock.ASR{}, Translator: &mock.Translator{}},
		},
		IngestSource: func(string) domain.AudioSource { return &fake.Source{Realtime: true} },
	})
	t.Cleanup(m.Close)
	testdata, _ := filepath.Abs(filepath.Join("..", "..", "..", "testdata"))
	s := New()
	s.Manager = m
	s.Files = &ffmpeg.Files{Roots: []string{testdata}}
	h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))

	fixture := filepath.Join(testdata, "audio", "fixtures", "en.wav")
	body := func(uri string) string { return `{"uri":` + strconv.Quote(uri) + `,"loop":true}` }
	for _, c := range []call{
		{"DELETE", "/api/sessions/main/sources/file", "", "", "", 404, `"code":"source.not_running"`},
		{"POST", "/api/sessions/main/sources/file", body("/etc/hostname"), "", "", 400, `"code":"source.file_not_allowed"`},
		{"POST", "/api/sessions/main/sources/file", body(filepath.Join(testdata, "nope.wav")), "", "", 400, `"code":"source.file_not_found"`},
		{"POST", "/api/sessions/main/sources/file", `{"uri":"x.wav","startAtSec":-1}`, "", "", 400, `"code":"request.invalid"`},
		{"POST", "/api/sessions/nope/sources/file", body(fixture), "", "", 404, `"code":"session.not_found"`},
		{"POST", "/api/sessions/main/sources/file", body(fixture), "", "", 202, `"source":"file"`},
		{"POST", "/api/sessions/main/sources/file", body(fixture), "", "", 409, `"code":"session.state_conflict"`},
		{"POST", "/api/sessions/main/pause", "", "", "", 200, `"state":"paused"`},
		// A paused session doesn't resume onto a different source.
		{"POST", "/api/sessions/main/sources/file", body(fixture), "", "", 409, `"code":"session.state_conflict"`},
		{"DELETE", "/api/sessions/main/sources/file", "", "", "", 204, ""},
		{"GET", "/api/sessions/main/status", "", "", "", 200, `"state":"idle"`},
		{"DELETE", "/api/sessions/nope/sources/file", "", "", "", 404, `"code":"session.not_found"`},
	} {
		c.do(t, h)
	}

	s.Files.Binary = filepath.Join(t.TempDir(), "no-ffmpeg")
	call{"POST", "/api/sessions/main/sources/file", body(fixture), "", "", 422, `"code":"source.unavailable"`}.do(t, h)
}
