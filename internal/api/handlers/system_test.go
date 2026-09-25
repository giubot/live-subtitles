// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
)

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
