// SPDX-License-Identifier: Apache-2.0

package subtitle

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// Options shape the cues. Zero fields take the defaults.
type Options struct {
	// MaxCharsPerLine and MaxLines bound a cue's text (settings.captions).
	MaxCharsPerLine int
	MaxLines        int
	// MinDuration and MaxDuration bound how long a cue stays on screen, in
	// seconds. A short cue is extended up to the next cue's start.
	MinDuration float64
	MaxDuration float64
}

// Defaults follow common broadcast subtitle guidelines (42 characters, two
// lines, 5/6 s to 7 s on screen).
const (
	DefaultMaxCharsPerLine = 42
	DefaultMaxLines        = 2
	DefaultMinDuration     = 5.0 / 6
	DefaultMaxDuration     = 7.0
)

func (o Options) withDefaults() Options {
	if o.MaxCharsPerLine <= 0 {
		o.MaxCharsPerLine = DefaultMaxCharsPerLine
	}
	if o.MaxLines <= 0 {
		o.MaxLines = DefaultMaxLines
	}
	if o.MinDuration <= 0 {
		o.MinDuration = DefaultMinDuration
	}
	if o.MaxDuration <= 0 {
		o.MaxDuration = DefaultMaxDuration
	}
	o.MaxDuration = max(o.MaxDuration, o.MinDuration)
	return o
}

// Cue is one subtitle: its time on the session clock (seconds) and its
// wrapped lines.
type Cue struct {
	Start, End float64
	Lines      []string
}

// Segment turns final captions into cues. Hidden and empty captions are
// skipped; the rest are taken in start-time order.
//
// Each caption is split into sentences, which are packed into cues of at
// most MaxLines lines of MaxCharsPerLine characters; a sentence only shares
// a cue with others if it fits whole. A sentence that is too long for one
// cue is cut at word boundaries, after a comma, semicolon or colon when one
// falls in the second half of the cue. The caption's time is shared between
// its cues in proportion to their length. Finally each cue's duration is
// clamped to [MinDuration, MaxDuration] without running into the next cue.
func Segment(captions []domain.CaptionEvent, opts Options) []Cue {
	opts = opts.withDefaults()
	sorted := slices.Clone(captions)
	slices.SortStableFunc(sorted, func(a, b domain.CaptionEvent) int {
		switch {
		case a.Start < b.Start:
			return -1
		case a.Start > b.Start:
			return 1
		}
		return 0
	})

	var cues []Cue
	for _, c := range sorted {
		if c.Hidden != nil && *c.Hidden {
			continue
		}
		blocks := pack(sentences(c.Text), opts)
		if len(blocks) == 0 {
			continue
		}
		cues = append(cues, timed(blocks, float64(c.Start), float64(c.End))...)
	}
	for i := range cues {
		c := &cues[i]
		next := -1.0
		if i+1 < len(cues) && cues[i+1].Start > c.Start {
			next = cues[i+1].Start
		}
		dur := min(max(c.End-c.Start, opts.MinDuration), opts.MaxDuration)
		c.End = c.Start + dur
		if next >= 0 && c.End > next {
			c.End = next
		}
	}
	return cues
}

