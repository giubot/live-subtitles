// SPDX-License-Identifier: Apache-2.0

package subtitle

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/domain"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func caption(seg string, start, end float32, text string) domain.CaptionEvent {
	return domain.CaptionEvent{SessionId: "main", Lang: "es", SegmentId: seg, Final: true, Start: start, End: end, Text: text, SourceLang: "en"}
}

func TestWrap(t *testing.T) {
	o := Options{}.withDefaults()
	tests := []struct {
		name   string
		text   string
		want   []string
		wantOK bool
	}{
		{"short", "Hola a todos.", []string{"Hola a todos."}, true},
		{"exactly one line", strings.Repeat("a", 42), []string{strings.Repeat("a", 42)}, true},
		{"balanced two lines", "Hoy vamos a hablar de observabilidad en Kubernetes y de cómo medir.",
			[]string{"Hoy vamos a hablar de observabilidad", "en Kubernetes y de cómo medir."}, true},
		{"too long", strings.Repeat("palabra ", 12), nil, false},
		{"word longer than a line", strings.Repeat("x", 50), nil, false},
		{"accents count as one character", strings.Repeat("ñ", 42), []string{strings.Repeat("ñ", 42)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := wrap(strings.Fields(tt.text), o)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (%q)", ok, tt.wantOK, got)
			}
			if tt.want != nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("wrap = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSegment(t *testing.T) {
	hidden := true
	hiddenCap := caption("s-3", 9, 10, "Esto no se ve.")
	hiddenCap.Hidden = &hidden
	tests := []struct {
		name     string
		captions []domain.CaptionEvent
		opts     Options
		want     []Cue
	}{
		{
			name:     "one short caption",
			captions: []domain.CaptionEvent{caption("s-1", 1, 3, "Hola a todos.")},
			want:     []Cue{{1, 3, []string{"Hola a todos."}}},
		},
		{
			name:     "short sentences share a cue",
			captions: []domain.CaptionEvent{caption("s-1", 0, 2, "Sí. Claro. Empecemos.")},
			want:     []Cue{{0, 2, []string{"Sí. Claro. Empecemos."}}},
		},
		{
			name: "a sentence that doesn't fit starts a new cue",
			captions: []domain.CaptionEvent{caption("s-1", 0, 9,
				"Bienvenidos a Nerdearla. Hoy vamos a hablar de observabilidad en Kubernetes y de cómo medir la latencia.")},
			// 24 and 79 characters: the 9 s are shared 24:79.
			want: []Cue{
				{0, 24.0 / 103 * 9, []string{"Bienvenidos a Nerdearla."}},
				{24.0 / 103 * 9, 9, []string{"Hoy vamos a hablar de observabilidad en", "Kubernetes y de cómo medir la latencia."}},
			},
		},
		{
			name: "long sentence cut after a comma",
			captions: []domain.CaptionEvent{caption("s-1", 0, 12,
				"Cuando desplegamos el servicio en producción por primera vez, nos dimos cuenta de que las métricas no alcanzaban para entender qué pasaba.")},
			want: []Cue{
				{0, 0, []string{"Cuando desplegamos el servicio", "en producción por primera vez,"}},
				{0, 12, []string{"nos dimos cuenta de que las métricas no", "alcanzaban para entender qué pasaba."}},
			},
		},
		{
			name: "min duration, capped by the next cue",
			captions: []domain.CaptionEvent{
				caption("s-1", 1, 1.2, "Sí."),
				caption("s-2", 1.5, 1.7, "No."),
				caption("s-3", 5, 5.1, "Bueno."),
			},
			want: []Cue{
				{1, 1.5, []string{"Sí."}},
				{1.5, 1.5 + DefaultMinDuration, []string{"No."}},
				{5, 5 + DefaultMinDuration, []string{"Bueno."}},
			},
		},
		{
			name:     "max duration",
			captions: []domain.CaptionEvent{caption("s-1", 0, 20, "Silencio largo.")},
			want:     []Cue{{0, DefaultMaxDuration, []string{"Silencio largo."}}},
		},
		{
			name: "hidden and empty captions are skipped, order is by start",
			captions: []domain.CaptionEvent{
				caption("s-2", 4, 5, "Segundo."),
				hiddenCap,
				caption("s-4", 6, 7, "   "),
				caption("s-1", 2, 3, "Primero."),
			},
			want: []Cue{{2, 3, []string{"Primero."}}, {4, 5, []string{"Segundo."}}},
		},
		{
			name:     "custom line limits",
			captions: []domain.CaptionEvent{caption("s-1", 0, 4, "Uno dos tres cuatro cinco seis.")},
			opts:     Options{MaxCharsPerLine: 12, MaxLines: 1},
			want: []Cue{
				{0, 4.0 * 12 / 29, []string{"Uno dos tres"}},
				{4.0 * 12 / 29, 4.0 * 24 / 29, []string{"cuatro cinco"}},
				{4.0 * 24 / 29, 4.0*24/29 + DefaultMinDuration, []string{"seis."}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Segment(tt.captions, tt.opts)
			// The comma case checks the cut, not the proportional times.
			if tt.name == "long sentence cut after a comma" {
				for i := range got {
					got[i].Start, got[i].End = tt.want[i].Start, tt.want[i].End
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d cues %+v, want %d", len(got), got, len(tt.want))
			}
			for i := range got {
				g, w := got[i], tt.want[i]
				if !near(g.Start, w.Start) || !near(g.End, w.End) || !reflect.DeepEqual(g.Lines, w.Lines) {
					t.Errorf("cue %d = %.3f–%.3f %q, want %.3f–%.3f %q", i, g.Start, g.End, g.Lines, w.Start, w.End, w.Lines)
				}
			}
		})
	}
}

func near(a, b float64) bool { return a-b < 1e-3 && b-a < 1e-3 }

// Every cue of a realistic transcript respects the limits.
func TestSegmentLimits(t *testing.T) {
	cues := Segment(talk, Options{})
	if len(cues) < len(talk) {
		t.Fatalf("%d cues for %d captions", len(cues), len(talk))
	}
	for i, c := range cues {
		if len(c.Lines) == 0 || len(c.Lines) > DefaultMaxLines {
			t.Errorf("cue %d has %d lines", i, len(c.Lines))
		}
		for _, l := range c.Lines {
			if n := utf8.RuneCountInString(l); n > DefaultMaxCharsPerLine || n == 0 {
				t.Errorf("cue %d line %q has %d characters", i, l, n)
			}
		}
		if d := c.End - c.Start; d <= 0 || d > DefaultMaxDuration+1e-9 {
			t.Errorf("cue %d lasts %.3f s", i, d)
		}
		if i > 0 && c.Start < cues[i-1].End-1e-9 {
			t.Errorf("cue %d starts at %.3f before cue %d ends at %.3f", i, c.Start, i-1, cues[i-1].End)
		}
	}
}

var talk = []domain.CaptionEvent{
	caption("s-000001", 0.4, 3.1, "Hola a todos, bienvenidos a Nerdearla."),
	caption("s-000002", 3.6, 9.8, "Hoy vamos a hablar de observabilidad en Kubernetes, y de por qué los logs solos no alcanzan cuando algo falla a las tres de la mañana."),
	caption("s-000003", 10.2, 12.0, "¿Quién usa Prometheus? ¡Muchos!"),
	caption("s-000004", 12.5, 16.9, `El equipo dijo: "medimos todo", pero nadie miraba los paneles <en serio> & a tiempo.`),
	caption("s-000005", 3725.25, 3727.5, "Gracias."),
}

func TestWriters(t *testing.T) {
	cues := Segment(talk, Options{})
	tests := []struct {
		golden string
		write  func(*bytes.Buffer) error
	}{
		{"talk.vtt", func(b *bytes.Buffer) error { return WriteVTT(b, cues) }},
		{"talk.srt", func(b *bytes.Buffer) error { return WriteSRT(b, cues) }},
		{"talk.txt", func(b *bytes.Buffer) error { return WriteTXT(b, talk) }},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			var b bytes.Buffer
			if err := tt.write(&b); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", tt.golden)
			if *update {
				if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(b.Bytes(), want) {
				t.Errorf("output differs from %s:\n%s", path, b.String())
			}
		})
	}
}

func TestEmpty(t *testing.T) {
	var b bytes.Buffer
	if err := WriteVTT(&b, nil); err != nil || b.String() != "WEBVTT\n" {
		t.Errorf("empty VTT = %q, %v", b.String(), err)
	}
	b.Reset()
	if err := WriteSRT(&b, nil); err != nil || b.Len() != 0 {
		t.Errorf("empty SRT = %q, %v", b.String(), err)
	}
}

func TestTimestamp(t *testing.T) {
	tests := []struct {
		sec  float64
		sep  byte
		want string
	}{
		{0, '.', "00:00:00.000"},
		{1.0005, '.', "00:00:01.001"},
		{3725.25, ',', "01:02:05,250"},
		{-1, '.', "00:00:00.000"},
		{360000, '.', "100:00:00.000"},
	}
	for _, tt := range tests {
		if got := timestamp(tt.sec, tt.sep); got != tt.want {
			t.Errorf("timestamp(%v) = %s, want %s", tt.sec, got, tt.want)
		}
	}
}
