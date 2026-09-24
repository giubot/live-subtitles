// SPDX-License-Identifier: Apache-2.0

package subtitle

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// vttEscaper escapes the characters WebVTT cue text reserves; it also
// keeps "-->" out of cue text.
var vttEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Write errors are sticky in bufio.Writer, so the writers only check Flush.

// WriteVTT writes cues as a WebVTT file (OUT-6).
func WriteVTT(w io.Writer, cues []Cue) error {
	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString("WEBVTT\n")
	for _, c := range cues {
		_, _ = fmt.Fprintf(bw, "\n%s --> %s\n", timestamp(c.Start, '.'), timestamp(c.End, '.'))
		for _, l := range c.Lines {
			_, _ = bw.WriteString(vttEscaper.Replace(l))
			_ = bw.WriteByte('\n')
		}
	}
	return bw.Flush()
}

// WriteSRT writes cues as a SubRip file (OUT-7).
func WriteSRT(w io.Writer, cues []Cue) error {
	bw := bufio.NewWriter(w)
	for i, c := range cues {
		if i > 0 {
			_ = bw.WriteByte('\n')
		}
		_, _ = fmt.Fprintf(bw, "%d\n%s --> %s\n", i+1, timestamp(c.Start, ','), timestamp(c.End, ','))
		for _, l := range c.Lines {
			_, _ = bw.WriteString(l)
			_ = bw.WriteByte('\n')
		}
	}
	return bw.Flush()
}

// WriteTXT writes the plain transcript: one line per visible caption, in
// the given order (OUT-8).
func WriteTXT(w io.Writer, captions []domain.CaptionEvent) error {
	bw := bufio.NewWriter(w)
	for _, c := range captions {
		if c.Hidden != nil && *c.Hidden {
			continue
		}
		if text := strings.Join(strings.Fields(c.Text), " "); text != "" {
			_, _ = bw.WriteString(text)
			_ = bw.WriteByte('\n')
		}
	}
	return bw.Flush()
}

// timestamp formats seconds as HH:MM:SS<sep>mmm, rounded to the millisecond.
func timestamp(sec float64, sep byte) string {
	ms := int64(math.Round(max(sec, 0) * 1000))
	return fmt.Sprintf("%02d:%02d:%02d%c%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, sep, ms%1000)
}
