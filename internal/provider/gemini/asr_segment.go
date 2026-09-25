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
	// minSentenceChars: shorter sentences are joined with the next one
	// instead of becoming a final on their own.
	minSentenceChars = 20
	// maxSegmentChars: a segment without a sentence end is cut at a word
	// boundary once it grows past this.
	maxSegmentChars = 200
	// Start estimate of a new segment: the transcription of the first
	// words arrives about firstWordsLag + perWord×words after they began.
	firstWordsLag = 700 * time.Millisecond
	perWord       = 350 * time.Millisecond
)

// segmenter turns the input transcription stream of a Live connection into
// segments: interim events while text grows, and a final event per sentence
// or at the end of a turn (AI-6). Times are on the session clock: the
// transcription has no timestamps, so a segment ends at the audio clock when
// its last text arrived, and starts where the previous one ended or at an
// estimate from its first words.
type segmenter struct {
	pinned domain.LanguageCode // "" detects EN/ES

	seq      int
	text     string        // pending text of the open segment
	start    time.Duration // start of the open segment
	last     time.Duration // audio clock when the last text arrived
	prevEnd  time.Duration // end of the previous segment
	code     domain.LanguageCode
	tag      domain.LanguageCode // the model's language tag for the current turn
	lastLang domain.LanguageCode // language of the last final
}

// add appends a transcription chunk heard by audio clock now. code is the
// BCP-47 language the API reported for it, if any.
func (s *segmenter) add(chunk, code string, now time.Duration) []domain.ASREvent {
	if chunk == "" {
		return nil
	}
	if s.text == "" {
		s.seq++
		words := time.Duration(len(strings.Fields(chunk)))
		s.start = max(s.prevEnd, now-firstWordsLag-perWord*words, 0)
		s.code = ""
	}
	if l := normalizeLang(code); l != "" {
		s.code = l
	}
	s.text = joinChunk(s.text, chunk)
	s.last = max(now, s.start)

	var evs []domain.ASREvent
	for {
		head, rest, ok := splitSentence(s.text)
		if !ok {
			break
		}
		evs = append(evs, s.cut(head, rest))
	}
	if s.text != "" {
		evs = append(evs, s.event(s.text, s.start, s.last, false))
	}
	return evs
}

// cut finalizes head and keeps rest as the open segment. Its end is placed
// between the segment's start and now in proportion to the text.
func (s *segmenter) cut(head, rest string) domain.ASREvent {
	total := utf8.RuneCountInString(head) + utf8.RuneCountInString(rest)
	end := s.start + time.Duration(float64(s.last-s.start)*float64(utf8.RuneCountInString(head))/float64(total))
	ev := s.final(head, end)
	s.seq++
	s.text, s.start = rest, end
	return ev
}

// finish finalizes the open segment, at the end of a turn, after a silence
// or when the connection ends.
func (s *segmenter) finish() (domain.ASREvent, bool) {
	defer func() { s.tag = "" }()
	if s.text == "" {
		return domain.ASREvent{}, false
	}
	ev := s.final(s.text, s.last)
	s.text = ""
	return ev, true
}

// setTag records the model's language reply for the current turn.
func (s *segmenter) setTag(reply string) {
	if l := parseTag(reply); l != "" {
		s.tag = l
	}
}

func (s *segmenter) open() bool { return s.text != "" }

func (s *segmenter) final(text string, end time.Duration) domain.ASREvent {
	ev := s.event(text, s.start, end, true)
	s.prevEnd = end
	s.lastLang = ev.Lang
	return ev
}

func (s *segmenter) event(text string, start, end time.Duration, final bool) domain.ASREvent {
	return domain.ASREvent{
		SegmentID: fmt.Sprintf("g%06d", s.seq),
		Text:      text,
		Final:     final,
		Start:     start,
		End:       max(end, start),
		Lang:      s.language(text),
	}
}

// language picks the segment language: the pinned one, else what the API
// reported for the transcription, else the model's tag, else a guess from
// the words, else the language of the previous final ("" if none yet; the
// session then keeps its last detected language).
func (s *segmenter) language(text string) domain.LanguageCode {
	for _, l := range []domain.LanguageCode{s.pinned, s.code, s.tag, guessLanguage(text)} {
		if l != "" {
			return l
		}
	}
	return s.lastLang
}

// joinChunk appends a transcription chunk. Chunks carry their own leading
// spaces; whitespace is collapsed.
func joinChunk(text, chunk string) string {
	return strings.Join(strings.Fields(text+chunk), " ")
}

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
			continue // end of text so far, or "3.5", "e.g."
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

// parseTag reads the model's language reply ("en", "es", "Spanish."...).
func parseTag(reply string) domain.LanguageCode {
	w := strings.ToLower(strings.TrimFunc(reply, func(r rune) bool { return !unicode.IsLetter(r) }))
	switch w {
	case "en", "english", "inglés", "ingles":
		return "en"
	case "es", "spanish", "español", "espanol":
		return "es"
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
