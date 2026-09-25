// SPDX-License-Identifier: Apache-2.0

//go:build whisper

// Checks against a real whisper-server:
//
//	WHISPER_URL=http://127.0.0.1:8178 go test -tags whisper -run Real -v ./internal/provider/local/whisper/
//
// WHISPER_MODEL names the model it runs (for the log; default large-v3-turbo).
// WHISPER_EN_URL, if set, is a server running an English-only model, which
// Start must refuse.

package whisper

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

var fixtureText = map[string]string{
	"en": "Welcome everyone. Today we are going to talk about observability in Kubernetes, and how metrics, logs and traces fit together.",
	"es": "Bienvenidos a todos. Hoy vamos a hablar de observabilidad en Kubernetes, y de cómo se combinan las métricas, los logs y las trazas.",
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestRealFixtures(t *testing.T) {
	url, model := envOr("WHISPER_URL", DefaultURL), envOr("WHISPER_MODEL", DefaultModel)
	for _, lang := range []string{"es", "en"} {
		for _, source := range []api.SourceLanguage{api.Auto, api.SourceLanguage(lang)} {
			t.Run(lang+"/"+string(source), func(t *testing.T) {
				pcm := readWAV(t, filepath.Join("..", "..", "..", "..", "testdata", "audio", "fixtures", lang+".wav"))
				p := &Provider{Settings: settingsFor(url, model)}
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				in, out, err := p.Start(ctx, domain.ASRConfig{SessionID: "real", SourceLanguage: source})
				if err != nil {
					t.Fatalf("start: %v", err)
				}
				// Real time, so interims and finals are paced like a talk.
				begin := time.Now()
				go func() {
					defer close(in)
					for _, f := range frames(pcm, 0) {
						time.Sleep(time.Until(begin.Add(f.End())))
						in <- f
					}
				}()
				var finals []string
				interims := 0
				for ev := range out {
					if ev.Err != nil {
						t.Fatalf("error: %v", ev.Err)
					}
					if !ev.Final {
						interims++
						continue
					}
					// Latency: wall time since the audio of the segment was sent.
					lat := time.Since(begin.Add(ev.End))
					t.Logf("final %s [%.2f–%.2f s] %s latency %v: %q", ev.SegmentID, ev.Start.Seconds(), ev.End.Seconds(), ev.Lang, lat.Round(time.Millisecond), ev.Text)
					if ev.Lang != domain.LanguageCode(lang) {
						t.Errorf("%s detected as %s", ev.SegmentID, ev.Lang)
					}
					finals = append(finals, ev.Text)
				}
				got := strings.Join(finals, " ")
				wer := wordErrorRate(fixtureText[lang], got)
				t.Logf("model %s, %d interims, WER %.1f%%", model, interims, wer*100)
				if wer > 0.35 {
					t.Errorf("WER %.0f%%: %q", wer*100, got)
				}
			})
		}
	}
}

func TestRealEnglishOnlyRefused(t *testing.T) {
	url := os.Getenv("WHISPER_EN_URL")
	if url == "" {
		t.Skip("WHISPER_EN_URL not set")
	}
	p := &Provider{Settings: settingsFor(url, "large-v3-turbo")} // the name hides it
	_, _, err := p.Start(t.Context(), domain.ASRConfig{})
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != CodeEnglishOnlyModel {
		t.Errorf("start: %v, want %s", err, CodeEnglishOnlyModel)
	}
}

// readWAV reads the samples of a 16 kHz mono s16le WAV file.
func readWAV(t *testing.T, path string) []int16 {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(b, []byte("data"))
	if i < 0 || binary.LittleEndian.Uint32(b[24:]) != domain.SampleRate {
		t.Fatalf("%s: not a 16 kHz WAV", path)
	}
	data := b[i+8:]
	pcm := make([]int16, len(data)/2)
	for j := range pcm {
		pcm[j] = int16(binary.LittleEndian.Uint16(data[2*j:]))
	}
	return pcm
}

// wordErrorRate is the word-level edit distance over the reference length.
func wordErrorRate(ref, hyp string) float64 {
	r, h := strings.Fields(normalize(ref)), strings.Fields(normalize(hyp))
	prev := make([]int, len(h)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(r); i++ {
		cur := make([]int, len(h)+1)
		cur[0] = i
		for j := 1; j <= len(h); j++ {
			sub := prev[j-1]
			if r[i-1] != h[j-1] {
				sub++
			}
			cur[j] = min(sub, prev[j]+1, cur[j-1]+1)
		}
		prev = cur
	}
	return float64(prev[len(h)]) / float64(len(r))
}
