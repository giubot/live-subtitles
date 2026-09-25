// SPDX-License-Identifier: Apache-2.0

package whisper

import (
	"time"

	"github.com/iencodev/live-subtitles/internal/audio"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// VADConfig tunes the utterance segmentation. Zero fields take the defaults.
type VADConfig struct {
	// Window is the energy measurement window (default 20 ms).
	Window time.Duration
	// MinSpeechDBFS is the RMS level below which a window is never speech
	// (default -55 dBFS).
	MinSpeechDBFS float64
	// MarginDB is how far above the tracked noise floor a window has to be
	// to count as speech (default 12 dB).
	MarginDB float64
	// PreRoll is the audio kept before the first speech window, so the
	// start of the first word isn't clipped (default 300 ms).
	PreRoll time.Duration
	// Pause is the silence that commits an utterance (default 600 ms).
	Pause time.Duration
	// Tail is the silence kept after the last speech window (default 200 ms).
	Tail time.Duration
	// MinSpeech is the voiced audio an utterance needs to be transcribed
	// at all (default 300 ms).
	MinSpeech time.Duration
	// MaxUtterance commits an utterance that doesn't pause (default 12 s).
	MaxUtterance time.Duration
	// InterimEvery is the new audio between interim transcriptions of the
	// utterance in progress (default 1 s).
	InterimEvery time.Duration
}

func (c VADConfig) withDefaults() VADConfig {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&c.Window, 20*time.Millisecond)
	def(&c.PreRoll, 300*time.Millisecond)
	def(&c.Pause, 600*time.Millisecond)
	def(&c.Tail, 200*time.Millisecond)
	def(&c.MinSpeech, 300*time.Millisecond)
	def(&c.MaxUtterance, 12*time.Second)
	def(&c.InterimEvery, time.Second)
	if c.MinSpeechDBFS == 0 {
		c.MinSpeechDBFS = -55
	}
	if c.MarginDB == 0 {
		c.MarginDB = 12
	}
	return c
}

// chunk is audio to transcribe: the utterance so far (interim) or all of it
// (final). Start and End are session clock offsets.
type chunk struct {
	utterance  int // 1-based, one per utterance
	pcm        []int16
	start, end time.Duration
	final      bool
}

func (c chunk) duration() time.Duration { return c.end - c.start }

// Noise floor tracking: it follows a quieter window at once and rises
// towards a louder one slowly (a time constant of ~7 s at 20 ms windows),
// so steady hum stops counting as speech while pauses keep it low.
const (
	initialFloorDBFS = -70.0
	floorRise        = 0.003
	// cutSearch is how far back from MaxUtterance the quietest window is
	// looked for when an utterance has to be cut without a pause.
	cutSearch = 3 * time.Second
	// gapTolerance is the jump in frame times read as a gap in the audio
	// (a pause of the session), which ends the utterance.
	gapTolerance = 100 * time.Millisecond
)

// segmenter cuts audio into utterances by energy. It measures time in
// samples, so its output is deterministic for a given input, and is not
// safe for concurrent use.
type segmenter struct {
	cfg VADConfig
	// Sizes in samples.
	window, preRoll, pause, tail, minSpeech, maxUtt, interimEvery, cutSearch int

	floor float64

	// buf holds the utterance in progress, or the pre-roll between
	// utterances. bufStart is the session clock of buf[0].
	buf      []int16
	bufStart time.Duration
	levels   []float64 // dBFS per complete window of buf
	pending  int       // samples of buf not yet in a complete window
	next     time.Duration
	started  bool

	inUtt       bool
	utterance   int
	voiced      int // speech samples in the utterance
	silence     int // samples since the last speech window
	voicedEnd   int // end of the last speech window in buf
	lastInterim int // len(buf) at the last interim
}

func newSegmenter(cfg VADConfig) *segmenter {
	cfg = cfg.withDefaults()
	n := func(d time.Duration) int { return max(1, int(d*domain.SampleRate/time.Second)) }
	return &segmenter{
		cfg: cfg, window: n(cfg.Window), preRoll: n(cfg.PreRoll), pause: n(cfg.Pause), tail: n(cfg.Tail),
		minSpeech: n(cfg.MinSpeech), maxUtt: n(cfg.MaxUtterance), interimEvery: n(cfg.InterimEvery),
		cutSearch: n(cutSearch), floor: initialFloorDBFS,
	}
}

func samplesDur(n int) time.Duration { return time.Duration(n) * time.Second / domain.SampleRate }

