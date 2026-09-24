// SPDX-License-Identifier: Apache-2.0

// Package fake generates synthetic audio (silence or a sine wave) for
// development and tests.
package fake

import (
	"context"
	"math"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Source is a domain.AudioSource that generates 20 ms frames.
type Source struct {
	// FreqHz is the sine frequency; 0 generates silence.
	FreqHz float64
	// Amplitude is the peak level, 0..1 of full scale.
	Amplitude float64
	// Duration stops the source after this much audio; 0 runs until ctx is cancelled.
	Duration time.Duration
	// Realtime paces frames at the wall-clock rate; otherwise they're produced as fast as read.
	Realtime bool
}

var _ domain.AudioSource = (*Source)(nil)

func (s *Source) Kind() api.AudioSourceKind { return api.AudioSourceKindFile }

func (s *Source) Err() error { return nil }

func (s *Source) Start(ctx context.Context) (<-chan domain.AudioFrame, error) {
	out := make(chan domain.AudioFrame)
	go func() {
		defer close(out)
		var tick <-chan time.Time
		if s.Realtime {
			t := time.NewTicker(domain.FrameDuration)
			defer t.Stop()
			tick = t.C
		}
		var n int // samples generated so far
		for i := 0; ; i++ {
			at := time.Duration(i) * domain.FrameDuration
			if s.Duration > 0 && at >= s.Duration {
				return
			}
			pcm := make([]int16, domain.FrameSamples)
			if s.FreqHz > 0 {
				for j := range pcm {
					phase := 2 * math.Pi * s.FreqHz * float64(n+j) / domain.SampleRate
					pcm[j] = int16(s.Amplitude * math.MaxInt16 * math.Sin(phase))
				}
			}
			n += len(pcm)
			if tick != nil {
				select {
				case <-tick:
				case <-ctx.Done():
					return
				}
			}
			select {
			case out <- domain.AudioFrame{PCM: pcm, T: at}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
