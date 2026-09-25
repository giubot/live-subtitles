// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/store"
	"github.com/iencodev/live-subtitles/internal/streamcc"
)

func TestStreamCaptionsHandlers(t *testing.T) {
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fail") != "" {
			http.Error(w, "nope", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, time.Now().UTC().Format("2006-01-02T15:04:05.000"))
	}))
	defer yt.Close()
	const name = "session:main:youtube_url"
	url := yt.URL + "/closedcaption?cid=abcd-efgh-1234"
	stored := func() *fakeSecrets { return &fakeSecrets{values: map[string]string{name: url}} }
	empty := func() *fakeSecrets { return &fakeSecrets{values: map[string]string{}} }

	tests := []struct {
		name, method, path, body string
		secrets                  *fakeSecrets
		noService                bool
		wantStatus               int
		wantBody                 []string
	}{
		{"put", "PUT", "/api/sessions/main/stream-captions/youtube-url", `{"value":"` + url + `"}`, empty(), false, 200,
			[]string{`"name":"youtube_caption_url"`, `"set":true`, `"hint":"••••1234"`}},
		{"put empty", "PUT", "/api/sessions/main/stream-captions/youtube-url", `{"value":""}`, empty(), false, 400,
			[]string{`"code":"secret.value_required"`}},
		{"put not a URL", "PUT", "/api/sessions/main/stream-captions/youtube-url", `{"value":"abcd-efgh-1234"}`, empty(), false, 400,
			[]string{`"code":"streamcc.url_invalid"`}},
		{"put env", "PUT", "/api/sessions/main/stream-captions/youtube-url", `{"value":"` + url + `"}`,
			&fakeSecrets{values: map[string]string{}, env: map[string]string{name: url}}, false, 422, []string{`"code":"secret.read_only_env"`}},
		{"put no backend", "PUT", "/api/sessions/main/stream-captions/youtube-url", `{"value":"` + url + `"}`,
			&fakeSecrets{values: map[string]string{}, noBackend: true}, false, 422, []string{`"code":"secret.no_backend"`}},
		{"put unknown session", "PUT", "/api/sessions/nope/stream-captions/youtube-url", `{"value":"` + url + `"}`, empty(), false, 404,
			[]string{`"code":"session.not_found"`}},
		{"delete", "DELETE", "/api/sessions/main/stream-captions/youtube-url", "", stored(), false, 204, nil},
		{"delete missing", "DELETE", "/api/sessions/main/stream-captions/youtube-url", "", empty(), false, 404,
			[]string{`"code":"secret.not_found"`}},
		{"delete env", "DELETE", "/api/sessions/main/stream-captions/youtube-url", "",
			&fakeSecrets{values: map[string]string{}, env: map[string]string{name: url}}, false, 409, []string{`"code":"secret.read_only_env"`}},
		{"delete unknown session", "DELETE", "/api/sessions/nope/stream-captions/youtube-url", "", stored(), false, 404,
			[]string{`"code":"session.not_found"`}},
		{"test", "POST", "/api/sessions/main/stream-captions/test", `{"text":"Probando"}`, stored(), false, 200,
			[]string{`"state":"ok"`, `"lastSeq":1`, `"target":"youtube_http"`}},
		{"test without body", "POST", "/api/sessions/main/stream-captions/test", "", stored(), false, 200,
			[]string{`"state":"ok"`}},
		{"test rejected", "POST", "/api/sessions/main/stream-captions/test", `{}`,
			&fakeSecrets{values: map[string]string{name: url + "&fail=1"}}, false, 200, []string{`"state":"error"`, `"code":"streamcc.rejected"`}},
		{"test without URL", "POST", "/api/sessions/main/stream-captions/test", `{}`, empty(), false, 422,
			[]string{`"code":"streamcc.no_url"`}},
		{"test unknown session", "POST", "/api/sessions/nope/stream-captions/test", `{}`, stored(), false, 404,
			[]string{`"code":"session.not_found"`}},
		{"session shows the URL is set", "GET", "/api/sessions/main", "", stored(), false, 200,
			[]string{`"youtubeUrlSet":true`}},
		{"session shows the URL is not set", "GET", "/api/sessions/main", "", empty(), false, 200,
			[]string{`"youtubeUrlSet":false`}},
		{"no service", "POST", "/api/sessions/main/stream-captions/test", `{}`, stored(), true, 501,
			[]string{`"code":"not_implemented"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			now := time.Now()
			if err := st.CreateSession(t.Context(), domain.Session{Id: "main", Name: "Main", CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			s := New()
			s.Sessions = st
			if !tt.noService {
				cc := streamcc.New(streamcc.Options{Secrets: tt.secrets, Logger: slog.New(slog.DiscardHandler)})
				defer cc.Close()
				s.StreamCaptions = cc
			}
			h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.path, body)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			got := rec.Body.String()
			for _, want := range tt.wantBody {
				if !strings.Contains(got, want) {
					t.Errorf("body %s lacks %s", got, want)
				}
			}
			if strings.Contains(got, "cid=") || strings.Contains(got, yt.URL) {
				t.Errorf("the ingestion URL leaked: %s", got)
			}
		})
	}
}
