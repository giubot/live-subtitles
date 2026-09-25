// SPDX-License-Identifier: Apache-2.0

//go:build gemini

// Run against the real Live API:
//
//	GEMINI_API_KEY=… go test -tags gemini -run Integration -v ./internal/provider/gemini/
//
// GEMINI_LIVE_MODEL overrides the model.

package gemini

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestIntegrationLiveASR(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		t.Skip("set GEMINI_API_KEY or GOOGLE_API_KEY")
	}
	for _, tc := range []struct {
		clip   string
		source api.SourceLanguage
		want   domain.LanguageCode
	}{
		{"en.wav", api.Auto, "en"},
		{"es.wav", api.Auto, "es"},
		{"es.wav", api.Es, "es"},
	} {
		t.Run(tc.clip+"/"+string(tc.source), func(t *testing.T) {
			pcm := readWAV(t, filepath.Join("..", "..", "..", "testdata", "audio", "fixtures", tc.clip))
			a := &ASR{
				APIKey: func(context.Context) (string, error) { return key, nil },
				Settings: func(context.Context) (api.Settings, error) {
					var s api.Settings
					s.Providers.Gemini.LiveModel = os.Getenv("GEMINI_LIVE_MODEL")
					return s, nil
				},
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			in, out, err := a.Start(ctx, domain.ASRConfig{SessionID: "it", SourceLanguage: tc.source})
			if err != nil {
				t.Fatal(err)
			}
			origin := time.Now()
			go func() {
				defer close(in)
				// The clip in real time, then 2 s of silence.
				pcm = append(pcm, make([]int16, 2*domain.SampleRate)...)
				for i := 0; i+domain.FrameSamples <= len(pcm); i += domain.FrameSamples {
					f := domain.AudioFrame{PCM: pcm[i : i+domain.FrameSamples], T: time.Duration(i) * time.Second / domain.SampleRate}
					time.Sleep(time.Until(origin.Add(f.T)))
					select {
					case in <- f:
					case <-ctx.Done():
						return
					}
				}
			}()
			var (
				finals         []string
				langs          []domain.LanguageCode
				first          time.Duration
				finalLatency   []time.Duration
				usage          domain.Usage
				interimLatency []time.Duration
			)
			for ev := range out {
				usage = usage.Add(ev.Usage)
				if ev.Err != nil {
					t.Logf("provider error: %v", ev.Err)
					continue
				}
				if ev.Text == "" {
					continue
				}
				lat := time.Since(origin) - ev.End
				if first == 0 {
					first = time.Since(origin)
				}
				if ev.Final {
					finals = append(finals, ev.Text)
					langs = append(langs, ev.Lang)
					finalLatency = append(finalLatency, lat)
				} else {
					interimLatency = append(interimLatency, lat)
				}
			}
			t.Logf("finals: %q", finals)
			t.Logf("langs: %v, first text after %v, interim latency %v, final latency %v, usage %+v",
				langs, first, interimLatency, finalLatency, usage)
			if len(finals) == 0 || strings.TrimSpace(strings.Join(finals, "")) == "" {
				t.Fatal("no final transcription")
			}
			if !slices.Contains(langs, tc.want) {
				t.Errorf("languages %v, want %s", langs, tc.want)
			}
			if usage.AudioSeconds < 8 {
				t.Errorf("audio seconds = %v", usage.AudioSeconds)
			}
		})
	}
}

// readWAV reads the samples of a 16 kHz mono s16le WAV file.
func readWAV(t *testing.T, path string) []int16 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(b, []byte("data"))
	if !bytes.HasPrefix(b, []byte("RIFF")) || i < 0 || i+8 > len(b) {
		t.Fatalf("%s: not a WAV file", path)
	}
	data := b[i+8:]
	pcm := make([]int16, len(data)/2)
	for j := range pcm {
		pcm[j] = int16(binary.LittleEndian.Uint16(data[2*j:]))
	}
	return pcm
}
