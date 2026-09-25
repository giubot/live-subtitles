// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/bus"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/store"
)

func captionsServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	return captionsServerWithBus(t, nil)
}

// captionsServerWithBus is captionsServer with corrections published on b.
func captionsServerWithBus(t *testing.T, b domain.CaptionBus) (http.Handler, *store.Store) {
	t.Helper()
	ctx := t.Context()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now()
	if err := st.CreateSession(ctx, domain.Session{Id: "main", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	hidden := true
	for i, text := range []string{"Hola a todos.", "Esto está oculto.", "Hoy hablamos de <observabilidad> & métricas."} {
		c := domain.CaptionEvent{SessionId: "main", Lang: "es", SegmentId: fmt.Sprintf("s-%d", i+1), Final: true,
			Start: float32(2 * i), End: float32(2*i + 2), Text: text, SourceLang: "en"}
		if i == 1 {
			c.Hidden = &hidden
		}
		if err := st.SaveCaption(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SaveCaption(ctx, domain.CaptionEvent{SessionId: "main", Lang: "en", SegmentId: "s-1", Final: true,
		Start: 0, End: 2, Text: "Hello everyone.", SourceLang: "en"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Sessions, s.Captions, s.Settings, s.CaptionEdits, s.CaptionBus = st, st, st, st, b
	return s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler)), st
}

func get(t *testing.T, h http.Handler, path string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	return res, string(body)
}

func TestSubtitles(t *testing.T) {
	h, _ := captionsServer(t)
	const base = "/api/public/sessions/main/subtitles?lang=es"
	tests := []struct {
		name, query     string
		wantStatus      int
		wantType        string
		wantBody        []string
		wantNot         []string
		wantCache       string
		wantDisposition string
	}{
		{"vtt export", "&format=vtt", 200, "text/vtt",
			[]string{"WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nHola a todos.\n", "&lt;observabilidad&gt; &amp; métricas."},
			[]string{"oculto"}, "", `attachment; filename="main-es.vtt"`},
		{"srt live", "&format=srt&live=true", 200, "application/x-subrip",
			[]string{"1\n00:00:00,000 --> 00:00:02,000\nHola a todos.\n\n2\n00:00:04,000"}, []string{"oculto"}, "no-store", ""},
		{"txt", "&format=txt", 200, "text/plain", []string{string(rune(0xFEFF)) + "Hola a todos.\nHoy hablamos"}, []string{"oculto"}, "", `attachment; filename="main-es.txt"`},
		{"json", "&format=json&live=true", 200, "application/json", []string{`"segmentId":"s-3"`}, []string{"oculto"}, "no-store", ""},
		{"other track", "&format=vtt&lang=en", 200, "text/vtt", []string{"Hello everyone."}, []string{"Hola"}, "", ""},
		{"bad format", "&format=ass", 400, "application/json", []string{`"code":"request.invalid"`}, nil, "", ""},
		{"recording is P3-08", "&format=vtt&recordingId=r1", 501, "application/json", []string{`"code":"not_implemented"`}, nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := base + tt.query
			if strings.Contains(tt.query, "lang=en") {
				path = "/api/public/sessions/main/subtitles?" + strings.TrimPrefix(tt.query, "&")
			}
			res, body := get(t, h, path)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d: %s", res.StatusCode, tt.wantStatus, body)
			}
			if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, tt.wantType) {
				t.Errorf("Content-Type %q, want %q", ct, tt.wantType)
			}
			for _, w := range tt.wantBody {
				if !strings.Contains(body, w) {
					t.Errorf("body %q lacks %q", body, w)
				}
			}
			for _, w := range tt.wantNot {
				if strings.Contains(body, w) {
					t.Errorf("body %q contains %q", body, w)
				}
			}
			if got := res.Header.Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control %q, want %q", got, tt.wantCache)
			}
			if tt.wantDisposition != "" && res.Header.Get("Content-Disposition") != tt.wantDisposition {
				t.Errorf("Content-Disposition %q, want %q", res.Header.Get("Content-Disposition"), tt.wantDisposition)
			}
		})
	}
}

