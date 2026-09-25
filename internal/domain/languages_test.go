// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
)

func TestLanguageCatalog(t *testing.T) {
	for _, c := range []struct {
		code           string
		target, source bool
	}{
		{"es", true, true},
		{"en", true, true},
		{"pt", true, false},
		{"ko", true, false},
		{"ru", false, false},
		{"", false, false},
		{"auto", false, true}, // auto is a source choice, not a track
	} {
		if got := SupportedTarget(c.code); got != c.target {
			t.Errorf("SupportedTarget(%q) = %v, want %v", c.code, got, c.target)
		}
		if got := SupportedSource(api.SourceLanguage(c.code)); got != c.source {
			t.Errorf("SupportedSource(%q) = %v, want %v", c.code, got, c.source)
		}
	}
	l := Languages()
	l[0].Name = "changed"
	if got, _ := LookupLanguage("es"); got.Name != "Spanish" {
		t.Error("Languages() doesn't return a copy")
	}
}
