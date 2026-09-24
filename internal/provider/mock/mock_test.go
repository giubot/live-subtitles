// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// run feeds d of fake audio through a mock ASR and collects its events.
func run(t *testing.T, asr *ASR, lang domain.SourceLanguage, d time.Duration) []domain.ASREvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frames, _ := (&fake.Source{Duration: d}).Start(ctx)
	in, out, err := asr.Start(ctx, domain.ASRConfig{SessionID: "main", SourceLanguage: lang})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(in)
		for f := range frames {
			in <- f
		}
	}()
	var evs []domain.ASREvent
	for ev := range out {
		evs = append(evs, ev)
	}
	if ctx.Err() != nil {
		t.Fatal("timed out")
	}
	return evs
}

func finals(evs []domain.ASREvent) []domain.ASREvent {
	var fs []domain.ASREvent
	for _, ev := range evs {
		if ev.Final {
			fs = append(fs, ev)
		}
	}
	return fs
}

func TestASRFollowsScript(t *testing.T) {
	tests := []struct {
		lang      domain.SourceLanguage
		wantLangs []string
	}{
		{api.Auto, []string{"en", "en", "en", "es", "es", "es", "en", "en"}},
		{api.Es, []string{"es", "es", "es", "es", "es", "es", "es", "es"}},
		{api.En, []string{"en", "en", "en", "en", "en", "en", "en", "en"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.lang), func(t *testing.T) {
			evs := run(t, &ASR{WordEvery: 100 * time.Millisecond}, tt.lang, 2*time.Minute)
			fs := finals(evs)
			if len(fs) < len(DefaultScript) {
				t.Fatalf("got %d finals, want at least %d", len(fs), len(DefaultScript))
			}
			for i, f := range fs[:len(DefaultScript)] {
				want := DefaultScript[i].EN
				if tt.wantLangs[i] == "es" {
					want = DefaultScript[i].ES
				}
				if f.Text != want || f.Lang != tt.wantLangs[i] {
					t.Errorf("final %d = %q (%s), want %q (%s)", i, f.Text, f.Lang, want, tt.wantLangs[i])
				}
			}
		})
	}
}

func TestASRInterimsBuildUpToFinal(t *testing.T) {
	evs := run(t, &ASR{WordEvery: 100 * time.Millisecond}, api.Auto, 30*time.Second)
	var prev domain.ASREvent
	for i, ev := range evs {
		if i > 0 && !prev.Final {
			if ev.SegmentID != prev.SegmentID {
				t.Fatalf("event %d: segment changed from %s to %s before a final", i, prev.SegmentID, ev.SegmentID)
			}
			if !strings.HasPrefix(ev.Text, prev.Text) || len(ev.Text) <= len(prev.Text) {
				t.Fatalf("event %d: %q does not extend %q", i, ev.Text, prev.Text)
			}
			if ev.Start != prev.Start {
				t.Fatalf("event %d: start moved within a segment", i)
			}
		}
		if i > 0 && prev.Final && ev.Start < prev.End {
			t.Fatalf("event %d starts at %v, before the previous segment ended at %v", i, ev.Start, prev.End)
		}
		if ev.End <= ev.Start {
			t.Fatalf("event %d: end %v <= start %v", i, ev.End, ev.Start)
		}
		prev = ev
	}
	if !prev.Final {
		t.Fatal("stream did not end with a final event")
	}
}

func TestASRFlushesOnEndOfAudio(t *testing.T) {
	// 1 s at 300 ms per word (words due at 0, 0.3, 0.6, 0.9 s) stops mid-sentence.
	evs := run(t, &ASR{}, api.En, time.Second)
	last := evs[len(evs)-1]
	if !last.Final || last.Text != "Welcome everyone, and thanks" {
		t.Fatalf("last event = %+v, want final \"Welcome everyone, and thanks\"", last)
	}
}

func TestASRLatency(t *testing.T) {
	ctx := context.Background()
	in, out, _ := (&ASR{Latency: 50 * time.Millisecond}).Start(ctx, domain.ASRConfig{SourceLanguage: api.En})
	sent := time.Now()
	in <- domain.AudioFrame{PCM: make([]int16, domain.SampleRate)} // 1 s of audio
	<-out
	if d := time.Since(sent); d < 50*time.Millisecond {
		t.Fatalf("first event after %v, want >= 50ms", d)
	}
	close(in)
	for range out {
	}
}

func TestTranslator(t *testing.T) {
	tests := []struct {
		name, text, from, to, want string
	}{
		{"full line en→es", "Welcome everyone, and thanks for joining this session.", "en", "es", "Bienvenidos a todos, y gracias por sumarse a esta sesión."},
		{"full line es→en", "¿Alguna pregunta antes de pasar a trazas?", "es", "en", "Any questions before we move on to tracing?"},
		{"interim prefix", "Today we are going to", "en", "es", "Hoy vamos a hablar"},
		{"passthrough", "hola", "es", "es", "hola"},
		{"unknown text", "something else", "en", "es", "[es] something else"},
		{"other target", "Welcome everyone,", "en", "pt", "[pt] Welcome everyone,"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := (&Translator{}).Translate(context.Background(), domain.TranslateRequest{Text: tt.text, From: tt.from, To: tt.to})
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != tt.want {
				t.Errorf("got %q, want %q", res.Text, tt.want)
			}
		})
	}
}