func TestSubtitlesUseSettings(t *testing.T) {
	h, st := captionsServer(t)
	chars, lines := 12, 2
	var set api.Settings
	set.Captions = &struct {
		MaxCharsPerLine *int `json:"maxCharsPerLine,omitempty"`
		MaxLines        *int `json:"maxLines,omitempty"`
	}{&chars, &lines}
	if err := st.PutSettings(t.Context(), set); err != nil {
		t.Fatal(err)
	}
	// "Hola a todos." is 13 characters: with 12 per line it wraps.
	_, body := get(t, h, "/api/public/sessions/main/subtitles?lang=es&format=vtt")
	if !strings.Contains(body, "\nHola a\ntodos.\n") {
		t.Errorf("caption not wrapped at 12 characters:\n%s", body)
	}
}

func TestSubtitlesFilenameIsSafe(t *testing.T) {
	h, _ := captionsServer(t)
	res, _ := get(t, h, `/api/public/sessions/main/subtitles?lang=es%22%0d%0aX:1&format=vtt`)
	if d := res.Header.Get("Content-Disposition"); d != `attachment; filename="main-es_X_1.vtt"` {
		t.Errorf("Content-Disposition %q", d)
	}
}

func TestListCaptions(t *testing.T) {
	h, _ := captionsServer(t)
	res, body := get(t, h, "/api/public/sessions/main/captions?lang=es&limit=1")
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	var page api.CaptionPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].SegmentId != "s-1" || page.NextCursor == nil {
		t.Fatalf("first page %s", body)
	}
	// Page 2 holds only the hidden caption, which is filtered out.
	var texts []string
	cursor := *page.NextCursor
	for cursor != "" {
		_, body := get(t, h, "/api/public/sessions/main/captions?lang=es&limit=1&after="+cursor)
		var p api.CaptionPage
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		for _, c := range p.Items {
			texts = append(texts, c.Text)
		}
		cursor = ""
		if p.NextCursor != nil {
			cursor = *p.NextCursor
		}
	}
	if len(texts) != 1 || !strings.HasPrefix(texts[0], "Hoy hablamos") {
		t.Errorf("remaining pages %q", texts)
	}

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/api/public/sessions/nope/captions?lang=es", 404, `"code":"session.not_found"`},
		{"/api/public/sessions/nope/subtitles?lang=es&format=vtt", 404, `"code":"session.not_found"`},
		{"/api/public/sessions/main/captions?lang=es&after=%25%25", 400, `"code":"request.invalid"`},
		{"/api/public/sessions/main/captions?lang=fr", 200, `{"items":[]}`},
	}
	for _, tt := range tests {
		res, body := get(t, h, tt.path)
		if res.StatusCode != tt.wantStatus || !strings.Contains(body, tt.wantBody) {
			t.Errorf("%s: %d %s, want %d %s", tt.path, res.StatusCode, body, tt.wantStatus, tt.wantBody)
		}
	}
}

