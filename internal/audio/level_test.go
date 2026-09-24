// SPDX-License-Identifier: Apache-2.0

package audio

import (
	"math"
	"testing"
	"time"
)

func sine(n int, amplitude float64) []int16 {
	pcm := make([]int16, n)
	for i := range pcm {
		pcm[i] = int16(math.Round(amplitude * math.MaxInt16 * math.Sin(2*math.Pi*1000*float64(i)/16000)))
	}
	return pcm
}

func constant(n int, v int16) []int16 {
	pcm := make([]int16, n)
	for i := range pcm {
		pcm[i] = v
	}
	return pcm
}

func TestDBFS(t *testing.T) {
	tests := []struct {
		name      string
		pcm       []int16
		rms, peak float64
		tolerance float64
	}{
		{"empty", nil, FloorDBFS, FloorDBFS, 0},
		{"digital silence", make([]int16, 1600), FloorDBFS, FloorDBFS, 0},
		{"full-scale sine", sine(1600, 1), -3.01, 0, 0.02},
		{"half-scale sine", sine(1600, 0.5), -9.03, -6.02, 0.02},
		{"full-scale square", constant(1600, math.MinInt16), 0, 0, 0.001},
		{"one LSB", constant(1600, 1), -90.31, -90.31, 0.01},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RMSDBFS(tt.pcm); math.Abs(got-tt.rms) > tt.tolerance {
				t.Errorf("RMS = %.3f, want %.3f", got, tt.rms)
			}
			if got := PeakDBFS(tt.pcm); math.Abs(got-tt.peak) > tt.tolerance {
				t.Errorf("peak = %.3f, want %.3f", got, tt.peak)
			}
		})
	}
}

func TestLevelMeter(t *testing.T) {
	cfg := LevelMeterConfig{Window: 100 * time.Millisecond, SilenceAfter: time.Second, ClipHold: 500 * time.Millisecond}
	sec := func(d float64) int { return int(d * 16000) }
	tests := []struct {
		name  string
		feed  [][]int16
		want  Level
		check func(t *testing.T, l Level)
	}{
		{
			name: "nothing yet",
			want: Level{RMSDBFS: FloorDBFS, PeakDBFS: FloorDBFS},
		},
		{
			name: "short silence is not silent yet",
			feed: [][]int16{make([]int16, sec(0.5))},
			want: Level{RMSDBFS: FloorDBFS, PeakDBFS: FloorDBFS},
		},
		{
			name: "silence for the threshold",
			feed: [][]int16{make([]int16, sec(1))},
			want: Level{RMSDBFS: FloorDBFS, PeakDBFS: FloorDBFS, Silent: true},
		},
		{
			name: "quiet noise below threshold counts as silence",
			feed: [][]int16{constant(sec(1.2), 30)}, // ≈ -60.8 dBFS
			check: func(t *testing.T, l Level) {
				if !l.Silent || l.Clipping {
					t.Errorf("got %+v, want silent, not clipping", l)
				}
			},
		},
		{
			name: "sound resets silence",
			feed: [][]int16{make([]int16, sec(2)), sine(sec(0.1), 0.5)},
			check: func(t *testing.T, l Level) {
				if l.Silent || math.Abs(l.RMSDBFS+9.03) > 0.05 || math.Abs(l.PeakDBFS+6.02) > 0.05 {
					t.Errorf("got %+v, want not silent at -9/-6 dBFS", l)
				}
			},
		},
		{
			name: "full-scale sine clips",
			feed: [][]int16{sine(sec(0.2), 1)},
			check: func(t *testing.T, l Level) {
				if !l.Clipping || math.Abs(l.RMSDBFS+3.01) > 0.05 || l.PeakDBFS < -0.01 {
					t.Errorf("got %+v, want clipping at -3/0 dBFS", l)
				}
			},
		},
		{
			name: "clipping is held then released",
			feed: [][]int16{constant(10, math.MaxInt16), sine(sec(0.4), 0.5)},
			check: func(t *testing.T, l Level) {
				if !l.Clipping {
					t.Errorf("got %+v, want clipping still held", l)
				}
			},
		},
		{
			name: "clipping released after hold",
			feed: [][]int16{constant(10, math.MinInt16), sine(sec(0.6), 0.5)},
			check: func(t *testing.T, l Level) {
				if l.Clipping {
					t.Errorf("got %+v, want clipping released", l)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewLevelMeter(cfg)
			for _, pcm := range tt.feed {
				// Feed in odd-sized chunks so windows straddle writes.
				for len(pcm) > 0 {
					n := min(len(pcm), 333)
					m.Write(pcm[:n])
					pcm = pcm[n:]
				}
			}
			got := m.Level()
			if tt.check != nil {
				tt.check(t, got)
			} else if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
