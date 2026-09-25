// SPDX-License-Identifier: Apache-2.0

package session

import (
	"strings"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// switchWords is how much speech in another language it takes to switch
// the session's detected language (AI-10): a final in the new language of
// at least this many words, or consecutive finals in it adding up to
// this. Short answers like "OK" or "sí" in the middle of a talk would
// otherwise flip the language, and with it the translation direction, back
// and forth.
const switchWords = 4

// langVote is the evidence for a language switch that isn't there yet:
// the words of the finals detected in lang since the last final in the
// session's language.
type langVote struct {
	lang  domain.LanguageCode
	words int
}

// sourceLang is the source language of a caption with this text and the
// provider's detection:
//
//   - A pinned session language (`en`, `es`) always wins.
//   - Without a detection the caption takes the session's detected
//     language (English before the first detection).
//   - The first detection of a run is taken as is.
//   - A detection that differs from the session's language is taken only
//     if the caption, with the finals right before it in the same
//     language, has switchWords words; shorter ones keep the session's
//     language. Only finals count and switch; an interim that is long
//     enough is labelled with its language without switching.
//
// A switch updates the detected language in the status and tells viewers
// with a state message.
func (r *run) sourceLang(detected domain.LanguageCode, final bool, text string) domain.LanguageCode {
	r.mu.Lock()
	lang, changed := r.voteLang(detected, final, len(strings.Fields(text)))
	r.mu.Unlock()
	if changed {
		r.m.changed(r)
	}
	return lang
}

// voteLang implements sourceLang with mu held; changed reports a new
// detected language.
func (r *run) voteLang(detected domain.LanguageCode, final bool, words int) (lang domain.LanguageCode, changed bool) {
	commit := func(l domain.LanguageCode) (domain.LanguageCode, bool) {
		if !final || l == r.detected {
			return l, false
		}
		r.detected, r.pending = l, langVote{}
		return l, true
	}
	if pinned := r.sess.SourceLanguage; pinned == api.En || pinned == api.Es {
		return commit(domain.LanguageCode(pinned))
	}
	switch {
	case detected == "" && r.detected == "":
		return "en", false
	case detected == "":
		return r.detected, false
	case r.detected == "":
		return commit(detected)
	case detected == r.detected:
		if final {
			r.pending = langVote{}
		}
		return detected, false
	}
	total := words
	if r.pending.lang == detected {
		total += r.pending.words
	}
	if total >= switchWords {
		return commit(detected)
	}
	if final {
		r.pending = langVote{lang: detected, words: total}
	}
	return r.detected, false
}
