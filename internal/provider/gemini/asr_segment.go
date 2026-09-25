// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// Segmenting limits.
const (
	// minSentenceChars: shorter sentences of a final are joined with the
	// next one instead of becoming a segment on their own.
	minSentenceChars = 20
	// maxSegmentChars: a final without a sentence end is cut at a word
	// boundary once it grows past this.
	maxSegmentChars = 200
	// Start estimate of a new segment: the transcription of the first
	// words arrives about firstWordsLag + perWord×words after they began.
	firstWordsLag = 700 * time.Millisecond
	perWord       = 350 * time.Millisecond
)

// utterance is what one connection is transcribing: the server's interim
// hypothesis until its final arrives.
type utterance struct {
	id    string
	text  string
	start time.Duration // session clock
	last  time.Duration // audio clock when the interim text last changed
	code  domain.LanguageCode
	// flushAt is the wall-time deadline of the safety flush.
	flushAt time.Time
}

func (u *utterance) open() bool { return u.id != "" }

// segmenter turns the transcription of the Live connections into segments
// (AI-6). Each server utterance is one segment: its interim transcription
// replaces the segment's text, and its input transcription is the final.
// A final holding several sentences is cut into one segment per sentence,
// the first keeping the utterance's ID.
//
// Times are on the session clock. The transcription has no timestamps in
// Live mode, so a segment ends at the audio clock when its text last
// changed, and starts where the previous one ended or at an estimate from
// its first words.
type segmenter struct {
	pinned domain.LanguageCode // "" detects EN/ES

	seq      int
	prevEnd  time.Duration // end of the previous final
	lastLang domain.LanguageCode
}

// interim replaces the hypothesis of u with text heard by audio clock now.
// code is the BCP-47 language the API reported, if any. It reports false
// when the text is empty or unchanged.
func (s *segmenter) interim(u *utterance, text, code string, now time.Duration) (domain.ASREvent, bool) {
	text = clean(text)
	if text == "" || text == u.text {
		return domain.ASREvent{}, false
	}
	s.begin(u, text, now)
	if l := normalizeLang(code); l != "" {
		u.code = l
	}
	u.text, u.last = text, max(now, u.start)
	return s.event(u.id, text, u.start, u.last, false, s.language(text, u.code)), true
}

// final ends u with the server's final text, or with its interim when text
// is empty (a safety flush), and resets u.
func (s *segmenter) final(u *utterance, text, code string, now time.Duration) []domain.ASREvent {
	defer func() { *u = utterance{} }()
	if text = clean(text); text == "" {
		text = u.text
	}
	if text == "" {
		return nil
	}
	end := now
	if u.open() {
		end = u.last
	}
	s.begin(u, text, now)
	if l := normalizeLang(code); l != "" {
		u.code = l
	}
	if u.code == "" {
		// Without an API code, judge the whole utterance: one of its
		// sentences alone ("Bienvenidos a todos.") may have no common word.
		u.code = guessLanguage(text)
	}
	end = max(end, u.start)

	var evs []domain.ASREvent
	id, start, rest := u.id, u.start, text
	total, done := utf8.RuneCountInString(text), 0
	for {
		head, tail, ok := splitSentence(rest)
		if !ok {
			break
		}
		// The cut is placed between start and end in proportion to the text.
		done += utf8.RuneCountInString(head) + 1
		at := u.start + time.Duration(float64(end-u.start)*float64(done)/float64(total))
		evs = append(evs, s.finalEvent(id, head, start, at, u.code))
		s.seq++
		id, start, rest = segmentID(s.seq), at, tail
	}
	return append(evs, s.finalEvent(id, rest, start, end, u.code))
}

// begin opens u with a new segment ID, estimating its start.
func (s *segmenter) begin(u *utterance, text string, now time.Duration) {
	if u.open() {
		return
	}
	s.seq++
	words := time.Duration(len(strings.Fields(text)))
	u.id = segmentID(s.seq)
	u.start = max(s.prevEnd, now-firstWordsLag-perWord*words, 0)
	u.last = u.start
}

