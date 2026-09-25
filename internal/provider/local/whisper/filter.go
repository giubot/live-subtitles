// SPDX-License-Identifier: Apache-2.0

package whisper

import (
	"regexp"
	"strings"
	"unicode"
)

// Whisper's own "this was silence" rule: a segment it thinks has no speech
// and decoded with low confidence is dropped.
const (
	noSpeechThreshold = 0.6
	logprobThreshold  = -1.0
	// Short phrases that are real speech as often as hallucinations are
	// only dropped when the segment looks doubtful.
	doubtfulNoSpeech = 0.3
	doubtfulLogprob  = -0.8
)

var (
	// annotations are whisper's non-speech tags: [Música], [BLANK_AUDIO],
	// (applause), ♪, <|nospeech|>.
	annotations = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)|<\|[^|]*\|>|[♪♫*]+`)
	// alwaysHallucinated are captions whisper invents on noise and silence
	// (mostly from subtitled YouTube videos in its training data) and that
	// never open a sentence in a live talk. They match the normalized text
	// from its start, so a talk that merely mentions subtitles is kept.
	alwaysHallucinated = regexp.MustCompile(`^(` + strings.Join([]string{
		`.*amara\.?org.*`,
		`subt[ií]tulos? (realizados? |hechos? )?(por|by) .*`,
		`(subtitles?|captions?|transcription|transcribed|translated|translation) by .*`,
		`(thanks|thank you) (so much |very much )?for watching.*`,
		`gracias por (ver|mirar).*`,
		`(please )?(like and )?subscribe( to (my|the|our) channel)?`,
		`(suscr[ií]bete|suscr[ií]banse)( al canal)?.*`,
		`no olvides suscribirte.*`,
		`www\.[a-z0-9-]+\.[a-z]+`,
		`(ah|eh|mm+|hmm+|uh|um)`,
	}, "|") + `)$`)
	// doubtful are short phrases dropped only from doubtful segments.
	doubtful = map[string]bool{
		"you": true, "thank you": true, "thanks": true, "bye": true, "okay": true, "ok": true, "so": true,
		"gracias": true, "muchas gracias": true, "adiós": true, "chau": true, "sí": true,
	}
)

// transcript turns a verbose_json answer into caption text, without the
// segments whisper marks as silence, non-speech tags, runs of repeated
// words and known hallucinations. It returns "" when nothing is left.
func transcript(r response) string {
	var parts []string
	doubt := false
	for _, s := range r.Segments {
		if s.NoSpeechProb > noSpeechThreshold && s.AvgLogprob < logprobThreshold {
			continue
		}
		if s.NoSpeechProb > doubtfulNoSpeech || s.AvgLogprob < doubtfulLogprob {
			doubt = true
		}
		parts = append(parts, s.Text)
	}
	if len(r.Segments) == 0 {
		parts = []string{r.Text}
	}
	text := annotations.ReplaceAllString(strings.Join(parts, " "), " ")
	text = collapseRepeats(strings.Fields(text))
	norm := normalize(text)
	switch {
	case norm == "":
		return ""
	case alwaysHallucinated.MatchString(norm):
		return ""
	case doubt && doubtful[norm]:
		return ""
	}
	return text
}

// normalize lowercases s and strips punctuation other than the dots of
// domain names, for matching.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '.' || r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(strings.Trim(b.String(), ".- ")), " ")
}

// Repetition limits: whisper's failure mode is looping on a phrase ("y y y
// y", "la verdad es que la verdad es que la verdad es que").
const (
	maxNgram      = 8
	maxWordRepeat = 3 // "no, no, no" is speech; a fourth "no" isn't kept
	maxNgramRun   = 2 // a phrase said twice is kept; the third copy is dropped
)

// collapseRepeats joins words, cutting a word repeated more than
// maxWordRepeat times in a row, or a phrase of 2..maxNgram words repeated
// more than maxNgramRun times in a row, down to one occurrence.
func collapseRepeats(words []string) string {
	keys := make([]string, len(words))
	for i, w := range words {
		keys[i] = normalize(w)
	}
	for n := 1; n <= maxNgram; n++ {
		limit := maxNgramRun
		if n == 1 {
			limit = maxWordRepeat
		}
		var outW, outK []string
		for i := 0; i < len(words); {
			reps := 1
			for i+(reps+1)*n <= len(keys) && equal(keys[i:i+n], keys[i+reps*n:i+(reps+1)*n]) {
				reps++
			}
			if reps > limit && keys[i] != "" {
				outW = append(outW, words[i:i+n]...)
				outK = append(outK, keys[i:i+n]...)
				i += reps * n
				continue
			}
			outW = append(outW, words[i])
			outK = append(outK, keys[i])
			i++
		}
		words, keys = outW, outK
	}
	return strings.Join(words, " ")
}

func equal(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
