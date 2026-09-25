// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
	"github.com/iencodev/live-subtitles/internal/auth"
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

func TestSessionCRUD(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m := session.New(session.Options{
		Sessions: st, Captions: st, Bus: bus.New(), Logger: slog.New(slog.DiscardHandler),
		Providers: map[domain.ProviderKind]session.Provider{
			api.ProviderKindMock: {ASR: &mock.ASR{}, Translator: &mock.Translator{}},
		},
		IngestSource: func(string) domain.AudioSource { return &fake.Source{Realtime: true} },
	})
	t.Cleanup(m.Close)
	events := m.Events().Subscribe(t.Context())
	s := New()
	s.Sessions, s.Settings, s.Manager = st, st, m
	s.Auth = auth.New(st, auth.Options{AdminToken: testAdminToken})
	s.Network = func() api.NetworkInfo { return api.NetworkInfo{ViewerBaseUrl: "http://192.168.1.20:8080"} }
	h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
	admin := func(method, path, body string, status int, want string) *http.Response {
		t.Helper()
		return call{method, path, body, "", testAdminToken, status, want}.do(t, h)
	}

	// Defaults without stored settings: [es, en], auto, recording on.
	res := admin("POST", "/api/sessions", `{"slug":"main","name":" Main stage ","provider":"mock"}`, 201, `"ingestToken":"`)
	var created api.SessionWithToken
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	want := api.SessionUrls{
		Viewer: "http://192.168.1.20:8080/s/main", Stage: "http://192.168.1.20:8080/stage/main",
		Overlay: "http://192.168.1.20:8080/overlay/main?lang=es", Capture: "http://192.168.1.20:8080/capture/main",
		Replay: "http://192.168.1.20:8080/replay/main",
	}
	if created.Name != "Main stage" || created.SourceLanguage != api.Auto || !slices.Equal(created.TargetLanguages, []string{"es", "en"}) ||
		!created.RecordingEnabled || created.State != api.SessionStateIdle || created.EffectiveProvider != api.ProviderKindMock ||
		created.Urls != want {
		t.Errorf("created = %+v", created)
	}
	if err := s.Auth.VerifyIngestToken(t.Context(), "main", created.IngestToken); err != nil {
		t.Errorf("ingest token doesn't verify: %v", err)
	}
	if ev := <-events; ev.Type != api.AdminEventTypeSessionCreated || ev.Session == nil || ev.Session.Id != "main" {
		t.Errorf("event = %+v", ev)
	}

	// Defaults from stored settings.
	var set api.Settings
	_ = json.Unmarshal([]byte(`{"defaultSourceLanguage":"en","defaultTargetLanguages":["pt"],
		"providers":{"gemini":{"liveModel":"a","translationModel":"b"},"local":{"whisperUrl":"","whisperModel":"","ollamaUrl":"","gemmaModel":""}},
		"network":{},"recording":{"enabledByDefault":false,"bitrateKbps":32,"retentionDays":30}}`), &set)
	if err := st.PutSettings(t.Context(), set); err != nil {
		t.Fatal(err)
	}
	admin("POST", "/api/sessions", `{"slug":"side","name":"Side"}`, 201,
		`"recordingEnabled":false,"sourceLanguage":"en","state":"idle","targetLanguages":["pt"]`)

	for _, c := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"bad slug", `{"slug":"Main Stage","name":"x"}`, 400, `"code":"session.slug_invalid"`},
		{"slug too long", `{"slug":"` + strings.Repeat("a", 64) + `","name":"x"}`, 400, `"slug":"session.slug_invalid"`},
		{"blank name", `{"slug":"x","name":"  "}`, 400, `"fields":{"name":"request.invalid"}`},
		{"bad language", `{"slug":"x","name":"x","targetLanguages":["spa"]}`, 400, `"targetLanguages":"request.invalid"`},
		{"unsupported target", `{"slug":"x","name":"x","targetLanguages":["es","ru"]}`, 400,
			`"code":"session.invalid_language","fields":{"targetLanguages":"session.invalid_language"}`},
		{"unsupported source", `{"slug":"x","name":"x","sourceLanguage":"pt"}`, 400,
			`"code":"session.invalid_language","fields":{"sourceLanguage":"session.invalid_language"}`},
		{"bad source", `{"slug":"x","name":"x","sourceLanguage":"english"}`, 400, `"sourceLanguage":"request.invalid"`},
		{"mixed errors", `{"slug":"x","name":" ","targetLanguages":["ru"]}`, 400, `"code":"request.invalid"`},
		{"duplicate language", `{"slug":"x","name":"x","targetLanguages":["es","es"]}`, 400, `"targetLanguages"`},
		{"no languages", `{"slug":"x","name":"x","targetLanguages":[]}`, 400, `"targetLanguages"`},
		{"bad provider", `{"slug":"x","name":"x","provider":"openai"}`, 400, `"provider":"request.invalid"`},
		{"bad stage lines", `{"slug":"x","name":"x","stageStyle":{"lines":9}}`, 400, `"stageStyle"`},
		{"slug taken", `{"slug":"main","name":"Again"}`, 409, `"code":"session.slug_taken"`},
	} {
		t.Run(c.name, func(t *testing.T) { admin("POST", "/api/sessions", c.body, c.status, c.want) })
	}

	admin("GET", "/api/sessions", "", 200, `"id":"main"`)
	admin("GET", "/api/sessions/main", "", 200, `"urls":{"capture":"http://192.168.1.20:8080/capture/main"`)
	admin("GET", "/api/sessions/nope", "", 404, `"code":"session.not_found"`)
	admin("PATCH", "/api/sessions/main", `{"sourceLanguage":"es","targetLanguages":["es","en","pt","fr","de","it","zh","ja","ko"]}`, 200,
		`"sourceLanguage":"es","state":"idle","targetLanguages":["es","en","pt","fr","de","it","zh","ja","ko"]`)
	admin("PATCH", "/api/sessions/main", `{"room":"Sala Konex","targetLanguages":["en"]}`, 200,
		`"overlay":"http://192.168.1.20:8080/overlay/main?lang=en"`)
	admin("PATCH", "/api/sessions/main", `{"name":""}`, 400, `"name":"request.invalid"`)
	admin("PATCH", "/api/sessions/main", `{"targetLanguages":["xx"]}`, 400, `"code":"session.invalid_language"`)
	admin("PATCH", "/api/sessions/main", `{"sourceLanguage":"fr"}`, 400, `"sourceLanguage":"session.invalid_language"`)
	admin("PATCH", "/api/sessions/nope", `{"name":"x"}`, 404, `"code":"session.not_found"`)

	// Public views carry no tokens or provider config.
	res = call{"GET", "/api/public/sessions", "", "", "", 200, `"languages":["en"]`}.do(t, h)
	var pub []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&pub); err != nil {
		t.Fatal(err)
	}
	if len(pub) != 2 || pub[0]["room"] != "Sala Konex" || pub[0]["provider"] != nil || pub[0]["urls"] != nil {
		t.Errorf("public sessions = %v", pub)
	}
	call{"GET", "/api/public/sessions/side", "", "", "", 200, `"state":"idle"`}.do(t, h)
	call{"GET", "/api/public/sessions/nope", "", "", "", 404, `"code":"session.not_found"`}.do(t, h)

	// A running session keeps its pipeline settings and can't be deleted.
	admin("POST", "/api/sessions/main/start", "", 200, `"state":"live"`)
	call{"GET", "/api/public/sessions/main", "", "", "", 200, `"state":"live"`}.do(t, h)
	admin("PATCH", "/api/sessions/main", `{"provider":"local"}`, 409, `"code":"session.state_conflict"`)
	admin("PATCH", "/api/sessions/main", `{"name":"Main hall"}`, 200, `"name":"Main hall"`)
	admin("DELETE", "/api/sessions/main", "", 409, `"code":"session.state_conflict"`)
	admin("POST", "/api/sessions/main/stop", "", 200, `"state":"idle"`)
	admin("DELETE", "/api/sessions/main", "", 204, "")
	admin("DELETE", "/api/sessions/main", "", 404, `"code":"session.not_found"`)
	admin("GET", "/api/sessions", "", 200, `[{"createdAt"`)
}
