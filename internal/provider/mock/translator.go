// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"strings"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Translator is a domain.Translator that knows the script's translations.
// Interim text that is a prefix of a script line gets the same share of the
// translated line's words. Unknown EN/ES text comes back tagged, e.g.
// "[es] hello".
type Translator struct {
	// Script defaults to DefaultScript.
	Script []Line
}

var _ domain.Translator = (*Translator)(nil)

func (t *Translator) Kind() domain.ProviderKind { return api.ProviderKindMock }

func (t *Translator) Translate(ctx context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.TranslateResult{}, err
	}
	if req.From == req.To {
		return domain.TranslateResult{Text: req.Text}, nil
	}
	script := t.Script
	if len(script) == 0 {
		script = DefaultScript
	}
	words := strings.Fields(req.Text)
	for _, l := range script {
		from, to, ok := pick(l, req.From, req.To)
		if !ok {
			continue
		}
		src := strings.Fields(from)
		if len(words) == 0 || len(words) > len(src) || !equalWords(words, src[:len(words)]) {
			continue
		}
		dst := strings.Fields(to)
		n := max(1, len(dst)*len(words)/len(src))
		return domain.TranslateResult{Text: strings.Join(dst[:n], " ")}, nil
	}
	return domain.TranslateResult{Text: "[" + req.To + "] " + req.Text}, nil
}

func pick(l Line, from, to domain.LanguageCode) (string, string, bool) {
	switch {
	case from == "en" && to == "es":
		return l.EN, l.ES, true
	case from == "es" && to == "en":
		return l.ES, l.EN, true
	}
	return "", "", false
}

func equalWords(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