func patch(t *testing.T, h http.Handler, path, body string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

// captionsOnly passes on the next caption message of ch, skipping the
// others (such as viewers counts), without blocking the caller.
func captionsOnly(ch <-chan domain.BusMessage) <-chan domain.BusMessage {
	out := make(chan domain.BusMessage, 1)
	for {
		select {
		case m := <-ch:
			if m.Type == api.CaptionsServerMessageTypeCaption {
				out <- m
				return out
			}
		default:
			return out
		}
	}
}

func TestPatchCaption(t *testing.T) {
	b := bus.New()
	h, _ := captionsServerWithBus(t, b)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	live := b.Subscribe(ctx, "main", []string{"es"})
	if m := <-live; m.Type != api.CaptionsServerMessageTypeHistory {
		t.Fatalf("first message %+v", m)
	}

	// The cases run in order against the same session.
	tests := []struct {
		name, path, body string
		wantStatus       int
		wantBody         string
		// wantLive is the text of the broadcast caption; empty: none.
		wantLive       string
		wantLiveHidden bool
	}{
		{"edit text", "/api/sessions/main/captions/s-1?lang=es", `{"text":"  Hola a todas.  "}`, 200,
			`"edited":true`, "Hola a todas.", false},
		{"hide", "/api/sessions/main/captions/s-3?lang=es", `{"hidden":true}`, 200,
			`"hidden":true`, "Hoy hablamos de <observabilidad> & métricas.", true},
		{"unhide", "/api/sessions/main/captions/s-2?lang=es", `{"hidden":false}`, 200,
			`"text":"Esto está oculto."`, "Esto está oculto.", false},
		{"other track", "/api/sessions/main/captions/s-1?lang=en", `{"text":"Hello all."}`, 200,
			`"lang":"en"`, "", false},
		{"missing session", "/api/sessions/nope/captions/s-1?lang=es", `{"text":"x"}`, 404,
			`"code":"session.not_found"`, "", false},
		{"missing segment", "/api/sessions/main/captions/s-9?lang=es", `{"text":"x"}`, 404,
			`"code":"caption.not_found"`, "", false},
		{"missing track", "/api/sessions/main/captions/s-1?lang=fr", `{"text":"x"}`, 404,
			`"code":"caption.not_found"`, "", false},
		{"empty patch", "/api/sessions/main/captions/s-1?lang=es", `{}`, 400, `"code":"request.invalid"`, "", false},
		{"blank text", "/api/sessions/main/captions/s-1?lang=es", `{"text":" "}`, 400, `"code":"request.invalid"`, "", false},
		{"no lang", "/api/sessions/main/captions/s-1", `{"text":"x"}`, 400, `"code":"request.invalid"`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, body := patch(t, h, tt.path, tt.body)
			if res.StatusCode != tt.wantStatus || !strings.Contains(body, tt.wantBody) {
				t.Fatalf("%d %s, want %d %s", res.StatusCode, body, tt.wantStatus, tt.wantBody)
			}
			select {
			case m := <-captionsOnly(live):
				c := m.Caption
				if tt.wantLive == "" || c == nil || c.Text != tt.wantLive || c.Edited == nil || !*c.Edited ||
					(c.Hidden != nil && *c.Hidden) != tt.wantLiveHidden {
					t.Errorf("broadcast %+v, want text %q hidden %v", m, tt.wantLive, tt.wantLiveHidden)
				}
			case <-time.After(50 * time.Millisecond):
				if tt.wantLive != "" {
					t.Errorf("no broadcast, want %q", tt.wantLive)
				}
			}
		})
	}

	// Exports and replay show the corrections and leave out hidden lines;
	// so does the history a new viewer gets.
	_, vtt := get(t, h, "/api/public/sessions/main/subtitles?lang=es&format=vtt")
	for _, want := range []string{"Hola a todas.", "Esto está oculto."} {
		if !strings.Contains(vtt, want) {
			t.Errorf("vtt lacks %q:\n%s", want, vtt)
		}
	}
	if strings.Contains(vtt, "observabilidad") || strings.Contains(vtt, "Hola a todos.") {
		t.Errorf("vtt shows a hidden or uncorrected line:\n%s", vtt)
	}
	_, list := get(t, h, "/api/public/sessions/main/captions?lang=en")
	if !strings.Contains(list, `"text":"Hello all."`) {
		t.Errorf("captions %s", list)
	}
}

func TestPatchCaptionNeedsEditor(t *testing.T) {
	s := New()
	h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
	res, body := patch(t, h, "/api/sessions/main/captions/s-1?lang=es", `{"text":"x"}`)
	if res.StatusCode != 501 {
		t.Errorf("%d %s, want 501", res.StatusCode, body)
	}
}
