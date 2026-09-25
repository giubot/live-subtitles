// SPDX-License-Identifier: Apache-2.0

package streamcc

import (
	"slices"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/api"
)

func TestWrap(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"empty", "   ", 32, nil},
		{"fits", "Hello everyone.", 32, []string{"Hello everyone."}},
		{"breaks at spaces", "Today we talk about observability in Kubernetes clusters.", 20,
			[]string{"Today we talk about", "observability in", "Kubernetes clusters."}},
		{"collapses whitespace", "a  b\n\tc", 32, []string{"a b c"}},
		{"counts runes not bytes", "¿Qué pasó con la señal? ¡Ñandú!", 16, []string{"¿Qué pasó con la", "señal? ¡Ñandú!"}},
		{"cuts long words", "supercalifragilistic ok", 8, []string{"supercal", "ifragili", "stic ok"}},
		{"default width", "x", 0, []string{"x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrap(tt.text, tt.width)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("wrap(%q, %d) = %q, want %q", tt.text, tt.width, got, tt.want)
			}
			w := tt.width
			if w <= 0 {
				w = DefaultMaxChars
			}
			for _, l := range got {
				if n := utf8.RuneCountInString(l); n > w {
					t.Errorf("line %q has %d chars, more than %d", l, n, w)
				}
			}
		})
	}
}

func TestCues(t *testing.T) {
	t0 := time.Date(2026, 9, 25, 13, 30, 0, 0, time.UTC)
	tests := []struct {
		name      string
		text      string
		maxChars  int
		dur       time.Duration
		wantLines [][]string
		wantAt    []time.Duration
	}{
		{"one cue", "Hello everyone.", 32, 2 * time.Second, [][]string{{"Hello everyone."}}, []time.Duration{0}},
		{"two lines one cue", "one two three four", 9, time.Second, [][]string{{"one two", "three"}, {"four"}}, []time.Duration{0, 500 * time.Millisecond}},
		{"spread over the speech", "aa bb cc dd ee ff", 2, 3 * time.Second,
			[][]string{{"aa", "bb"}, {"cc", "dd"}, {"ee", "ff"}}, []time.Duration{0, time.Second, 2 * time.Second}},
		{"no duration", "aa bb cc", 2, 0, [][]string{{"aa", "bb"}, {"cc"}}, []time.Duration{0, 0}},
		{"empty", "", 32, time.Second, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cues(tt.text, tt.maxChars, t0, t0.Add(tt.dur))
			if len(got) != len(tt.wantLines) {
				t.Fatalf("got %d cues, want %d: %+v", len(got), len(tt.wantLines), got)
			}
			for i, c := range got {
				if !slices.Equal(c.lines, tt.wantLines[i]) {
					t.Errorf("cue %d lines = %q, want %q", i, c.lines, tt.wantLines[i])
				}
				if at := c.at.Sub(t0); at != tt.wantAt[i] {
					t.Errorf("cue %d at +%v, want +%v", i, at, tt.wantAt[i])
				}
			}
		})
	}
}

func TestNewItemPlacesSpeechInWallTime(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 30, 10, 0, time.UTC)
	lat := 1500
	tests := []struct {
		name               string
		latency            *int
		start, end         float32
		wantStart, wantEnd time.Time
	}{
		{"with latency", &lat, 4, 6, now.Add(-3500 * time.Millisecond), now.Add(-1500 * time.Millisecond)},
		{"without latency", nil, 4, 6, now.Add(-2 * time.Second), now},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := newItem(api.Caption{SegmentId: "s-1", Text: "Hola", Start: tt.start, End: tt.end, LatencyMs: tt.latency}, 32, now)
			if !it.end.Equal(tt.wantEnd) || !it.cues[0].at.Equal(tt.wantStart) {
				t.Fatalf("item spans %v..%v, want %v..%v", it.cues[0].at, it.end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
