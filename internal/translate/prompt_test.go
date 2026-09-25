// SPDX-License-Identifier: Apache-2.0

package translate

import (
	"strings"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func ptr[T any](v T) *T { return &v }

func TestBuildPrompt(t *testing.T) {
	glossary := &domain.Glossary{
		Terms: []api.GlossaryTerm{
			{Term: "pod", Translations: &map[string]string{"es": "pod"}, Note: ptr("Kubernetes unit")},
			{Term: "deploy", Translations: &map[string]string{"es": "desplegar"}},
			{Term: "Nerdearla", Note: ptr("conference name")},
			{Term: "sidecar", Translations: &map[string]string{"fr": "side-car"}}, // no es translation, no note
			{Term: "cluster", Translations: &map[string]string{"es": "clúster"}},  // not in the text
		},
		DoNotTranslate: []string{"Kubernetes", "React", " kubernetes "},
	}
	tests := []struct {
		name       string
		req        domain.TranslateRequest
		sysHas     []string
		sysHasNot  []string
		userHas    []string
		userHasNot []string
	}{
		{
			name:       "final without context or glossary",
			req:        domain.TranslateRequest{Text: "Hello  everyone", From: "en", To: "es", Final: true},
			sysHas:     []string{"from English to Spanish", "translation of the caption only"},
			sysHasNot:  []string{"unfinished", "Glossary", "Never translate"},
			userHas:    []string{"Caption to translate into Spanish:\nHello everyone"},
			userHasNot: []string{"Earlier captions"},
		},
		{
			name:    "interim",
			req:     domain.TranslateRequest{Text: "and then we", From: "en", To: "es"},
			sysHas:  []string{"unfinished fragment"},
			userHas: []string{"and then we"},
		},
		{
			name:    "context",
			req:     domain.TranslateRequest{Text: "Gracias.", From: "es", To: "en", Final: true, Context: []string{"Hola a todos.", "Bienvenidos\na Nerdearla."}},
			sysHas:  []string{"from Spanish to English"},
			userHas: []string{"Earlier captions (context only):\nHola a todos.\nBienvenidos a Nerdearla.\n\nCaption to translate into English:\nGracias."},
		},
		{
			name: "glossary terms and do-not-translate that occur in the text",
			req: domain.TranslateRequest{Text: "Deploy the pod to Kubernetes at nerdearla", From: "en", To: "es", Final: true,
				Glossary: glossary},
			sysHas: []string{
				"Glossary (use these translations):",
				`- "pod" → "pod" (Kubernetes unit)`,
				`- "deploy" → "desplegar"`,
				`- "Nerdearla": conference name`,
				"copy them exactly: Kubernetes",
			},
			sysHasNot: []string{"sidecar", "clúster", "React", "Kubernetes, "},
		},
		{
			name:      "masked do-not-translate entries",
			req:       domain.TranslateRequest{Text: "⟦1⟧ on ⟦2⟧", From: "en", To: "es", Final: true, Glossary: glossary},
			sysHas:    []string{"Copy every ⟦n⟧ placeholder"},
			sysHasNot: []string{"Never translate"},
		},
		{
			name:      "terms match whole words only",
			req:       domain.TranslateRequest{Text: "Reactive rapid deployment", From: "en", To: "es", Final: true, Glossary: glossary},
			sysHasNot: []string{"Glossary", "Never translate"},
		},
		{
			name:      "unknown language code is used as is",
			req:       domain.TranslateRequest{Text: "hi", From: "en", To: "sv", Final: true},
			sysHas:    []string{"from English to sv"},
			sysHasNot: []string{"Glossary"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildPrompt(tt.req)
			for _, s := range tt.sysHas {
				if !strings.Contains(p.System, s) {
					t.Errorf("system prompt lacks %q:\n%s", s, p.System)
				}
			}
			for _, s := range tt.sysHasNot {
				if strings.Contains(p.System, s) {
					t.Errorf("system prompt has %q:\n%s", s, p.System)
				}
			}
			for _, s := range tt.userHas {
				if !strings.Contains(p.User, s) {
					t.Errorf("user prompt lacks %q:\n%s", s, p.User)
				}
			}
			for _, s := range tt.userHasNot {
				if strings.Contains(p.User, s) {
					t.Errorf("user prompt has %q:\n%s", s, p.User)
				}
			}
		})
	}
}

func TestCleanOutput(t *testing.T) {
	tests := []struct{ in, want string }{
		{"  Hola a todos.  ", "Hola a todos."},
		{`"Hola a todos."`, "Hola a todos."},
		{"“Hola”", "Hola"},
		{"«Hola»", "Hola"},
		{"Translation: Hola", "Hola"},
		{"traducción: Hola", "Hola"},
		{"Hola.\n\nNote: I kept the tone.", "Hola."},
		{`Dijo "hola" y "adiós"`, `Dijo "hola" y "adiós"`},
		{`"a" y "b"`, `"a" y "b"`},
		{"", ""},
	}
	for _, tt := range tests {
		if got := CleanOutput(tt.in); got != tt.want {
			t.Errorf("CleanOutput(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestContextSentences(t *testing.T) {
	tests := []struct {
		name string
		tr   *struct {
			ContextSentences *int `json:"contextSentences,omitempty"`
		}
		want int
	}{
		{name: "unset", want: DefaultContextSentences},
		{name: "empty", tr: &struct {
			ContextSentences *int `json:"contextSentences,omitempty"`
		}{}, want: DefaultContextSentences},
		{name: "zero", tr: &struct {
			ContextSentences *int `json:"contextSentences,omitempty"`
		}{ptr(0)}, want: 0},
		{name: "five", tr: &struct {
			ContextSentences *int `json:"contextSentences,omitempty"`
		}{ptr(5)}, want: 5},
		{name: "clamped", tr: &struct {
			ContextSentences *int `json:"contextSentences,omitempty"`
		}{ptr(50)}, want: MaxContextSentences},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContextSentences(api.Settings{Translation: tt.tr}); got != tt.want {
				t.Errorf("ContextSentences = %d, want %d", got, tt.want)
			}
		})
	}
}
