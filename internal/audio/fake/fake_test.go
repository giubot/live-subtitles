// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"context"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestSource(t *testing.T) {
	tests := []struct {
		name     string
		src      Source
		wantPeak int16 // minimum absolute peak over all frames
		maxPeak  int16
	}{
		{"silence", Source{Duration: time.Second}, 0, 0},
		{"sine", Source{FreqHz: 440, Amplitude: 0.5, Duration: time.Second}, 16000, 16384},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := tt.src.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var frames int
			var peak int16
			var last time.Duration
			for f := range ch {
				if len(f.PCM) != domain.FrameSamples {
					t.Fatalf("frame %d has %d samples", frames, len(f.PCM))
				}
				if frames > 0 && f.T != last+domain.FrameDuration {
					t.Fatalf("frame %d at %v, want %v", frames, f.T, last+domain.FrameDuration)
				}
				last = f.T
				for _, s := range f.PCM {
					peak = max(peak, s, -s)
				}
				frames++
			}
			if frames != 50 {
				t.Errorf("got %d frames, want 50", frames)
			}
			if peak < tt.wantPeak || peak > tt.maxPeak {
				t.Errorf("peak %d, want %d..%d", peak, tt.wantPeak, tt.maxPeak)
			}
		})
	}
}

func TestSourceStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := (&Source{}).Start(ctx)
	<-ch
	cancel()
	for range ch {
	}
}
