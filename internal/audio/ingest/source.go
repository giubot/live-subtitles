// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// frameBytes is the size of one 20 ms s16le frame.
const frameBytes = domain.FrameSamples * 2

// ErrAlreadyStarted is returned by Start while a previous Start is still
// being consumed.
var ErrAlreadyStarted = errors.New("ingest: source already started")

// ErrClosed is returned by Start after the source was removed from its hub.
var ErrClosed = errors.New("ingest: source closed")

// Source is the domain.AudioSource of one session. It outlives ingest
// connections: clients connect, disconnect and replace each other, and the
// consumer sees one continuous frame stream.
//
// # Session clock
//
// Frame T values count the audio received since the source started, in 20 ms
// steps. When a client reconnects, the wall time between the last audio of the
// old connection and the first audio of the new one is the gap: T jumps
// forward by it (rounded to whole frames), so T keeps tracking wall time and
// stays monotonic. Consumers that need gapless audio (the recorder) see
// f.T > previous End and pad with silence. The gap is reported as
// AudioStatus.lastGapMs.
//
// Within one connection, a stall (audio arriving more than StallThreshold
// behind real time) is counted as a frame gap; T is not shifted for it
// because a client that stalls usually catches up with a burst.
//
// # Jitter buffer
//
// Network reads are decoupled from the consumer by a bounded FIFO of frames
// (Options.BufferFrames, 1 s by default). Frames are handed out as soon as they
// are complete: the ASR doesn't need isochronous delivery, so the buffer adds
// no playout delay; it only absorbs bursts and a briefly slow consumer. When it
// is full the oldest frame is dropped and counted, so a stalled pipeline never
// blocks the socket and resumes near live.
type Source struct {
	id    string
	opts  *Options
	clock domain.Clock

	mu    sync.Mutex
	conn  *conn // active connection, nil when disconnected
	kind  api.AudioSourceKind
	meter *audio.LevelMeter
	level audio.Level

	// Timeline.
	nextT     time.Duration // T of the next frame
	lastAudio time.Time     // wall time of the last received audio
	hadAudio  bool
	lastGap   time.Duration
	gapKnown  bool
	frameGaps int

	rem []byte // bytes of an incomplete frame from the active connection

	// Jitter buffer.
	buf     []domain.AudioFrame
	dropped int
	frames  int
	notify  chan struct{}
	running bool

	closed bool
	done   chan struct{}
}

var _ domain.AudioSource = (*Source)(nil)

func newSource(id string, opts *Options) *Source {
	return &Source{
		id:     id,
		opts:   opts,
		clock:  opts.Clock,
		kind:   api.AudioSourceKindBrowser,
		meter:  audio.NewLevelMeter(opts.Level),
		level:  audio.Level{RMSDBFS: audio.FloorDBFS, PeakDBFS: audio.FloorDBFS},
		notify: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
}

// ID is the session id.
func (s *Source) ID() string { return s.id }

// Kind reports the source announced by the connected client (browser by default).
func (s *Source) Kind() api.AudioSourceKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kind
}

// Err is always nil: the source only ends when its context is cancelled or
// it is removed from the hub.
func (s *Source) Err() error { return nil }

// Start returns the frame stream. Frames received before Start (up to the
// jitter buffer size) are delivered first. Only one consumer may run at a
// time; after its context is cancelled Start may be called again.
func (s *Source) Start(ctx context.Context) (<-chan domain.AudioFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if s.running {
		return nil, ErrAlreadyStarted
	}
	s.running = true
	out := make(chan domain.AudioFrame)
	go s.pump(ctx, out)
	return out, nil
}

func (s *Source) pump(ctx context.Context, out chan<- domain.AudioFrame) {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		close(out)
	}()
	for {
		s.mu.Lock()
		if len(s.buf) > 0 {
			f := s.buf[0]
			s.buf = s.buf[1:]
			s.mu.Unlock()
			select {
			case out <- f:
			case <-ctx.Done():
				return
			}
			continue
		}
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		select {
		case <-s.notify:
		case <-s.done:
		case <-ctx.Done():
			return
		}
	}
}

