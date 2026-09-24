// SPDX-License-Identifier: Apache-2.0

package audio

import (
	"math"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// FloorDBFS is the lowest level reported. Digital silence would be -Inf,
// which JSON can't carry; 16-bit audio has ~96 dB of dynamic range.
const FloorDBFS = -96.0

// fullScale is the magnitude of a full-scale s16 sample.
const fullScale = 32768.0

// RMSDBFS returns the RMS level of pcm in dBFS, where a full-scale square
// wave is 0 dBFS and a full-scale sine is about -3 dBFS.
func RMSDBFS(pcm []int16) float64 {
	if len(pcm) == 0 {
		return FloorDBFS
	}
	var sum float64
	for _, s := range pcm {
		v := float64(s)
		sum += v * v
	}
	return toDBFS(math.Sqrt(sum / float64(len(pcm))))
}

// PeakDBFS returns the level of the largest sample magnitude in pcm in dBFS.
func PeakDBFS(pcm []int16) float64 {
	var peak float64
	for _, s := range pcm {
		peak = max(peak, math.Abs(float64(s)))
	}
	return toDBFS(peak)
}

func toDBFS(amplitude float64) float64 {
	if amplitude <= 0 {
		return FloorDBFS
	}
	return max(FloorDBFS, 20*math.Log10(amplitude/fullScale))
}

// Level is one reading of a LevelMeter.
type Level struct {
	// RMSDBFS and PeakDBFS are measured over the last complete window.
	RMSDBFS  float64
	PeakDBFS float64
	// Silent is set once the RMS level has stayed below the silence
	// threshold for LevelMeterConfig.SilenceAfter.
	Silent bool
	// Clipping is set while a clipped sample was seen within the last
	// LevelMeterConfig.ClipHold.
	Clipping bool
}

// LevelMeterConfig tunes a LevelMeter. Zero fields take the defaults.
type LevelMeterConfig struct {
	// Window is the measurement window for RMS and peak (default 100 ms).
	Window time.Duration
	// SilenceDBFS is the RMS level below which a window counts as silent
	// (default -50 dBFS).
	SilenceDBFS float64
	// SilenceAfter is how long the level must stay below SilenceDBFS before
	// Silent is reported (default 3 s).
	SilenceAfter time.Duration
	// ClipLevel is the sample magnitude counted as clipped (default 32767,
	// i.e. the rails).
	ClipLevel int
	// ClipHold keeps Clipping set for this long after the last clipped
	// sample, so a short burst is still visible to a slow poller (default 1 s).
	ClipHold time.Duration
}

func (c LevelMeterConfig) withDefaults() LevelMeterConfig {
	if c.Window <= 0 {
		c.Window = 100 * time.Millisecond
	}
	if c.SilenceDBFS == 0 {
		c.SilenceDBFS = -50
	}
	if c.SilenceAfter <= 0 {
		c.SilenceAfter = 3 * time.Second
	}
	if c.ClipLevel <= 0 {
		c.ClipLevel = math.MaxInt16
	}
	if c.ClipHold <= 0 {
		c.ClipHold = time.Second
	}
	return c
}

// samples converts a duration to a sample count at domain.SampleRate.
func samples(d time.Duration) int {
	return max(1, int(d*domain.SampleRate/time.Second))
}

// LevelMeter measures RMS and peak levels over fixed windows and flags
// silence and clipping (AUD-6). Time is counted in samples, so readings are
// deterministic for a given input. It is not safe for concurrent use.
type LevelMeter struct {
	window, silenceAfter, clipHold int // in samples
	silenceDBFS                    float64
	clipLevel                      int

	sumSq     float64 // current window
	peak      int
	n         int
	silentN   int  // consecutive samples in silent windows
	sinceClip int  // samples since the last clipped sample
	clipped   bool // a clipped sample was ever seen
	level     Level
}

// NewLevelMeter returns a meter; until the first window completes it
// reports FloorDBFS and not silent.
func NewLevelMeter(cfg LevelMeterConfig) *LevelMeter {
	cfg = cfg.withDefaults()
	return &LevelMeter{
		window:       samples(cfg.Window),
		silenceAfter: samples(cfg.SilenceAfter),
		clipHold:     samples(cfg.ClipHold),
		silenceDBFS:  cfg.SilenceDBFS,
		clipLevel:    cfg.ClipLevel,
		level:        Level{RMSDBFS: FloorDBFS, PeakDBFS: FloorDBFS},
	}
}

// Write feeds samples to the meter.
func (m *LevelMeter) Write(pcm []int16) {
	for _, s := range pcm {
		a := int(s)
		if a < 0 {
			a = -a
		}
		m.sumSq += float64(a) * float64(a)
		m.peak = max(m.peak, a)
		m.n++
		if a >= m.clipLevel {
			m.clipped = true
			m.sinceClip = 0
		} else {
			m.sinceClip++
		}
		if m.n == m.window {
			m.closeWindow()
		}
	}
	m.level.Clipping = m.clipped && m.sinceClip < m.clipHold
}

func (m *LevelMeter) closeWindow() {
	m.level.RMSDBFS = toDBFS(math.Sqrt(m.sumSq / float64(m.n)))
	m.level.PeakDBFS = toDBFS(float64(m.peak))
	if m.level.RMSDBFS < m.silenceDBFS {
		m.silentN += m.n
	} else {
		m.silentN = 0
	}
	m.level.Silent = m.silentN >= m.silenceAfter
	m.sumSq, m.peak, m.n = 0, 0, 0
}

// Level returns the latest reading.
func (m *LevelMeter) Level() Level { return m.level }