func (s *segmenter) finalEvent(id, text string, start, end time.Duration, code domain.LanguageCode) domain.ASREvent {
	ev := s.event(id, text, start, end, true, s.language(text, code))
	s.prevEnd = max(s.prevEnd, ev.End)
	if ev.Lang != "" {
		s.lastLang = ev.Lang
	}
	return ev
}

func (s *segmenter) event(id, text string, start, end time.Duration, final bool, lang domain.LanguageCode) domain.ASREvent {
	return domain.ASREvent{SegmentID: id, Text: text, Final: final, Start: start, End: max(end, start), Lang: lang}
}

func segmentID(seq int) string { return fmt.Sprintf("g%06d", seq) }

// language picks the segment language: the pinned one, else what the API
// reported for the utterance, else a guess from the words, else the
// language of the previous final ("" if none yet; the session then keeps
// its last detected language).
func (s *segmenter) language(text string, code domain.LanguageCode) domain.LanguageCode {
	for _, l := range []domain.LanguageCode{s.pinned, code, guessLanguage(text)} {
		if l != "" {
			return l
		}
	}
	return s.lastLang
}

// clean collapses whitespace: SMART mode may format the text in
// paragraphs, and a caption is one line.
func clean(text string) string { return strings.Join(strings.Fields(text), " ") }

// splitSentence splits off the first sentences of text that are at least
// minSentenceChars long and followed by more text, or the first
// maxSegmentChars at a word boundary.
func splitSentence(text string) (head, rest string, ok bool) {
	for i, r := range text {
		if !isSentenceEnd(r) {
			continue
		}
		j := i + utf8.RuneLen(r)
		for j < len(text) && isSentenceEnd(rune(text[j])) {
			j++
		}
		if j >= len(text) || text[j] != ' ' {
			continue // end of text, or "3.5", "e.g."
		}
		if utf8.RuneCountInString(text[:j]) < minSentenceChars {
			continue
		}
		return text[:j], strings.TrimSpace(text[j:]), true
	}
	if utf8.RuneCountInString(text) <= maxSegmentChars {
		return "", "", false
	}
	cut := len(text)
	n := 0
	for i := range text {
		if n == maxSegmentChars {
			cut = i
			break
		}
		n++
	}
	at := strings.LastIndex(text[:cut], ", ")
	if at > 0 {
		at++ // keep the comma
	} else if at = strings.LastIndex(text[:cut], " "); at <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(text[:at]), strings.TrimSpace(text[at:]), true
}

func isSentenceEnd(r rune) bool { return r == '.' || r == '?' || r == '!' || r == '…' }

// normalizeLang maps a BCP-47 code to en or es; anything else is "".
func normalizeLang(code string) domain.LanguageCode {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(code, "-_"); i > 0 {
		code = code[:i]
	}
	switch code {
	case "en", "es":
		return code
	}
	return ""
}

var (
	enWords = wordSet("the and is are was were of to in that it you we this what with for on be have has not i they there can do does how but so if about my your our will would just like from at by an")
	esWords = wordSet("el la los las de del que y en un una es son por con para se lo al como más mas pero su sus está esta estamos muy yo nosotros hay qué cómo también porque cuando donde ya fue ser tiene tenemos este esto eso")
)

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// guessLanguage tells English from Spanish by common words and Spanish
// letters, or returns "" when the text doesn't say clearly.
func guessLanguage(text string) domain.LanguageCode {
	var en, es int
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	}) {
		if enWords[w] {
			en++
		}
		if esWords[w] {
			es++
		}
		if strings.ContainsAny(w, "ñáéíóú") {
			es++
		}
	}
	if strings.ContainsAny(text, "¿¡") {
		es += 2
	}
	switch {
	case en > es && (es == 0 || en-es >= 2):
		return "en"
	case es > en && (en == 0 || es-en >= 2):
		return "es"
	}
	return ""
}
