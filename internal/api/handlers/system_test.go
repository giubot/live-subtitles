// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/hwcheck"
)

type fakeHardware struct {
	runtimes hwcheck.Runtimes
	bench    api.BenchmarkResult
	err      error
}

func (f fakeHardware) Report(context.Context) api.HardwareReport {
	r := api.HardwareReport{Os: "linux", Arch: "amd64"}
	r.Recommendation.WhisperModel = "small"
	return r
}
func (f fakeHardware) Benchmark(context.Context) (api.BenchmarkResult, error) { return f.bench, f.err }
func (f fakeHardware) Runtimes(context.Context) hwcheck.Runtimes              { return f.runtimes }

type fakeSettingsStore struct{ err error }

func (f fakeSettingsStore) Settings(context.Context) (api.Settings, error) {
	return api.Settings{}, f.err
}
func (f fakeSettingsStore) PutSettings(context.Context, api.Settings) error { return nil }

func TestSystemHandlers(t *testing.T) {
	up := api.RuntimeStatus{Reachable: true}
	allUp := hwcheck.Runtimes{Whisper: up, Ollama: up, FFmpeg: up, SRT: true}
	noSidecars := hwcheck.Runtimes{FFmpeg: up}
	noFFmpeg := hwcheck.Runtimes{Whisper: up, Ollama: up}
	coded := &domain.CodedError{Code: hwcheck.CodeBenchmarkUnavailable, Message: "whisper down", Params: map[string]any{"runtime": "whisper"}}
	for _, c := range []struct {
		name     string
		setup    func(s *Server)
		method   string
		path     string
		status   int
		wantBody []string
	}{
		{"health bare", func(*Server) {}, "GET", "/healthz", 200, []string{`{"status":"ok"}`}},
		{"health all up", func(s *Server) {
			s.Hardware, s.Settings = fakeHardware{runtimes: allUp}, fakeSettingsStore{err: domain.ErrNotFound}
		}, "GET", "/healthz", 200, []string{`"status":"ok"`, `"database":"ok"`, `"whisper":"ok"`, `"ollama":"ok"`, `"ffmpeg":"ok"`}},
		{"health without sidecars is ok", func(s *Server) { s.Hardware = fakeHardware{runtimes: noSidecars} },
			"GET", "/healthz", 200, []string{`"status":"ok"`, `"whisper":"unreachable"`}},
		{"health without ffmpeg is degraded", func(s *Server) { s.Hardware = fakeHardware{runtimes: noFFmpeg} },
			"GET", "/healthz", 200, []string{`"status":"degraded"`, `"ffmpeg":"unreachable"`}},
		{"health database error", func(s *Server) { s.Settings = fakeSettingsStore{err: errors.New("disk I/O")} },
			"GET", "/healthz", 200, []string{`"status":"degraded"`, `"database":"error"`}},
		{"info bare", func(*Server) {}, "GET", "/api/system/info", 200,
			[]string{`"version":"dev"`, `"mode":"dev"`, `"srtIngest":false`, `"recording":false`, `"tls":false`}},
		{"info features", func(s *Server) {
			s.Hardware, s.TLS = fakeHardware{runtimes: allUp}, fakeTLS{info: api.TlsInfo{Enabled: true}}
			s.Build = BuildInfo{Version: "0.3.0", Commit: "abc1234", Mode: api.Edge}
		}, "GET", "/api/system/info", 200,
			[]string{`"version":"0.3.0"`, `"commit":"abc1234"`, `"mode":"edge"`, `"srtIngest":true`, `"recording":true`, `"tls":true`}},
		{"hardware 501", func(*Server) {}, "GET", "/api/system/hardware", 501, []string{"not_implemented"}},
		{"hardware", func(s *Server) { s.Hardware = fakeHardware{} }, "GET", "/api/system/hardware", 200,
			[]string{`"os":"linux"`, `"whisperModel":"small"`}},
		{"benchmark ok", func(s *Server) {
			s.Hardware = fakeHardware{bench: api.BenchmarkResult{RealTimeFactor: 0.25, Ok: true}}
		}, "POST", "/api/system/benchmark", 200, []string{`"realTimeFactor":0.25`, `"ok":true`}},
		{"benchmark busy", func(s *Server) { s.Hardware = fakeHardware{err: hwcheck.ErrBusy} },
			"POST", "/api/system/benchmark", 409, []string{`"code":"benchmark.running"`}},
		{"benchmark unavailable", func(s *Server) { s.Hardware = fakeHardware{err: coded} },
			"POST", "/api/system/benchmark", 422, []string{`"code":"benchmark.runtime_unavailable"`, `"runtime":"whisper"`}},
		{"benchmark internal", func(s *Server) { s.Hardware = fakeHardware{err: errors.New("boom")} },
			"POST", "/api/system/benchmark", 500, []string{`"code":"internal"`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := New()
			c.setup(s)
			h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
			res := call{c.method, c.path, "", "", "", c.status, ""}.do(t, h)
			body, _ := io.ReadAll(res.Body)
			for _, w := range c.wantBody {
				if !strings.Contains(string(body), w) {
					t.Errorf("body %s lacks %s", body, w)
				}
			}
		})
	}
}

func TestListLanguages(t *testing.T) {
	h := New().Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
	res := call{"GET", "/api/languages", "", "", "", 200, `"code":"es"`}.do(t, h)
	var got []api.Language
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	byCode := map[string]api.Language{}
	for _, l := range got {
		byCode[l.Code] = l
	}
	for _, c := range []struct {
		code, name, native string
		source             bool
	}{
		{"es", "Spanish", "Español", true},
		{"en", "English", "English", true},
		{"pt", "Portuguese", "Português", false},
		{"fr", "French", "Français", false},
		{"de", "German", "Deutsch", false},
		{"it", "Italian", "Italiano", false},
		{"zh", "Chinese", "中文", false},
		{"ja", "Japanese", "日本語", false},
		{"ko", "Korean", "한국어", false},
	} {
		l, ok := byCode[c.code]
		if !ok || l.Name != c.name || l.NativeName != c.native || l.CanBeSource != c.source {
			t.Errorf("%s = %+v (present %v), want %+v", c.code, l, ok, c)
		}
	}
	if len(got) != 9 {
		t.Errorf("got %d languages, want 9", len(got))
	}
}
