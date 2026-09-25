// SPDX-License-Identifier: Apache-2.0

package translate

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// DefaultContextSentences is how many previous final sentences go to the
// translator when the settings don't say (Settings.translation.contextSentences).
const DefaultContextSentences = 3

// MaxContextSentences is the largest context the settings allow.
const MaxContextSentences = 10

// ContextSentences reads Settings.translation.contextSentences, with the
// default when it's unset and clamped to [0, MaxContextSentences].
func ContextSentences(s api.Settings) int {
	if s.Translation == nil || s.Translation.ContextSentences == nil {
		return DefaultContextSentences
	}
	return min(max(*s.Translation.ContextSentences, 0), MaxContextSentences)
}

// Prompt is a translation request rendered for a chat model: the system
// instructions and the user turn. Every text translator (Gemini, Gemma)
// uses the same template, so quality work happens in one place.
type Prompt struct {
	System string
	User   string
}

// languageNames names the curated target languages (AI-5) for the prompt.
var languageNames = map[domain.LanguageCode]string{
	"en": "English", "es": "Spanish", "pt": "Portuguese", "fr": "French",
	"de": "German", "it": "Italian", "zh": "Chinese (Simplified)", "ja": "Japanese",
	"ko": "Korean", "ca": "Catalan", "nl": "Dutch", "pl": "Polish", "ru": "Russian",
	"uk": "Ukrainian", "ar": "Arabic", "hi": "Hindi", "tr": "Turkish",
}

// LanguageName is the English name of a language code, or the code itself.
func LanguageName(code domain.LanguageCode) string {
	if n, ok := languageNames[strings.ToLower(code)]; ok {
		return n
	}
	return code
}

// BuildPrompt renders req with the shared template: the rules, the
// glossary terms and do-not-translate entries that occur in the text
// (AI-7), the previous final sentences as context, and the caption.
func BuildPrompt(req domain.TranslateRequest) Prompt {
	from, to := LanguageName(req.From), LanguageName(req.To)
	var sys strings.Builder
	fmt.Fprintf(&sys, "You translate live captions of a talk from %s to %s.\n", from, to)
	sys.WriteString("Rules:\n")
	sys.WriteString("- Reply with the translation of the caption only: no quotes, labels, notes or the original text.\n")
	sys.WriteString("- Keep the meaning, tone and register, and keep it about as short as the original; it is shown as a subtitle.\n")
	sys.WriteString("- Keep names, product names, code, numbers and units as they are unless the glossary says otherwise.\n")
	sys.WriteString("- The caption comes from speech recognition and may contain mistakes; translate what the speaker meant.\n")
	sys.WriteString("- Earlier captions are context only; never translate or repeat them.\n")
	if strings.Contains(req.Text, "⟦") {
		sys.WriteString(placeholderRule + "\n")
	}
	if !req.Final {
		sys.WriteString("- The caption is an unfinished fragment that is still being spoken: translate only what is there, don't complete it.\n")
	}
	if terms := glossaryTerms(req.Glossary, req.Text, req.To); len(terms) > 0 {
		sys.WriteString("Glossary (use these translations):\n")
		for _, t := range terms {
			sys.WriteString("- " + t + "\n")
		}
	}
	if keep := doNotTranslate(req.Glossary, req.Text); len(keep) > 0 {
		sys.WriteString("Never translate these terms; copy them exactly: " + strings.Join(keep, ", ") + "\n")
	}

	var user strings.Builder
	if len(req.Context) > 0 {
		user.WriteString("Earlier captions (context only):\n")
		for _, s := range req.Context {
			user.WriteString(oneLine(s) + "\n")
		}
		user.WriteString("\n")
	}
	fmt.Fprintf(&user, "Caption to translate into %s:\n%s", to, oneLine(req.Text))
	return Prompt{System: strings.TrimRight(sys.String(), "\n"), User: user.String()}
}

// glossaryTerms renders the glossary entries whose term occurs in text (as
// a whole word, ignoring case) and
// that have a translation into to or a note.
func glossaryTerms(g *domain.Glossary, text string, to domain.LanguageCode) []string {
	if g == nil {
		return nil
	}
	var out []string
	for _, t := range g.Terms {
		if t.Term == "" || !containsTerm(text, t.Term) {
			continue
		}
		var tr string
		if t.Translations != nil {
			tr = (*t.Translations)[to]
		}
		note := ""
		if t.Note != nil {
			note = strings.TrimSpace(*t.Note)
		}
		switch {
		case tr != "" && note != "":
			out = append(out, fmt.Sprintf("%q → %q (%s)", t.Term, tr, note))
		case tr != "":
			out = append(out, fmt.Sprintf("%q → %q", t.Term, tr))
		case note != "":
			out = append(out, fmt.Sprintf("%q: %s", t.Term, note))
		}
	}
	return out
}

// doNotTranslate lists the glossary's do-not-translate entries that occur
// in text.
func doNotTranslate(g *domain.Glossary, text string) []string {
	if g == nil {
		return nil
	}
	var out []string
	for _, k := range g.DoNotTranslate {
		k = strings.TrimSpace(k)
		if k != "" && containsTerm(text, k) && !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, k) }) {
			out = append(out, k)
		}
	}
	return out
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// CleanOutput tidies a model reply: it keeps the first non-empty line and
// strips labels and quotes that small models sometimes add.
func CleanOutput(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	for _, label := range []string{"Translation:", "Traducción:"} {
		if len(s) >= len(label) && strings.EqualFold(s[:len(label)], label) {
			s = strings.TrimSpace(s[len(label):])
		}
	}
	for _, q := range [][2]string{{`"`, `"`}, {"“", "”"}, {"«", "»"}, {"'", "'"}} {
		if len(s) > len(q[0])+len(q[1]) && strings.HasPrefix(s, q[0]) && strings.HasSuffix(s, q[1]) {
			inner := s[len(q[0]) : len(s)-len(q[1])]
			if !strings.ContainsAny(inner, q[0]+q[1]) {
				s = strings.TrimSpace(inner)
			}
		}
	}
	return strings.TrimFunc(s, unicode.IsSpace)
}
