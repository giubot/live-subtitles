// SPDX-License-Identifier: Apache-2.0

package whisper

import "testing"

func seg(text string) segment { return segment{Text: text, AvgLogprob: -0.2, NoSpeechProb: 0.01} }

func TestTranscript(t *testing.T) {
	tests := []struct {
		name string
		resp response
		want string
	}{
		{"plain", response{Segments: []segment{seg(" Hola a todos,"), seg(" bienvenidos.")}}, "Hola a todos, bienvenidos."},
		{"no segments uses the text", response{Text: " Hello there.\n"}, "Hello there."},
		{"music tag", response{Segments: []segment{seg(" [Música]")}}, ""},
		{"blank audio", response{Segments: []segment{seg(" [BLANK_AUDIO]")}}, ""},
		{"tags inside speech", response{Segments: []segment{seg(" (applause) Thank you all ♪")}}, "Thank you all"},
		{"amara", response{Segments: []segment{seg(" Subtítulos realizados por la comunidad de Amara.org")}}, ""},
		{"subtitles by", response{Segments: []segment{seg(" Subtitles by the Amara.org community")}}, ""},
		{"thanks for watching", response{Segments: []segment{seg(" Thanks for watching!")}}, ""},
		{"gracias por ver", response{Segments: []segment{seg(" ¡Gracias por ver el video!")}}, ""},
		{"subscribe", response{Segments: []segment{seg(" Please subscribe to my channel.")}}, ""},
		{"a talk about subtitles is kept", response{Segments: []segment{seg(" We added subtitles by default.")}}, "We added subtitles by default."},
		{"subscribe inside a sentence is kept", response{Segments: []segment{seg(" You can subscribe to the topic.")}}, "You can subscribe to the topic."},
		{"filler", response{Segments: []segment{seg(" Mmm.")}}, ""},
		{"confident thank you is kept", response{Segments: []segment{seg(" Thank you.")}}, "Thank you."},
		{"doubtful thank you is dropped", response{Segments: []segment{{Text: " Thank you.", AvgLogprob: -0.9, NoSpeechProb: 0.2}}}, ""},
		{"doubtful gracias is dropped", response{Segments: []segment{{Text: " Gracias.", AvgLogprob: -0.3, NoSpeechProb: 0.5}}}, ""},
		{"silent segment is dropped", response{Segments: []segment{seg(" Hola."), {Text: " Chau.", AvgLogprob: -1.2, NoSpeechProb: 0.8}}}, "Hola."},
		{"punctuation only", response{Segments: []segment{seg(" ...")}}, ""},
		{"looping phrase", response{Segments: []segment{seg(" la verdad es que la verdad es que la verdad es que la verdad es que funciona")}}, "la verdad es que funciona"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := transcript(tt.resp); got != tt.want {
				t.Errorf("transcript = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollapseRepeats(t *testing.T) {
	tests := []struct{ in, want string }{
		{"no, no, no", "no, no, no"},
		{"y y y y y y y entonces", "y entonces"},
		{"so we we go", "so we we go"},
		{"thank you thank you", "thank you thank you"},
		{"thank you thank you thank you thank you.", "thank you"},
		{"Muy bien. Muy bien. Muy bien. Sigamos", "Muy bien. Sigamos"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := collapseRepeats(fields(tt.in)); got != tt.want {
			t.Errorf("collapseRepeats(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func fields(s string) []string {
	var out []string
	word := ""
	for _, r := range s + " " {
		if r == ' ' {
			if word != "" {
				out = append(out, word)
			}
			word = ""
			continue
		}
		word += string(r)
	}
	return out
}

func TestIsEnglishOnlyModel(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"large-v3-turbo", false},
		{"medium", false},
		{"ggml-small.bin", false},
		{"models/ggml-large-v3-turbo-q5_0.bin", false},
		{"base.en", true},
		{"tiny.en", true},
		{"ggml-base.en.bin", true},
		{"small.en-q5_1", true},
		{"/opt/models/ggml-medium.en.bin", true},
		{`C:\models\ggml-tiny.en.bin`, true},
		{"distil-medium.en", true},
		{"Small.EN", true},
		{"encoder-large", false},
	}
	for _, tt := range tests {
		if got := IsEnglishOnlyModel(tt.name); got != tt.want {
			t.Errorf("IsEnglishOnlyModel(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