// sentences splits text into sentences of words. A sentence ends with a
// word ending in . ! ? or …, possibly followed by closing quotes or brackets.
func sentences(text string) [][]string {
	var out [][]string
	var cur []string
	for _, w := range strings.Fields(text) {
		cur = append(cur, w)
		if endsWith(w, ".!?…") {
			out = append(out, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// endsWith reports whether w's last character, ignoring closing quotes and
// brackets, is one of marks.
func endsWith(w, marks string) bool {
	w = strings.TrimRight(w, `"')]»”’`)
	r, _ := utf8.DecodeLastRuneInString(w)
	return r != utf8.RuneError && strings.ContainsRune(marks, r)
}

// block is the text of one cue, already wrapped.
type block struct {
	lines []string
	chars int // characters including the spaces between words
}

// pack groups sentences into cue blocks.
func pack(sents [][]string, o Options) []block {
	var out []block
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			lines, _ := wrap(cur, o)
			out = append(out, newBlock(lines))
			cur = nil
		}
	}
	for _, s := range sents {
		if _, ok := wrap(append(slices.Clip(cur), s...), o); ok {
			cur = append(cur, s...)
			continue
		}
		flush()
		if _, ok := wrap(s, o); ok {
			cur = s
			continue
		}
		chunks := split(s, o)
		for _, c := range chunks[:len(chunks)-1] {
			lines, _ := wrap(c, o)
			out = append(out, newBlock(lines))
		}
		cur = chunks[len(chunks)-1]
	}
	flush()
	return out
}

func newBlock(lines []string) block {
	b := block{lines: lines}
	for i, l := range lines {
		b.chars += utf8.RuneCountInString(l)
		if i > 0 {
			b.chars++ // the space the line break replaced
		}
	}
	return b
}

// split cuts a sentence that doesn't fit one cue into chunks that do.
func split(words []string, o Options) [][]string {
	var out [][]string
	for len(words) > 0 {
		n := 1 // a single word too long for a line still makes a chunk
		for n < len(words) {
			if _, ok := wrap(words[:n+1], o); !ok {
				break
			}
			n++
		}
		if n < len(words) {
			// Prefer to cut after clause punctuation in the second half.
			half := chars(words[:n]) / 2
			for i := n - 1; i > 0; i-- {
				if chars(words[:i]) < half {
					break
				}
				if endsWith(words[i-1], ",;:") {
					n = i
					break
				}
			}
		}
		out = append(out, words[:n])
		words = words[n:]
	}
	return out
}

// chars is the length of words joined with spaces.
func chars(words []string) int {
	n := max(0, len(words)-1)
	for _, w := range words {
		n += utf8.RuneCountInString(w)
	}
	return n
}

// wrap breaks words into at most MaxLines lines of at most MaxCharsPerLine
// characters, as evenly as possible. ok is false when they don't fit; lines
// then holds the greedy wrap anyway. A word longer than a line gets a line
// of its own.
func wrap(words []string, o Options) (lines []string, ok bool) {
	if len(words) == 0 {
		return nil, true
	}
	greedy := fill(words, o.MaxCharsPerLine)
	if len(greedy) > o.MaxLines {
		return join(greedy), false
	}
	for _, l := range greedy {
		if chars(l) > o.MaxCharsPerLine {
			return join(greedy), false
		}
	}
	// Narrow the width while the line count stays the same, for balanced lines.
	best := greedy
	for w := o.MaxCharsPerLine - 1; w >= (chars(words)+len(greedy)-1)/len(greedy); w-- {
		l := fill(words, w)
		if len(l) != len(greedy) {
			break
		}
		best = l
	}
	return join(best), true
}

// fill wraps words greedily at width characters per line.
func fill(words []string, width int) [][]string {
	var lines [][]string
	var cur []string
	for _, w := range words {
		if len(cur) > 0 && chars(cur)+1+utf8.RuneCountInString(w) > width {
			lines = append(lines, cur)
			cur = nil
		}
		cur = append(cur, w)
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

func join(lines [][]string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.Join(l, " ")
	}
	return out
}

// timed shares [start, end] between blocks by length.
func timed(blocks []block, start, end float64) []Cue {
	total := 0
	for _, b := range blocks {
		total += b.chars
	}
	span := max(end-start, 0)
	cues := make([]Cue, len(blocks))
	at, done := start, 0
	for i, b := range blocks {
		done += b.chars
		next := start + span*float64(done)/float64(max(total, 1))
		if i == len(blocks)-1 {
			next = start + span
		}
		cues[i] = Cue{Start: at, End: next, Lines: b.lines}
		at = next
	}
	return cues
}
