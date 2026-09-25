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
	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/store"
)

func overlaysServer(t *testing.T, withAuth bool) http.Handler {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New()
	s.OverlayPresets = st
	if withAuth {
		s.Auth = auth.New(st, auth.Options{AdminToken: "admin-token"})
	}
	return s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
}

func TestOverlayPresetEndpoints(t *testing.T) {
	h := overlaysServer(t, false)
	box := `{"name":"Caja clásica","style":{"fontSizePx":50,"color":"#FFFFFF","background":"rgba(0,0,0,0.5)"}}`
	for _, c := range []call{
		{"GET", "/api/overlay-presets", "", "", "", 200, `[{"builtIn":true,"id":"classic","name":"Classic box"`},
		{"GET", "/api/overlay-presets/lower-third", "", "", "", 200, `"align":"left"`},
		{"GET", "/api/overlay-presets/nope", "", "", "", 404, `"code":"overlay.not_found"`},
		{"POST", "/api/overlay-presets", box, "", "", 201, `"id":"caja-clasica"`},
		{"POST", "/api/overlay-presets", box, "", "", 201, `"id":"caja-clasica-2"`},
		// A name that would take a built-in id gets a suffix.
		{"POST", "/api/overlay-presets", `{"name":"Classic","style":{}}`, "", "", 201, `"id":"classic-2"`},
		{"POST", "/api/overlay-presets", `{"name":"中文","style":{}}`, "", "", 201, `"id":"preset"`},
		{"GET", "/api/overlay-presets/caja-clasica", "", "", "", 200, `"builtIn":false`},
		{"PUT", "/api/overlay-presets/caja-clasica", `{"name":" Box ","style":{"maxLines":3}}`, "", "", 200, `{"builtIn":false,"id":"caja-clasica","name":"Box","style":{"maxLines":3}}`},
		{"GET", "/api/overlay-presets/caja-clasica", "", "", "", 200, `"name":"Box"`},
		{"PUT", "/api/overlay-presets/nope", `{"name":"X","style":{}}`, "", "", 404, `"code":"overlay.not_found"`},
		{"PUT", "/api/overlay-presets/classic", `{"name":"X","style":{}}`, "", "", 409, `"code":"overlay.builtin_read_only"`},
		{"DELETE", "/api/overlay-presets/outline", "", "", "", 409, `"code":"overlay.builtin_read_only"`},
		{"DELETE", "/api/overlay-presets/caja-clasica", "", "", "", 204, ""},
		{"DELETE", "/api/overlay-presets/caja-clasica", "", "", "", 404, `"code":"overlay.not_found"`},
	} {
		c.do(t, h)
	}

	res := call{"GET", "/api/overlay-presets", "", "", "", 200, ""}.do(t, h)
	var list []api.OverlayPreset
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range list {
		ids = append(ids, p.Id)
	}
	// Built-ins first, then saved presets by name.
	if got, want := strings.Join(ids, ","), "classic,outline,lower-third,caja-clasica-2,classic-2,preset"; got != want {
		t.Errorf("ids = %s, want %s", got, want)
	}
}

func TestOverlayPresetAuth(t *testing.T) {
	h := overlaysServer(t, true)
	for _, c := range []call{
		{"GET", "/api/overlay-presets", "", "", "", 200, `"id":"classic"`},
		{"GET", "/api/overlay-presets/classic", "", "", "", 200, `"id":"classic"`},
		{"POST", "/api/overlay-presets", `{"name":"A","style":{}}`, "", "", 401, `"code":"auth.required"`},
		{"POST", "/api/overlay-presets", `{"name":"A","style":{}}`, "", "admin-token", 201, `"id":"a"`},
		{"PUT", "/api/overlay-presets/a", `{"name":"A","style":{}}`, "", "", 401, `"code":"auth.required"`},
		{"DELETE", "/api/overlay-presets/a", "", "", "", 401, `"code":"auth.required"`},
	} {
		c.do(t, h)
	}
}

func TestOverlayPresetValidation(t *testing.T) {
	h := overlaysServer(t, false)
	tests := []struct {
		name, body string
		wantCode   string
		wantField  string
	}{
		{"empty name", `{"name":"  ","style":{}}`, codeOverlayName, "name"},
		{"long name", `{"name":"` + strings.Repeat("n", 81) + `","style":{}}`, codeOverlayName, "name"},
		{"font size", `{"name":"A","style":{"fontSizePx":8}}`, codeOverlayOutOfRange, "style.fontSizePx"},
		{"max lines", `{"name":"A","style":{"maxLines":5}}`, codeOverlayOutOfRange, "style.maxLines"},
		{"fade", `{"name":"A","style":{"fadeAfterMs":-1}}`, codeOverlayOutOfRange, "style.fadeAfterMs"},
		{"colour escapes css", `{"name":"A","style":{"color":"red;}body{x:y"}}`, codeOverlayColor, "style.color"},
		{"background quote", `{"name":"A","style":{"background":"url('x')"}}`, codeOverlayColor, "style.background"},
		{"font family", `{"name":"A","style":{"fontFamily":"a;b"}}`, "request.invalid", "style.fontFamily"},
		{"position", `{"name":"A","style":{"position":"middle"}}`, "request.invalid", "style.position"},
		{"align", `{"name":"A","style":{"align":"justify"}}`, "request.invalid", "style.align"},
		{"mixed", `{"name":"","style":{"maxLines":9}}`, codeOverlayInvalid, "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := call{"POST", "/api/overlay-presets", tt.body, "", "", 400, ""}.do(t, h)
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
	// The built-in styles pass the same checks.
	for _, p := range builtinOverlayPresets() {
		if bad := validateOverlayPreset(api.OverlayPresetInput{Name: p.Name, Style: p.Style}); len(bad) > 0 {
			t.Errorf("built-in %s is invalid: %v", p.Id, bad)
		}
	}
}

func TestOverlayPresetSlug(t *testing.T) {
	tests := []struct{ name, want string }{
		{"Classic box", "classic-box"},
		{"  Lower—third!! ", "lower-third"},
		{"Caja clásica Ñandú", "caja-clasica-nandu"},
		{"100% OBS", "100-obs"},
		{"日本語", "preset"},
		{strings.Repeat("ab ", 40), strings.TrimRight(strings.Repeat("ab-", 16), "-")},
	}
	for _, tt := range tests {
		if got := overlayPresetSlug(tt.name); got != tt.want {
			t.Errorf("slug(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestOverlayPresetsWithoutStore(t *testing.T) {
	h := New().Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
	for _, c := range []call{
		{"GET", "/api/overlay-presets", "", "", "", 200, `"id":"lower-third"`},
		{"GET", "/api/overlay-presets/outline", "", "", "", 200, `"background":"transparent"`},
		{"GET", "/api/overlay-presets/mine", "", "", "", 404, `"code":"overlay.not_found"`},
		{"POST", "/api/overlay-presets", `{"name":"A","style":{}}`, "", "", 501, `"code":"not_implemented"`},
	} {
		c.do(t, h)
	}
}
