// SPDX-License-Identifier: Apache-2.0

package translate

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// Do-not-translate enforcement (AI-7). The prompt asks the model to copy
// the glossary's do-not-translate entries, but models still translate or
// re-case them now and then ("Kubernetes" → "kubernetes", "React" →
// "Reaccionar"). Every translation is checked against the entries found
// in its source text:
//
//   - An entry that is there with other casing gets its glossary spelling
//     back. This is free, so interims and streamed partials get it too.
//   - An entry that is gone from a final is repaired by translating the
//     caption once more with the entries masked as ⟦n⟧ placeholders,
//     which models copy reliably, and putting the entries back. If the
//     placeholders don't survive either, the first translation is kept.

// placeholder is the mask of the n-th do-not-translate entry of a caption.
func placeholder(n int) string { return fmt.Sprintf("⟦%d⟧", n) }

// placeholderRule is added to the prompt of a masked caption.
const placeholderRule = "- Copy every ⟦n⟧ placeholder exactly as it is, where it belongs in the sentence; each one stands for a name."

// EnforceDoNotTranslate checks out, the translation of src, against the
// do-not-translate entries of g that occur in src. An entry found in out
// with other casing is restored to its glossary spelling; entries missing
// from out are returned.
func EnforceDoNotTranslate(g *domain.Glossary, src, out string) (string, []string) {
	var missing []string
	for _, k := range doNotTranslate(g, src) {
		spans := findTerm(out, k)
		if len(spans) == 0 {
			missing = append(missing, k)
			continue
		}
		var b strings.Builder
		last := 0
		for _, sp := range spans {
			b.WriteString(out[last:sp[0]])
			b.WriteString(k)
			last = sp[1]
		}
		b.WriteString(out[last:])
		out = b.String()
	}
	return out, missing
}

// maskTerms replaces the occurrences of keep in text with placeholders,
// in order of keep.
func maskTerms(text string, keep []string) string {
	for i, k := range keep {
		spans := findTerm(text, k)
		for j := len(spans) - 1; j >= 0; j-- {
			text = text[:spans[j][0]] + placeholder(i+1) + text[spans[j][1]:]
		}
	}
	return text
}

// unmaskTerms puts keep back in place of the placeholders; ok is false if
// any placeholder is gone.
func unmaskTerms(text string, keep []string) (string, bool) {
	for i, k := range keep {
		p := placeholder(i + 1)
		if !strings.Contains(text, p) {
			return text, false
		}
		text = strings.ReplaceAll(text, p, k)
	}
	return text, !strings.Contains(text, "⟦")
}

// keepTerms enforces the glossary's do-not-translate entries on text, the
// translation of req (see the top of this file). The repair call of a
// final is bounded by the fan-out's timeout and its usage is counted.
func (t *target) keepTerms(req domain.TranslateRequest, text string) string {
	f := t.f
	text, missing := EnforceDoNotTranslate(req.Glossary, req.Text, text)
	if len(missing) == 0 || !req.Final {
		return text
	}
	keep := doNotTranslate(req.Glossary, req.Text)
	masked := req
	masked.Text = maskTerms(req.Text, keep)
	ctx, cancel := context.WithTimeout(f.ctx, f.opts.Timeout)
	res, err := f.opts.Translator.Translate(ctx, masked)
	cancel()
	if err != nil {
		f.log.Warn("do-not-translate repair failed", "session", f.opts.SessionID, "lang", t.lang, "segment", req.SegmentID, "err", err)
		return text
	}
	if f.opts.Usage != nil {
		f.opts.Usage(res.Usage)
	}
	fixed, ok := unmaskTerms(CleanOutput(res.Text), keep)
	if !ok {
		f.log.Warn("translation dropped do-not-translate terms", "session", f.opts.SessionID, "lang", t.lang,
			"segment", req.SegmentID, "terms", missing)
		return text
	}
	return fixed
}

// containsTerm reports whether term occurs in s as a whole word,
// ignoring case.
func containsTerm(s, term string) bool { return len(findTerm(s, term)) > 0 }

// findTerm returns the byte ranges of the whole-word occurrences of term
// in s, ignoring case. The edges of a term that start or end with a
// letter or digit must not touch another letter or digit, so "API" isn't
// found in "rapid" nor "Go" in "algo". A plural "s" or "es" may follow
// ("APIs", "clústeres"); the range then covers the term only.
func findTerm(s, term string) [][2]int {
	if term == "" {
		return nil
	}
	first, _ := utf8.DecodeRuneInString(term)
	lastR, _ := utf8.DecodeLastRuneInString(term)
	var spans [][2]int
	for i := 0; i < len(s); {
		end, ok := matchFold(s, i, term)
		if ok && (!isWord(first) || !isWord(runeBefore(s, i))) && (!isWord(lastR) || wordEnd(s, end)) {
			spans = append(spans, [2]int{i, end})
			i = end
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return spans
}

// matchFold matches term at s[i:] rune by rune, ignoring case, and
// returns where the match ends in s.
func matchFold(s string, i int, term string) (int, bool) {
	for _, tr := range term {
		if i >= len(s) {
			return 0, false
		}
		sr, size := utf8.DecodeRuneInString(s[i:])
		if sr != tr && unicode.ToLower(sr) != unicode.ToLower(tr) {
			return 0, false
		}
		i += size
	}
	return i, true
}

// wordEnd reports whether a word ends at s[i:], maybe after a plural
// suffix.
func wordEnd(s string, i int) bool {
	for _, suffix := range []string{"", "s", "es"} {
		if j, ok := matchFold(s, i, suffix); ok && !isWord(runeAt(s, j)) {
			return true
		}
	}
	return false
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func runeBefore(s string, i int) rune {
	if i == 0 {
		return utf8.RuneError
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func runeAt(s string, i int) rune {
	if i >= len(s) {
		return utf8.RuneError
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}
