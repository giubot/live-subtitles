// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/store"
)

func settingsServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New()
	s.Settings = st
	return s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler)), st
}

// settingsBody is DefaultSettings as JSON with edits applied by the
// replacer pairs (old, new).
func settingsBody(t *testing.T, edits ...string) string {
	t.Helper()
	b, err := json.Marshal(DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer(edits...).Replace(string(b))
}

func TestDefaultSettings(t *testing.T) {
	d := DefaultSettings()
	checks := []struct {
		name      string
		got, want any
	}{
		{"source", d.DefaultSourceLanguage, api.Auto},
		{"targets", strings.Join(d.DefaultTargetLanguages, ","), "es,en"},
		{"liveModel", d.Providers.Gemini.LiveModel, "gemini-3.5-transcribe-live"},
		{"translationModel", d.Providers.Gemini.TranslationModel, "gemini-3.5-flash-lite"},
		{"whisperUrl", d.Providers.Local.WhisperUrl, "http://127.0.0.1:8178"},
		{"whisperModel", d.Providers.Local.WhisperModel, "large-v3-turbo"},
		{"ollamaUrl", d.Providers.Local.OllamaUrl, "http://127.0.0.1:11434"},
		{"gemmaModel", d.Providers.Local.GemmaModel, "gemma3:4b"},
		{"fallback", *d.Providers.Fallback, false},
		{"contextSentences", *d.Translation.ContextSentences, 3},
		{"maxCharsPerLine", *d.Captions.MaxCharsPerLine, 42},
		{"maxLines", *d.Captions.MaxLines, 2},
		{"publicBaseUrl", d.Network.PublicBaseUrl, (*string)(nil)},
		{"recording.enabled", d.Recording.EnabledByDefault, true},
		{"bitrate", d.Recording.BitrateKbps, api.N32},
		{"retention", d.Recording.RetentionDays, 30},
		{"obs", *d.Obs.WebsocketUrl, "ws://127.0.0.1:4455"},
		{"srt.enabled", *d.Srt.Enabled, true},
		{"srt.port", *d.Srt.Port, 9000},
		{"srt.latency", *d.Srt.LatencyMs, 200},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if bad := validateSettings(d); len(bad) > 0 {
		t.Errorf("defaults are invalid: %v", bad)
	}
}

func TestSettingsEndpoints(t *testing.T) {
	h, _ := settingsServer(t)
	for _, c := range []call{
		// Nothing stored: the defaults.
		{"GET", "/api/settings", "", "", "", 200, `"liveModel":"gemini-3.5-transcribe-live"`},
		{"GET", "/api/settings", "", "", "", 200, `"retentionDays":30`},
		// Saved, then read back.
		{"PUT", "/api/settings", settingsBody(t, `"retentionDays":30`, `"retentionDays":0`), "", "", 200, `"retentionDays":0`},
		{"GET", "/api/settings", "", "", "", 200, `"retentionDays":0`},
		// Missing optional objects and blank strings take their defaults.
		{"PUT", "/api/settings", `{"defaultSourceLanguage":"es","defaultTargetLanguages":["en"],
			"providers":{"gemini":{"liveModel":" ","translationModel":""},"local":{"whisperUrl":"","whisperModel":"","ollamaUrl":"","gemmaModel":""}},
			"network":{"publicBaseUrl":""},"recording":{"enabledByDefault":false,"bitrateKbps":64,"retentionDays":7}}`,
			"", "", 200, `"liveModel":"gemini-3.5-transcribe-live"`},
		{"GET", "/api/settings", "", "", "", 200, `"defaultSourceLanguage":"es"`},
		{"GET", "/api/settings", "", "", "", 200, `"recording":{"bitrateKbps":64,"enabledByDefault":false,"retentionDays":7}`},
		// The SRT passphrase flag is read-only.
		{"PUT", "/api/settings", settingsBody(t, `"srt":{`, `"srt":{"passphraseSet":true,`), "", "", 200, `"srt":{"enabled":true,"latencyMs":200,"port":9000}`},
		{"PUT", "/api/settings", `not json`, "", "", 400, `"code":"request.invalid"`},
	} {
		c.do(t, h)
	}
}

func TestUpdateSettingsValidation(t *testing.T) {
	h, _ := settingsServer(t)
	tests := []struct {
		name      string
		edits     []string
		wantCode  string
		wantField string
	}{
		{"source not in catalog", []string{`"defaultSourceLanguage":"auto"`, `"defaultSourceLanguage":"pt"`}, codeSettingsLanguage, "defaultSourceLanguage"},
		{"unknown target", []string{`["es","en"]`, `["es","xx"]`}, codeSettingsLanguage, "defaultTargetLanguages"},
		{"duplicate target", []string{`["es","en"]`, `["es","es"]`}, "request.invalid", "defaultTargetLanguages"},
		{"whisper url scheme", []string{`"http://127.0.0.1:8178"`, `"ftp://127.0.0.1:8178"`}, codeSettingsURL, "providers.local.whisperUrl"},
		{"ollama url relative", []string{`"http://127.0.0.1:11434"`, `"127.0.0.1:11434"`}, codeSettingsURL, "providers.local.ollamaUrl"},
		{"public url with query", []string{`"network":{}`, `"network":{"publicBaseUrl":"https://subs.example.com/?a=1"}`}, codeSettingsURL, "network.publicBaseUrl"},
		{"obs url http", []string{`"ws://127.0.0.1:4455"`, `"http://127.0.0.1:4455"`}, codeSettingsURL, "obs.websocketUrl"},
		{"context sentences", []string{`"contextSentences":3`, `"contextSentences":11`}, codeSettingsOutOfRange, "translation.contextSentences"},
		{"chars per line", []string{`"maxCharsPerLine":42`, `"maxCharsPerLine":5`}, codeSettingsOutOfRange, "captions.maxCharsPerLine"},
		{"caption lines", []string{`"maxLines":2`, `"maxLines":0`}, codeSettingsOutOfRange, "captions.maxLines"},
		{"bitrate", []string{`"bitrateKbps":32`, `"bitrateKbps":128`}, codeSettingsOutOfRange, "recording.bitrateKbps"},
		{"retention negative", []string{`"retentionDays":30`, `"retentionDays":-1`}, codeSettingsOutOfRange, "recording.retentionDays"},
		{"srt port", []string{`"port":9000`, `"port":70000`}, codeSettingsOutOfRange, "srt.port"},
		{"srt latency", []string{`"latencyMs":200`, `"latencyMs":5`}, codeSettingsOutOfRange, "srt.latencyMs"},
		{"model too long", []string{`"gemma3:4b"`, `"` + strings.Repeat("m", 201) + `"`}, codeSettingsTooLong, "providers.local.gemmaModel"},
		{"mixed codes", []string{`"maxLines":2`, `"maxLines":0`, `"gemma3:4b"`, `"` + strings.Repeat("m", 201) + `"`}, codeSettingsInvalid, "captions.maxLines"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := call{"PUT", "/api/settings", settingsBody(t, tt.edits...), "", "", 400, ""}.do(t, h)
			var e api.Error
			if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
				t.Fatal(err)
			}
			if e.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", e.Code, tt.wantCode)
			}
			if e.Fields == nil || (*e.Fields)[tt.wantField] == "" {
				t.Errorf("fields = %v, want %s", e.Fields, tt.wantField)
			}
		})
	}
	// Nothing invalid was saved.
	call{"GET", "/api/settings", "", "", "", 200, `"maxLines":2`}.do(t, h)
}

func TestSettingsDefaultGlossary(t *testing.T) {
	h, st := settingsServer(t)
	for _, c := range []struct {
		name, id string
		status   int
		want     string
	}{
		{"unknown", `"nope"`, 400, `"fields":{"defaultGlossaryId":"glossary.not_found"}`},
		{"seeded", `"tech-terms"`, 200, `"defaultGlossaryId":"tech-terms"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := New()
			s.Settings, s.Glossaries = st, st
			h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
			call{"PUT", "/api/settings", settingsBody(t, `"defaultTargetLanguages"`, `"defaultGlossaryId":`+c.id+`,"defaultTargetLanguages"`), "", "", c.status, c.want}.do(t, h)
		})
	}
	// Without a glossary store the id isn't checked.
	call{"PUT", "/api/settings", settingsBody(t, `"defaultTargetLanguages"`, `"defaultGlossaryId":"nope","defaultTargetLanguages"`), "", "", 200, `"defaultGlossaryId":"nope"`}.do(t, h)
}

func TestSettingsNotImplemented(t *testing.T) {
	h := New().Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
	call{"GET", "/api/settings", "", "", "", 501, `"code":"not_implemented"`}.do(t, h)
}