// Status is the AudioStatus snapshot for the dashboard and session status.
func (s *Source) Status() api.AudioStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	kind := s.kind
	rms, peak := float32(s.level.RMSDBFS), float32(s.level.PeakDBFS)
	silent, clipping := s.level.Silent, s.level.Clipping
	st := api.AudioStatus{
		Connected: s.conn != nil,
		Source:    &kind,
		LevelDbfs: &rms,
		PeakDbfs:  &peak,
		Silent:    &silent,
		Clipping:  &clipping,
	}
	if s.gapKnown {
		ms := int(s.lastGap.Milliseconds())
		st.LastGapMs = &ms
	}
	return st
}

// Stats are counters for diagnostics and tests.
type Stats struct {
	Connected bool
	// Frames is the number of frames produced; Dropped the ones the jitter
	// buffer discarded because the consumer was behind.
	Frames, Dropped int
	// FrameGaps counts stalls within a connection.
	FrameGaps int
	// LastGap is the last reconnect gap (0 if none yet).
	LastGap time.Duration
	// NextT is the T the next frame will get.
	NextT time.Duration
	Level audio.Level
}

// Stats returns the current counters.
func (s *Source) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		Connected: s.conn != nil,
		Frames:    s.frames,
		Dropped:   s.dropped,
		FrameGaps: s.frameGaps,
		LastGap:   s.lastGap,
		NextT:     s.nextT,
		Level:     s.level,
	}
}

// Level returns the latest level reading.
func (s *Source) Level() audio.Level {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.level
}

// attach makes c the active connection and returns the one it replaces.
func (s *Source) attach(c *conn, kind api.AudioSourceKind) (old *conn, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	old, s.conn = s.conn, c
	s.kind = kind
	s.rem = s.rem[:0] // a partial frame from the old connection can't be completed
	return old, nil
}

// detach clears c if it is still the active connection.
func (s *Source) detach(c *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == c {
		s.conn = nil
		s.rem = s.rem[:0]
	}
}

// write ingests PCM bytes received on c at wall time now.
func (s *Source) write(c *conn, data []byte, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != c || s.closed || len(data) == 0 {
		return
	}
	dur := time.Duration(len(data)/2) * time.Second / domain.SampleRate
	if !c.started {
		c.started = true
		c.start = now.Add(-dur)
		if s.hadAudio {
			gap := max(0, c.start.Sub(s.lastAudio)).Round(domain.FrameDuration)
			s.nextT += gap
			s.lastGap, s.gapKnown = gap, true
		}
	}
	c.received += dur
	if lag := now.Sub(c.start) - c.received; lag > s.opts.StallThreshold {
		s.frameGaps++
		c.start = now.Add(-c.received) // re-anchor so one stall counts once
	}
	s.hadAudio = true
	s.lastAudio = now

	s.rem = append(s.rem, data...)
	n := 0
	for ; len(s.rem)-n >= frameBytes; n += frameBytes {
		pcm := make([]int16, domain.FrameSamples)
		for i := range pcm {
			pcm[i] = int16(binary.LittleEndian.Uint16(s.rem[n+2*i:]))
		}
		s.meter.Write(pcm)
		s.push(domain.AudioFrame{PCM: pcm, T: s.nextT})
		s.nextT += domain.FrameDuration
	}
	s.rem = s.rem[:copy(s.rem, s.rem[n:])]
	s.level = s.meter.Level()
}

// push appends to the jitter buffer, dropping the oldest frame when full.
// Called with mu held.
func (s *Source) push(f domain.AudioFrame) {
	if len(s.buf) >= s.opts.BufferFrames {
		s.buf = s.buf[1:]
		s.dropped++
	}
	s.buf = append(s.buf, f)
	s.frames++
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// close ends the frame stream and kicks the active connection.
func (s *Source) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.done)
}
