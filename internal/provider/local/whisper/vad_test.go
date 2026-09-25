// SPDX-License-Identifier: Apache-2.0

package whisper

import (
	"math"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// part is a stretch of synthetic audio: a tone at dbfs, or silence.
type part struct {
	d    time.Duration
	dbfs float64 // 0 means silence
}

func tone(d time.Duration) part    { return part{d, -12} }
func silence(d time.Duration) part { return part{d: d} }

// synth renders parts as 16 kHz PCM: a 220 Hz sine, or digital silence.
func synth(parts ...part) []int16 {
	var pcm []int16
	for _, p := range parts {
		n := int(p.d * domain.SampleRate / time.Second)
		amp := 0.0
		if p.dbfs != 0 {
			amp = math.Sqrt2 * 32768 * math.Pow(10, p.dbfs/20)
		}
		for range n {
			pcm = append(pcm, int16(amp*math.Sin(2*math.Pi*220*float64(len(pcm))/domain.SampleRate)))
		}
	}
	return pcm
}

// frames splits pcm into 20 ms frames starting at t0.
func frames(pcm []int16, t0 time.Duration) []domain.AudioFrame {
	var out []domain.AudioFrame
	for i := 0; i < len(pcm); i += domain.FrameSamples {
		end := min(len(pcm), i+domain.FrameSamples)
		out = append(out, domain.AudioFrame{PCM: pcm[i:end], T: t0 + samplesDur(i)})
	}
	return out
}

func segmentAll(cfg VADConfig, fs []domain.AudioFrame, flush bool) []chunk {
	s := newSegmenter(cfg)
	var out []chunk
	for _, f := range fs {
		out = append(out, s.write(f)...)
	}
	if flush {
		if c, ok := s.flush(); ok {
			out = append(out, c)
		}
	}
	return out
}

func finalsOf(cs []chunk) []chunk {
	var out []chunk
	for _, c := range cs {
		if c.final {
			out = append(out, c)
		}
	}
	return out
}

const ms = time.Millisecond

func near(a, b time.Duration) bool { return (a - b).Abs() <= 25*ms }

func TestSegmenter(t *testing.T) {
	type span struct{ start, end time.Duration }
	tests := []struct {
		name     string
		audio    []part
		t0       time.Duration
		flush    bool
		finals   []span
		interims int
	}{
		{
			name:     "one utterance ends on a pause",
			audio:    []part{tone(2500 * ms), silence(time.Second)},
			finals:   []span{{0, 2700 * ms}},
			interims: 2, // at 1 s and 2 s
		},
		{
			name:   "two utterances keep the pre-roll",
			audio:  []part{silence(time.Second), tone(1500 * ms), silence(time.Second), tone(800 * ms), silence(time.Second)},
			finals: []span{{700 * ms, 2700 * ms}, {3200 * ms, 4500 * ms}},
		},
		{
			name:   "session clock offset",
			audio:  []part{tone(1500 * ms), silence(time.Second)},
			t0:     42 * time.Second,
			finals: []span{{42 * time.Second, 43700 * ms}},
		},
		{
			name:  "a click is not speech",
			audio: []part{silence(time.Second), tone(100 * ms), silence(2 * time.Second)},
		},
		{
			name:  "quiet noise is not speech",
			audio: []part{{3 * time.Second, -62}},
		},
		{
			name:   "flush commits the utterance in progress",
			audio:  []part{tone(1500 * ms)},
			flush:  true,
			finals: []span{{0, 1500 * ms}},
		},
		{
			name:  "flush with too little speech drops it",
			audio: []part{tone(200 * ms)},
			flush: true,
		},
		{
			name:   "a short pause doesn't split",
			audio:  []part{tone(time.Second), silence(300 * ms), tone(time.Second), silence(time.Second)},
			finals: []span{{0, 2500 * ms}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := segmentAll(VADConfig{}, frames(synth(tt.audio...), tt.t0), tt.flush)
			fin := finalsOf(cs)
			if len(fin) != len(tt.finals) {
				t.Fatalf("%d finals, want %d: %+v", len(fin), len(tt.finals), spans(fin))
			}
			for i, f := range fin {
				if !near(f.start, tt.finals[i].start) || !near(f.end, tt.finals[i].end) {
					t.Errorf("final %d: %v–%v, want %v–%v", i, f.start, f.end, tt.finals[i].start, tt.finals[i].end)
				}
				if f.utterance != i+1 {
					t.Errorf("final %d: utterance %d", i, f.utterance)
				}
				if got := samplesDur(len(f.pcm)); !near(got, f.duration()) {
					t.Errorf("final %d: %v of audio for %v", i, got, f.duration())
				}
			}
			if tt.interims > 0 {
				if n := len(cs) - len(fin); n != tt.interims {
					t.Errorf("%d interims, want %d", n, tt.interims)
				}
			}
		})
	}
}

func spans(cs []chunk) [][2]time.Duration {
	var out [][2]time.Duration
	for _, c := range cs {
		out = append(out, [2]time.Duration{c.start, c.end})
	}
	return out
}

func TestSegmenterInterimsGrow(t *testing.T) {
	cs := segmentAll(VADConfig{}, frames(synth(tone(4*time.Second), silence(time.Second)), 0), false)
	var last time.Duration
	for _, c := range cs {
		if c.utterance != 1 {
			t.Fatalf("utterance %d", c.utterance)
		}
		if c.start != 0 || c.end <= last {
			t.Errorf("chunk %v–%v after %v: interims must re-cover the utterance and grow", c.start, c.end, last)
		}
		last = c.end
	}
	if !cs[len(cs)-1].final {
		t.Error("the last chunk is not final")
	}
}

func TestSegmenterMaxUtterance(t *testing.T) {
	// 30 s without a pause, with a dip every 5 s to cut at.
	var audio []part
	for range 6 {
		audio = append(audio, tone(4900*ms), part{100 * ms, -45})
	}
	cs := finalsOf(segmentAll(VADConfig{}, frames(synth(audio...), 0), true))
	if len(cs) < 3 {
		t.Fatalf("%d finals: %v", len(cs), spans(cs))
	}
	for i, c := range cs {
		if c.duration() > 12*time.Second {
			t.Errorf("final %d is %v long", i, c.duration())
		}
		if i > 0 && c.start != cs[i-1].end {
			t.Errorf("final %d starts at %v, the previous ended at %v", i, c.start, cs[i-1].end)
		}
	}
	// The first cut lands in the dip around 10 s, not at 12 s.
	if end := cs[0].end; end < 9800*ms || end > 10100*ms {
		t.Errorf("first cut at %v, want the dip at ~9.9 s", end)
	}
	if end := cs[len(cs)-1].end; !near(end, 30*time.Second) {
		t.Errorf("audio ends at %v", end)
	}
}

func TestSegmenterGapEndsUtterance(t *testing.T) {
	s := newSegmenter(VADConfig{})
	var out []chunk
	for _, f := range frames(synth(tone(1500*ms)), 0) {
		out = append(out, s.write(f)...)
	}
	// The session was paused: the next frame comes 10 s later.
	for _, f := range frames(synth(tone(1500*ms), silence(time.Second)), 11500*ms) {
		out = append(out, s.write(f)...)
	}
	fin := finalsOf(out)
	if len(fin) != 2 || !near(fin[0].end, 1500*ms) || !near(fin[1].start, 11500*ms) {
		t.Errorf("finals %v", spans(fin))
	}
}
