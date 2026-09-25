// SPDX-License-Identifier: Apache-2.0

//go:build gemini

package gemini

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// TestTranslatorLive calls the real Gemini API:
//
//	GEMINI_API_KEY=… go test -tags gemini -run TestTranslatorLive ./internal/provider/gemini/
//
// GEMINI_TRANSLATION_MODEL overrides the model.
func TestTranslatorLive(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		t.Skip("set GEMINI_API_KEY or GOOGLE_API_KEY")
	}
	tr := &Translator{
		APIKey: func(context.Context) (string, error) { return key, nil },
		Settings: func(context.Context) (api.Settings, error) {
			var s api.Settings
			s.Providers.Gemini.TranslationModel = os.Getenv("GEMINI_TRANSLATION_MODEL")
			return s, nil
		},
	}
	tests := []struct {
		req  domain.TranslateRequest
		want string // a word the translation should contain
	}{
		{domain.TranslateRequest{Text: "Kubernetes schedules pods across the nodes of the cluster.", From: "en", To: "es", Final: true,
			Glossary: &domain.Glossary{DoNotTranslate: []string{"Kubernetes"}}}, "Kubernetes"},
		{domain.TranslateRequest{Text: "Gracias a todos por venir a Nerdearla.", From: "es", To: "en", Final: true}, "Nerdearla"},
	}
	for _, tt := range tests {
		start := time.Now()
		var partials int
		res, err := tr.TranslateStream(t.Context(), tt.req, func(string) { partials++ })
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s→%s in %v (%d partials, %+v): %q", tt.req.From, tt.req.To, time.Since(start), partials, res.Usage, res.Text)
		if !strings.Contains(res.Text, tt.want) {
			t.Errorf("translation %q lacks %q", res.Text, tt.want)
		}
	}
}
