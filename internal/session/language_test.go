// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
)

// utterance is one scripted ASR event: the provider's detection and text.
type utterance struct {
	lang  domain.LanguageCode
	text  string
	final bool
}

func TestSourceLangHysteresis(t *testing.T) {
	en := func(text string) utterance { return utterance{"en", text, true} }
	es := func(text string) utterance { return utterance{"es", text, true} }
	tests := []struct {
		name   string
		pinned api.SourceLanguage
		script []utterance
		want   []domain.LanguageCode // each caption's source language
		final  domain.LanguageCode   // detected language at the end
	}{
		{
			name:   "first detection is taken as is",
			script: []utterance{es("sí")},
			want:   []string{"es"}, final: "es",
		},
		{
			name:   "no detection defaults to English",
			script: []utterance{{"", "hello there", false}},
			want:   []string{"en"}, final: "",
		},
		{
			name:   "long final switches",
			script: []utterance{en("welcome to the talk"), es("ahora pasamos a preguntas")},
			want:   []string{"en", "es"}, final: "es",
		},
		{
			name:   "short answers don't flip the language",
			script: []utterance{en("welcome to the talk"), es("sí"), en("so as I was saying"), es("OK"), es("vale")},
			want:   []string{"en", "en", "en", "en", "en"}, final: "en",
		},
		{
			name:   "short finals in a row add up",
			script: []utterance{en("welcome to the talk"), es("sí, claro"), es("muy bien")},
			want:   []string{"en", "en", "es"}, final: "es",
		},
		{
			name:   "a final in the session language resets the count",
			script: []utterance{en("welcome to the talk"), es("sí, claro"), en("right"), es("muy bien")},
			want:   []string{"en", "en", "en", "en"}, final: "en",
		},
		{
			name: "interims are labelled but never switch",
			script: []utterance{en("welcome to the talk"), {"es", "hola", false}, {"es", "hola a todos", false},
				{"es", "hola a todos y todas", false}},
			want: []string{"en", "en", "en", "es"}, final: "en",
		},
		{
			name:   "missing detection keeps the session language",
			script: []utterance{es("buenas tardes a todos"), {"", "gracias", true}},
			want:   []string{"es", "es"}, final: "es",
		},
		{
			name:   "pinned language wins",
			pinned: api.En,
			script: []utterance{es("buenas tardes a todos"), {"", "thanks", true}},
			want:   []string{"en", "en"}, final: "en",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := sess("main", "es")
			if tc.pinned != "" {
				s.SourceLanguage = tc.pinned
			}
			e := newEnv(t, Options{}, s)
			r := newRun(e.m, s)
			var got []domain.LanguageCode
			for _, u := range tc.script {
				got = append(got, r.sourceLang(u.lang, u.final, u.text))
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("source languages %v, want %v", got, tc.want)
			}
			if r.detected != tc.final {
				t.Errorf("detected %q, want %q", r.detected, tc.final)
			}
		})
	}
}

// scriptASR takes in all the audio, then emits its script as finals, one
// second of session clock each.
type scriptASR struct{ script []utterance }

func (a *scriptASR) Kind() domain.ProviderKind { return api.ProviderKindMock }

func (a *scriptASR) Start(ctx context.Context, _ domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	in, out := make(chan domain.AudioFrame, 16), make(chan domain.ASREvent)
	go func() {
		defer close(out)
		for range in {
		}
		for i, u := range a.script {
			ev := domain.ASREvent{SegmentID: fmt.Sprintf("u-%d", i), Text: u.text, Final: u.final, Lang: u.lang,
				Start: time.Duration(i) * time.Second, End: time.Duration(i+1) * time.Second}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return in, out, nil
}

// An English talk followed by a Spanish Q&A in one session: captions carry
// their language, viewers get a state message when it switches (not for
// the short "OK" in between), the status shows it, and the Spanish track
// passes the Spanish part through.
func TestLanguageSwitchInOneSession(t *testing.T) {
	script := []utterance{
		{"en", "Welcome everyone to this talk about observability.", true},
		{"en", "Let's start with a quick demo.", true},
		{"es", "OK", true}, // a short answer from the room
		{"en", "Any questions before we finish?", true},
		{"es", "Sí, tengo una pregunta sobre trazas.", true},
		{"es", "¿Cómo se configura el muestreo?", true},
	}
	e := newEnv(t, Options{Providers: map[domain.ProviderKind]Provider{
		api.ProviderKindMock: {ASR: &scriptASR{script: script}, Translator: &mock.Translator{}},
	}}, sess("main", "es", "en"))
	ctx := t.Context()
	sub := e.bus.Subscribe(ctx, "main", []string{"source", "es"})
	<-sub // history

	if _, err := e.m.Start(ctx, "main", &fake.Source{Duration: time.Second}); err != nil {
		t.Fatal(err)
	}
	var detected []string
	var status api.SessionStatus
	events := e.m.Events().Subscribe(ctx)
	src := map[string]string{} // segment → source language
	passthrough := map[string]bool{}
	timeout := time.After(10 * time.Second)
	for len(src) < len(script) || len(passthrough) < 2 {
		select {
		case msg := <-sub:
			switch {
			case msg.Type == api.CaptionsServerMessageTypeState && msg.DetectedLanguage != nil:
				if n := len(detected); n == 0 || detected[n-1] != *msg.DetectedLanguage {
					detected = append(detected, *msg.DetectedLanguage)
				}
			case msg.Type == api.CaptionsServerMessageTypeCaption && msg.Caption.Lang == "source":
				src[msg.Caption.SegmentId] = msg.Caption.SourceLang
			case msg.Type == api.CaptionsServerMessageTypeCaption && msg.Caption.Lang == "es" && msg.Caption.SourceLang == "es":
				if !strings.HasPrefix(msg.Caption.Text, "[") {
					passthrough[msg.Caption.SegmentId] = true
				}
			}
		case ev := <-events:
			if ev.Status != nil && ev.Status.DetectedLanguage != nil {
				status = *ev.Status
			}
		case <-timeout:
			t.Fatalf("timed out: source %v, passthrough %v", src, passthrough)
		}
	}
	waitState(t, e.m, "main", api.SessionStateIdle)

	want := []string{"en", "en", "en", "en", "es", "es"}
	for i, w := range want {
		if got := src[fmt.Sprintf("r0-u-%d", i)]; got != w {
			t.Errorf("segment %d: source language %q, want %q", i, got, w)
		}
	}
	if !reflect.DeepEqual(detected, []string{"en", "es"}) {
		t.Errorf("state messages detected %v, want [en es]", detected)
	}
	if status.DetectedLanguage == nil || *status.DetectedLanguage != "es" {
		t.Errorf("last status detectedLanguage %v, want es", status.DetectedLanguage)
	}
}