// write feeds one frame and returns the chunks it completes.
func (s *segmenter) write(f domain.AudioFrame) []chunk {
	var out []chunk
	if !s.started {
		s.bufStart, s.next, s.started = f.T, f.T, true
	}
	if d := f.T - s.next; d > gapTolerance || d < -gapTolerance {
		// A gap (the session was paused) or a clock jump: the audio on
		// either side isn't one utterance.
		if c, ok := s.flush(); ok {
			out = append(out, c)
		}
		s.buf, s.levels, s.pending = s.buf[:0], s.levels[:0], 0
		s.bufStart = f.T
	}
	s.next = f.End()
	pcm := f.PCM
	for len(pcm) > 0 {
		take := min(len(pcm), s.window-s.pending)
		s.buf = append(s.buf, pcm[:take]...)
		s.pending += take
		pcm = pcm[take:]
		if s.pending == s.window {
			s.pending = 0
			if cs, ok := s.closeWindow(); ok {
				out = append(out, cs...)
			}
		}
	}
	// Drop utterances with too little voiced audio (commit empties them).
	kept := out[:0]
	for _, c := range out {
		if len(c.pcm) > 0 {
			kept = append(kept, c)
		}
	}
	return kept
}

// closeWindow classifies the window that just filled up at the end of buf.
func (s *segmenter) closeWindow() ([]chunk, bool) {
	level := audio.RMSDBFS(s.buf[len(s.buf)-s.window:])
	s.levels = append(s.levels, level)
	speech := level >= max(s.cfg.MinSpeechDBFS, s.floor+s.cfg.MarginDB)
	if level < s.floor {
		s.floor = level
	} else {
		s.floor += (level - s.floor) * floorRise
	}

	if !s.inUtt {
		if speech {
			s.inUtt = true
			s.utterance++
			s.voiced, s.silence, s.voicedEnd, s.lastInterim = s.window, 0, len(s.buf), 0
			return nil, false
		}
		s.keepPreRoll()
		return nil, false
	}

	if speech {
		s.voiced += s.window
		s.silence = 0
		s.voicedEnd = len(s.buf)
	} else {
		s.silence += s.window
	}
	switch {
	case s.silence >= s.pause:
		c := s.commit(min(len(s.buf), s.voicedEnd+s.tail))
		s.inUtt = false
		s.keepPreRoll()
		return []chunk{c}, true
	case len(s.buf) >= s.maxUtt:
		return []chunk{s.cut()}, true
	case speech && s.voiced >= s.minSpeech && len(s.buf)-s.lastInterim >= s.interimEvery:
		// Only on speech: re-reading a pause adds nothing.
		s.lastInterim = len(s.buf)
		return []chunk{s.chunk(len(s.buf), false)}, true
	}
	return nil, false
}

// cut commits an utterance at MaxUtterance at its quietest window of the
// last seconds and carries the rest over as the start of the next one.
func (s *segmenter) cut() chunk {
	first := max(1, len(s.levels)-s.cutSearch/s.window)
	last := len(s.levels) - max(1, domain.SampleRate/2/s.window) // not in the last 0.5 s
	best := len(s.levels) - 1
	if last > first {
		best = first
		for i := first; i < last; i++ {
			if s.levels[i] < s.levels[best] {
				best = i
			}
		}
	}
	at := best * s.window
	c := s.commit(at)
	// The rest starts the next utterance.
	s.utterance++
	s.voiced, s.voicedEnd = 0, 0
	for i, l := range s.levels {
		if l >= max(s.cfg.MinSpeechDBFS, s.floor+s.cfg.MarginDB) {
			s.voiced += s.window
			s.voicedEnd = (i + 1) * s.window
		}
	}
	s.lastInterim = 0
	return c
}

// commit returns buf[:end] as the utterance's final chunk (or an empty,
// unvoiced one the caller drops) and removes it from buf.
func (s *segmenter) commit(end int) chunk {
	c := s.chunk(end, true)
	if s.voiced < s.minSpeech {
		c.pcm = nil
	}
	s.drop(end)
	return c
}

func (s *segmenter) chunk(end int, final bool) chunk {
	return chunk{
		utterance: s.utterance,
		pcm:       append([]int16(nil), s.buf[:end]...),
		start:     s.bufStart,
		end:       s.bufStart + samplesDur(end),
		final:     final,
	}
}

// drop removes the first n samples of buf (n is a multiple of the window
// unless it covers all of buf's complete windows).
func (s *segmenter) drop(n int) {
	n = min(n, len(s.buf))
	s.buf = append(s.buf[:0], s.buf[n:]...)
	s.bufStart += samplesDur(n)
	w := min(len(s.levels), n/s.window)
	if n%s.window != 0 && w < len(s.levels) {
		// A partial window was cut: its level is stale but close enough.
		w = min(len(s.levels), w+1)
	}
	s.levels = append(s.levels[:0], s.levels[w:]...)
}

// keepPreRoll trims the silence between utterances to PreRoll.
func (s *segmenter) keepPreRoll() {
	if extra := len(s.buf) - s.pending - s.preRoll; extra > 0 {
		extra -= extra % s.window
		s.drop(extra)
	}
}

// flush ends the stream: an utterance in progress becomes final. It
// reports false when there is none or it had too little voiced audio.
func (s *segmenter) flush() (chunk, bool) {
	if !s.inUtt {
		return chunk{}, false
	}
	s.inUtt = false
	end := len(s.buf)
	if s.silence > s.tail {
		end = min(end, s.voicedEnd+s.tail)
	}
	c := s.commit(end)
	s.buf, s.levels, s.pending = s.buf[:0], s.levels[:0], 0
	return c, len(c.pcm) > 0
}
