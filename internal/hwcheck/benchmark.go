// SPDX-License-Identifier: Apache-2.0

package hwcheck

import (
	"bytes"
	"context"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/translate"
)

// MaxRealTimeFactor is the highest real-time factor that counts as keeping
// up: the benchmark feeds audio faster than live and skips most interims,
// so live captions need headroom (AI-12).
const MaxRealTimeFactor = 0.8

// Error codes of the benchmark (UI-4).
const (
	CodeBenchmarkRunning     = "benchmark.running"
	CodeBenchmarkUnavailable = "benchmark.runtime_unavailable"
	CodeBenchmarkFailed      = "benchmark.failed"
	CodeBenchmarkNoSpeech    = "benchmark.no_speech"
)

// ErrBusy means a benchmark is already running.
var ErrBusy = &domain.CodedError{Code: CodeBenchmarkRunning, Message: "a benchmark is already running"}

// benchmarkTimeout bounds a whole run, model loads included.
const benchmarkTimeout = 5 * time.Minute

// minClip is how long the benchmark clip is at least; the pieces repeat
// until it is reached.
const minClip = 30 * time.Second

// clipGap is the silence between clip pieces, so the VAD ends an utterance.
const clipGap = time.Second

//go:embed clip/en.wav clip/es.wav
var clipFS embed.FS

// Clip is the bundled benchmark audio: the committed English and Spanish
// fixtures (synthetic speech) alternating, with a second of silence
// between them, for at least 30 s. 16 kHz mono.
func Clip() ([]int16, error) {
	var pieces [][]int16
	for _, name := range []string{"clip/en.wav", "clip/es.wav"} {
		b, err := clipFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		pcm, err := decodeWAV(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		pieces = append(pieces, pcm)
	}
	gap := make([]int16, int(clipGap.Seconds()*domain.SampleRate))
	var clip []int16
	for i := 0; len(clip) < int(minClip.Seconds()*domain.SampleRate); i++ {
		clip = append(clip, pieces[i%len(pieces)]...)
		clip = append(clip, gap...)
	}
	return clip, nil
}

// decodeWAV reads a 16 kHz mono s16le WAV file.
func decodeWAV(b []byte) ([]int16, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var fmtOK bool
	for p := 12; p+8 <= len(b); {
		id, size := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:]))
		body := b[p+8:]
		if size > len(body) {
			size = len(body)
		}
		body = body[:size]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, errors.New("short fmt chunk")
			}
			format, channels := binary.LittleEndian.Uint16(body), binary.LittleEndian.Uint16(body[2:])
			rate, bits := binary.LittleEndian.Uint32(body[4:]), binary.LittleEndian.Uint16(body[14:])
			if format != 1 || channels != 1 || rate != domain.SampleRate || bits != 16 {
				return nil, fmt.Errorf("want 16 kHz mono 16-bit PCM, got format %d, %d channels, %d Hz, %d bits", format, channels, rate, bits)
			}
			fmtOK = true
		case "data":
			if !fmtOK {
				return nil, errors.New("data before fmt")
			}
			pcm := make([]int16, len(body)/2)
			if err := binary.Read(bytes.NewReader(body[:2*len(pcm)]), binary.LittleEndian, pcm); err != nil {
				return nil, err
			}
			return pcm, nil
		}
		p += 8 + size + size%2
	}
	return nil, errors.New("no data chunk")
}

// benchmark runs clip through asr and translates every final with tr, one
// after the other, and reports processing time over audio time.
//
// Model loads are left out: the provider's start probe loads whisper's
// model and Warm loads Gemma's. The audio goes in as fast as the provider
// takes it, so the time is what the finals cost; live audio also gets
// interims, which MaxRealTimeFactor leaves room for.
func benchmark(ctx context.Context, asr domain.ASRProvider, tr domain.Translator, clip []int16, now func() time.Time) (api.BenchmarkResult, error) {
	ctx, cancel := context.WithTimeout(ctx, benchmarkTimeout)
	defer cancel()
	in, out, err := asr.Start(ctx, domain.ASRConfig{SessionID: "benchmark", SourceLanguage: api.Auto})
	if err != nil {
		var coded *domain.CodedError
		if errors.As(err, &coded) {
			return api.BenchmarkResult{}, err
		}
		return api.BenchmarkResult{}, unavailable("whisper-server", err)
	}
	if w, ok := tr.(translate.Warmer); ok {
		if err := w.Warm(ctx); err != nil {
			cancel()
			for range out { // drain until the provider stops
			}
			return api.BenchmarkResult{}, unavailable("Ollama", err)
		}
	}

	start := now()
	go func() {
		defer close(in)
		var t time.Duration
		for i := 0; i < len(clip); i += domain.FrameSamples {
			f := domain.AudioFrame{PCM: clip[i:min(i+domain.FrameSamples, len(clip))], T: t}
			select {
			case in <- f:
			case <-ctx.Done():
				return
			}
			t += domain.FrameDuration
		}
	}()
	var finals []domain.ASREvent
	var asrErr error
	for ev := range out {
		switch {
		case ev.Err != nil:
			asrErr = ev.Err
		case ev.Final && ev.Text != "":
			finals = append(finals, ev)
		}
	}
	asrTime := now().Sub(start)
	if err := ctx.Err(); err != nil {
		return api.BenchmarkResult{}, failed("speech recognition", err)
	}
	if asrErr != nil {
		return api.BenchmarkResult{}, failed("speech recognition", asrErr)
	}
	if len(finals) == 0 {
		return api.BenchmarkResult{}, &domain.CodedError{Code: CodeBenchmarkNoSpeech,
			Message: "whisper-server returned no text for the benchmark clip; check its model"}
	}

	start = now()
	var prev []string
	for _, f := range finals {
		to := domain.LanguageCode("es")
		if f.Lang == "es" {
			to = "en"
		}
		_, err := tr.Translate(ctx, domain.TranslateRequest{
			SessionID: "benchmark", SegmentID: f.SegmentID, Text: f.Text,
			From: f.Lang, To: to, Final: true, Context: prev,
		})
		if err != nil {
			return api.BenchmarkResult{}, failed("translation", err)
		}
		prev = append(prev, f.Text)
	}
	trTime := now().Sub(start)

	audio := time.Duration(len(clip)) * time.Second / domain.SampleRate
	rtf := float32((asrTime + trTime).Seconds() / audio.Seconds())
	maxRTF := float32(MaxRealTimeFactor)
	audioMs := int(audio.Milliseconds())
	return api.BenchmarkResult{
		RealTimeFactor:    rtf,
		AsrMs:             int(asrTime.Milliseconds()),
		TranslationMs:     int(trTime.Milliseconds()),
		Ok:                rtf <= maxRTF,
		RanAt:             now().UTC(),
		AudioMs:           &audioMs,
		MaxRealTimeFactor: &maxRTF,
	}, nil
}

func unavailable(runtime string, err error) error {
	return &domain.CodedError{Code: CodeBenchmarkUnavailable, Params: map[string]any{"runtime": runtime},
		Message: fmt.Sprintf("%s isn't available for the benchmark: %v", runtime, err)}
}

func failed(stage string, err error) error {
	return &domain.CodedError{Code: CodeBenchmarkFailed, Params: map[string]any{"stage": stage},
		Message: fmt.Sprintf("benchmark %s failed: %v", stage, err)}
}
