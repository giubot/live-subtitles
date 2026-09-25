// SPDX-License-Identifier: Apache-2.0

package streamcc

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/api"
)

// Defaults for cue layout.
const (
	// DefaultMaxChars is the line length when the session doesn't set one:
	// the CEA-608 limit, which YouTube's own renderer also reads well.
	DefaultMaxChars = 32
	// LinesPerCue is how many wrapped lines one cue shows.
	LinesPerCue = 2
)

// cue is one caption on screen: up to LinesPerCue lines shown from at.
type cue struct {
	at    time.Time // wall clock (local) when the speech it carries began
	lines []string
}

// item is the unit the sink queues and delivers: the cues of one final
// caption, sent in one YouTube POST (one seq) or as one run of OBS captions.
type item struct {
	segment string
	cues    []cue
	// end is when the speech ended (local wall clock); the item is stale
	// once it is older than the sink's MaxAge.
	end time.Time
}

// wrap splits text into lines of at most width characters (runes),
// breaking at spaces; a word longer than a line is cut. Whitespace is
// collapsed.
func wrap(text string, width int) []string {
	if width <= 0 {
		width = DefaultMaxChars
	}
	var lines []string
	var cur strings.Builder
	curLen := 0
	flush := func() {
		if curLen > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
		}
	}
	for _, w := range strings.Fields(text) {
		n := utf8.RuneCountInString(w)
		for n > width { // cut a word that can't fit on any line
			flush()
			cut := byteIndex(w, width)
			lines = append(lines, w[:cut])
			w, n = w[cut:], n-width
		}
		switch {
		case curLen == 0:
		case curLen+1+n <= width:
			cur.WriteByte(' ')
			curLen++
		default:
			flush()
		}
		cur.WriteString(w)
		curLen += n
	}
	flush()
	return lines
}

// byteIndex is the byte offset of the n-th rune of s.
func byteIndex(s string, n int) int {
	i := 0
	for range n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return i
}

// cues lays a final caption out as cues: its text wrapped to maxChars,
// LinesPerCue lines per cue, spread evenly over the speech from start to
// end (local wall clock).
func cues(text string, maxChars int, start, end time.Time) []cue {
	lines := wrap(text, maxChars)
	if len(lines) == 0 {
		return nil
	}
	n := (len(lines) + LinesPerCue - 1) / LinesPerCue
	step := time.Duration(0)
	if d := end.Sub(start); d > 0 {
		step = d / time.Duration(n)
	}
	out := make([]cue, 0, n)
	for i := range n {
		hi := min((i+1)*LinesPerCue, len(lines))
		out = append(out, cue{at: start.Add(time.Duration(i) * step), lines: lines[i*LinesPerCue : hi]})
	}
	return out
}

// newItem turns a final caption that reached the sink at now into an item.
// The speech is placed in wall time from the caption's latency (how long
// after the end of its audio it was emitted) and its duration.
func newItem(c api.Caption, maxChars int, now time.Time) item {
	end := now
	if c.LatencyMs != nil {
		end = now.Add(-time.Duration(*c.LatencyMs) * time.Millisecond)
	}
	dur := time.Duration(float64(c.End-c.Start) * float64(time.Second))
	start := end.Add(-max(dur, 0))
	return item{segment: c.SegmentId, cues: cues(c.Text, maxChars, start, end), end: end}
}
