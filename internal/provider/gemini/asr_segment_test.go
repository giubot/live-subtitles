// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestSplitSentence(t *testing.T) {
	long := strings.Repeat("word ", 45) + "and then, more words keep coming without a stop"
	cases := []struct {
		name       string
		text       string
		head, rest string
		ok         bool
	}{
		{"no end yet", "Hello and welcome to the", "", "", false},
		{"end of text so far", "Hello and welcome to the conference.", "", "", false},
		{"sentence and more", "Hello and welcome to the conference. Today we", "Hello and welcome to the conference.", "Today we", true},
		{"short sentence joins the next", "Yes. That is right, it works. And", "Yes. That is right, it works.", "And", true},
		{"question", "¿Qué es Kubernetes y para qué sirve? Bueno", "¿Qué es Kubernetes y para qué sirve?", "Bueno", true},
		{"decimal is not an end", "The version is 3.5 and it is fast", "", "", false},
		{"ellipsis", "So what happens next... we will see", "So what happens next...", "we will see", true},
		{"too long without an end", long, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head, rest, ok := splitSentence(tc.text)
			if ok != tc.ok {
				t.Fatalf("ok = %v", ok)
			}
			if tc.name == "too long without an end" {
				if len([]rune(head)) > maxSegmentChars || head+" "+rest != tc.text && head+rest != tc.text {
					t.Errorf("cut %q | %q", head, rest)
				}
				return
			}
			if head != tc.head || rest != tc.rest {
				t.Errorf("split = %q | %q", head, rest)
			}
		})
	}
}

func TestSegmenterTimes(t *testing.T) {
	s := segmenter{}
	// The first words arrive at 3 s: the segment starts at the estimate.
	evs := s.add(" Hello there", "", 3*time.Second)
	want := 3*time.Second - firstWordsLag - 2*perWord
	if len(evs) != 1 || evs[0].Start != want || evs[0].End != 3*time.Second || evs[0].Final {
		t.Fatalf("first = %+v, want start %v", evs, want)
	}
	// Two sentences end at 5 s; the cut is placed in proportion to the text.
	evs = s.add(" my friends, how are you. Fine", "en-US", 5*time.Second)
	if len(evs) != 2 || !evs[0].Final || evs[0].Lang != "en" {
		t.Fatalf("second = %+v", evs)
	}
	if evs[0].End <= want || evs[0].End >= 5*time.Second || evs[1].Start != evs[0].End || evs[1].SegmentID != "g000002" {
		t.Errorf("cut at %v, next %+v", evs[0].End, evs[1])
	}
	ev, ok := s.finish()
	if !ok || ev.Text != "Fine" || ev.End != 5*time.Second {
		t.Errorf("finish = %+v", ev)
	}
	if _, ok := s.finish(); ok {
		t.Error("finish twice")
	}
	// A new segment never starts before the previous one ended.
	evs = s.add(" next", "", 5100*time.Millisecond)
	if evs[0].Start != 5*time.Second || evs[0].SegmentID != "g000003" {
		t.Errorf("next = %+v", evs[0])
	}
}

func TestSegmenterLanguage(t *testing.T) {
	cases := []struct {
		name   string
		pinned domain.LanguageCode
		steps  func(s *segmenter) domain.ASREvent
		want   domain.LanguageCode
	}{
		{"keeps the last language when unclear", "", func(s *segmenter) domain.ASREvent {
			s.add("¿Qué tal, cómo están?", "", time.Second)
			s.finish()
			s.add("Kubernetes", "", 2*time.Second)
			ev, _ := s.finish()
			return ev
		}, "es"},
		{"tag resets at the end of the turn", "", func(s *segmenter) domain.ASREvent {
			s.setTag("es")
			s.add("Kubernetes", "", time.Second)
			s.finish()
			s.add("Kubernetes", "", 2*time.Second)
			s.setTag("en")
			ev, _ := s.finish()
			return ev
		}, "en"},
		{"unknown code ignored", "", func(s *segmenter) domain.ASREvent {
			return s.add("the cloud and the edge", "fr-FR", time.Second)[0]
		}, "en"},
		{"pinned", "en", func(s *segmenter) domain.ASREvent {
			return s.add("la nube y el borde", "es", time.Second)[0]
		}, "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &segmenter{pinned: tc.pinned}
			if got := tc.steps(s).Lang; got != tc.want {
				t.Errorf("lang = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLanguageHelpers(t *testing.T) {
	for _, tc := range []struct {
		in   string
		norm domain.LanguageCode
		tag  domain.LanguageCode
	}{
		{"en", "en", "en"},
		{"es-419", "es", "es"},
		{"EN_us", "en", ""},
		{" es.", "", "es"},
		{"Spanish.", "", "es"},
		{"English", "", "en"},
		{"fr", "", ""},
		{"", "", ""},
	} {
		if got := normalizeLang(tc.in); got != tc.norm {
			t.Errorf("normalizeLang(%q) = %q, want %q", tc.in, got, tc.norm)
		}
		if got := parseTag(tc.in); got != tc.tag {
			t.Errorf("parseTag(%q) = %q, want %q", tc.in, got, tc.tag)
		}
	}
	for _, tc := range []struct {
		text string
		want domain.LanguageCode
	}{
		{"Today we are going to talk about the cloud", "en"},
		{"Hoy vamos a hablar de la nube", "es"},
		{"Kubernetes", ""},
		{"no", ""},
		{"¡Hola!", "es"},
	} {
		if got := guessLanguage(tc.text); got != tc.want {
			t.Errorf("guessLanguage(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}
