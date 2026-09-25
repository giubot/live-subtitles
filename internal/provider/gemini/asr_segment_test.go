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
		{"end of text", "Hello and welcome to the conference.", "", "", false},
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
	var u utterance
	// The first words arrive at 3 s: the segment starts at the estimate.
	ev, ok := s.interim(&u, " Hello there", "", 3*time.Second)
	want := 3*time.Second - firstWordsLag - 2*perWord
	if !ok || ev.Start != want || ev.End != 3*time.Second || ev.Final || ev.SegmentID != "g000001" {
		t.Fatalf("interim = %+v, want start %v", ev, want)
	}
	// The hypothesis grows until 5 s; the final comes later, at 6 s.
	s.interim(&u, "Hello there my friends, how are you", "", 5*time.Second)
	evs := s.final(&u, "Hello there my friends, how are you? Fine", "en-US", 6*time.Second)
	if len(evs) != 2 || !evs[0].Final || !evs[1].Final || evs[0].Lang != "en" || evs[0].SegmentID != "g000001" {
		t.Fatalf("finals = %+v", evs)
	}
	if evs[0].Start != want || evs[0].End <= want || evs[0].End >= 5*time.Second ||
		evs[1].Start != evs[0].End || evs[1].End != 5*time.Second || evs[1].SegmentID != "g000002" {
		t.Errorf("cut at %v, next %+v", evs[0].End, evs[1])
	}
	if u.open() {
		t.Error("utterance still open after its final")
	}
	// A final without interims ends where it arrived, and never starts
	// before the previous segment ended.
	evs = s.final(&u, "next", "", 5100*time.Millisecond)
	if len(evs) != 1 || evs[0].Start != 5*time.Second || evs[0].End != 5100*time.Millisecond || evs[0].SegmentID != "g000003" {
		t.Errorf("next = %+v", evs)
	}
	// A safety flush finalizes the interim; with nothing open it's a no-op.
	s.interim(&u, "open words", "", 7*time.Second)
	if evs := s.final(&u, "", "", 9*time.Second); len(evs) != 1 || evs[0].Text != "open words" || evs[0].End != 7*time.Second {
		t.Errorf("flush = %+v", evs)
	}
	if evs := s.final(&u, "", "", 9*time.Second); evs != nil {
		t.Errorf("flush of nothing = %+v", evs)
	}
}

func TestSegmenterInterim(t *testing.T) {
	cases := []struct {
		name   string
		steps  []string
		wantOK []bool
	}{
		{"replaces", []string{"Hello", "Hello and welcome"}, []bool{true, true}},
		{"unchanged text is no event", []string{"Hello  there", "Hello there\n"}, []bool{true, false}},
		{"empty is no event", []string{" "}, []bool{false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s segmenter
			var u utterance
			for i, text := range tc.steps {
				ev, ok := s.interim(&u, text, "", time.Duration(i+1)*time.Second)
				if ok != tc.wantOK[i] {
					t.Errorf("step %d: ok = %v", i, ok)
				}
				if ok && (ev.SegmentID != "g000001" || ev.Text != clean(text)) {
					t.Errorf("step %d: %+v", i, ev)
				}
			}
		})
	}
}

func TestSegmenterLanguage(t *testing.T) {
	cases := []struct {
		name   string
		pinned domain.LanguageCode
		steps  func(s *segmenter, u *utterance) domain.ASREvent
		want   domain.LanguageCode
	}{
		{"keeps the last language when unclear", "", func(s *segmenter, u *utterance) domain.ASREvent {
			s.final(u, "¿Qué tal, cómo están?", "", time.Second)
			return s.final(u, "Kubernetes", "", 2*time.Second)[0]
		}, "es"},
		{"API code of the interim carries to the final", "", func(s *segmenter, u *utterance) domain.ASREvent {
			s.interim(u, "Kubernetes", "es-419", time.Second)
			return s.final(u, "Kubernetes y", "", 2*time.Second)[0]
		}, "es"},
		{"API code resets with the utterance", "", func(s *segmenter, u *utterance) domain.ASREvent {
			s.final(u, "Kubernetes", "es", time.Second)
			return s.final(u, "the cloud and the edge", "", 2*time.Second)[0]
		}, "en"},
		{"unknown code ignored", "", func(s *segmenter, u *utterance) domain.ASREvent {
			ev, _ := s.interim(u, "the cloud and the edge", "fr-FR", time.Second)
			return ev
		}, "en"},
		{"pinned", "en", func(s *segmenter, u *utterance) domain.ASREvent {
			return s.final(u, "la nube y el borde", "es", time.Second)[0]
		}, "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &segmenter{pinned: tc.pinned}
			if got := tc.steps(s, &utterance{}).Lang; got != tc.want {
				t.Errorf("lang = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLanguageHelpers(t *testing.T) {
	for _, tc := range []struct {
		in   string
		norm domain.LanguageCode
	}{
		{"en", "en"},
		{"es-419", "es"},
		{"EN_us", "en"},
		{"fr", ""},
		{"", ""},
	} {
		if got := normalizeLang(tc.in); got != tc.norm {
			t.Errorf("normalizeLang(%q) = %q, want %q", tc.in, got, tc.norm)
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
	for _, tc := range []struct {
		pinned domain.LanguageCode
		want   []string
	}{{"", nil}, {"en", []string{"en-US"}}, {"es", []string{"es-419"}}} {
		if got := languageCodes(tc.pinned); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("languageCodes(%q) = %v, want %v", tc.pinned, got, tc.want)
		}
	}
}
