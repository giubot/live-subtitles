// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/genai"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// defaultTick is how often the stream checks idle segments, the end of the
// audio and pending usage.
const defaultTick = 250 * time.Millisecond

// usageEvery is how often usage is reported when no caption carries it.
const usageEvery = time.Second

// stream is one ASR stream: a goroutine that owns the current Live
// connection, feeds it audio, maps its messages to events and replaces it
// when the server sends GoAway or the connection drops.
//
//	in ─frames─▶ run ─audio─▶ conn ─messages─▶ reader ─▶ run ─events─▶ out
//	              └─(down) buffer, up to MaxBuffer; older audio is a logged gap
type stream struct {
	ctx  context.Context
	o    ASR
	log  *slog.Logger
	dc   dialConfig
	in   chan domain.AudioFrame
	out  chan domain.ASREvent
	done chan struct{} // closed when run returns; stops readers and dialers
	tick time.Duration

	seg segmenter

	conn    liveConn
	gen     int // generation of conn; messages of older connections are ignored
	msgs    chan connMsg
	dials   chan dialResult
	dialing bool
	retries int // failed dials in a row
	retry   *time.Timer

	batch    []byte // audio not yet sent to conn
	batchDur time.Duration
	clock    time.Duration // session clock at the end of the last frame
	started  bool          // a frame arrived

	// While there is no connection: the audio waiting for the next one,
	// and what didn't fit.
	buffered  []domain.AudioFrame
	bufDur    time.Duration
	downSince time.Time
	dropped   time.Duration
	gapFrom   time.Duration
	gapTo     time.Duration

	reply     string    // the model's reply in the current turn (the language tag)
	lastText  time.Time // wall time of the last transcription
	usage     domain.Usage
	usageAt   time.Time // wall time usage was last reported
	ended     bool      // the input channel closed
	endedAt   time.Time
	drainBy   time.Time
	finished  bool
	streamEnd bool // AudioStreamEnd was sent on conn
}

type connMsg struct {
	gen int
	msg *genai.LiveServerMessage
	err error
}

type dialResult struct {
	conn liveConn
	err  error
}

func newStream(ctx context.Context, o ASR, cfg domain.ASRConfig, dc dialConfig) *stream {
	now := o.now()
	return &stream{
		ctx: ctx, o: o, dc: dc,
		log:  o.Logger.With("session", cfg.SessionID, "provider", "gemini"),
		in:   make(chan domain.AudioFrame, 64),
		out:  make(chan domain.ASREvent, 64),
		done: make(chan struct{}),
		tick: o.tick,
		seg:  segmenter{pinned: dc.Language},
		msgs: make(chan connMsg, 16), dials: make(chan dialResult, 1),
		lastText: now, usageAt: now,
	}
}

func (s *stream) run(first liveConn) {
	defer close(s.out)
	defer close(s.done)
	defer func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
		if s.retry != nil {
			s.retry.Stop()
		}
	}()
	s.attach(first)
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	in := s.in
	for !s.finished {
		var retry <-chan time.Time
		if s.retry != nil {
			retry = s.retry.C
		}
		select {
		case f, ok := <-in:
			if !ok {
				in = nil
				s.endInput()
				continue
			}
			s.frame(f)
		case m := <-s.msgs:
			if m.gen != s.gen {
				continue // a connection we already replaced
			}
			if m.err != nil {
				s.lost(m.err)
			} else {
				s.handle(m.msg)
			}
		case d := <-s.dials:
			s.dialed(d)
		case <-retry:
			s.retry = nil
			s.startDial()
		case <-ticker.C:
			s.onTick()
		case <-s.ctx.Done():
			return
		}
	}
	s.flushUsage(true)
}

// attach makes c the current connection and sends it the buffered audio.
func (s *stream) attach(c liveConn) {
	s.conn = c
	s.gen++
	s.streamEnd = false
	go s.read(s.gen, c)
	if !s.downSince.IsZero() {
		attrs := []any{"down", s.o.now().Sub(s.downSince).Round(time.Millisecond), "resumed", s.dc.Handle != ""}
		if s.dropped > 0 {
			attrs = append(attrs, "gap_from", s.gapFrom, "gap_to", s.gapTo, "dropped", s.dropped)
			s.log.Warn("gemini live reconnected; audio was lost", attrs...)
		} else {
			s.log.Info("gemini live reconnected", attrs...)
		}
		s.downSince, s.dropped = time.Time{}, 0
	}
	pending := s.buffered
	s.buffered, s.bufDur = nil, 0
	for i, f := range pending {
		if s.conn == nil { // the send failed: keep the rest for the next connection
			for _, g := range pending[i:] {
				s.buffer(g)
			}
			return
		}
		s.queue(f)
		if s.batchDur >= time.Second {
			s.send()
		}
	}
	s.send()
	if s.conn != nil && s.ended {
		s.send()
		s.sendStreamEnd()
	}
}

func (s *stream) read(gen int, c liveConn) {
	for {
		m, err := c.Receive()
		select {
		case s.msgs <- connMsg{gen, m, err}:
		case <-s.done:
			return
		}
		if err != nil {
			return
		}
	}
}

// frame takes one frame of audio from the session.
func (s *stream) frame(f domain.AudioFrame) {
	if !s.started {
		// Segments can't start before the audio does.
		s.started = true
		s.seg.prevEnd = max(s.seg.prevEnd, f.T)
	}
	s.clock = f.End()
	if s.conn == nil {
		s.buffer(f)
		return
	}
	s.queue(f)
	if s.batchDur >= s.o.SendEvery {
		s.send()
	}
}

func (s *stream) queue(f domain.AudioFrame) {
	for _, v := range f.PCM {
		s.batch = binary.LittleEndian.AppendUint16(s.batch, uint16(v))
	}
	s.batchDur += f.End() - f.T
}

// send sends the batched audio.
func (s *stream) send() {
	if s.conn == nil || len(s.batch) == 0 {
		return
	}
	dur := s.batchDur
	if err := s.conn.SendAudio(s.batch); err != nil {
		s.lost(err)
		return
	}
	s.usage.AudioSeconds += dur.Seconds()
	s.batch, s.batchDur = nil, 0
}

// buffer keeps audio while there is no connection, dropping the oldest
// beyond MaxBuffer.
func (s *stream) buffer(f domain.AudioFrame) {
	s.buffered = append(s.buffered, f)
	s.bufDur += f.End() - f.T
	for s.bufDur > s.o.MaxBuffer && len(s.buffered) > 0 {
		old := s.buffered[0]
		s.buffered = s.buffered[1:]
		d := old.End() - old.T
		s.bufDur -= d
		if s.dropped == 0 {
			s.gapFrom = old.T
		}
		s.dropped += d
		s.gapTo = old.End()
	}
}

// handle maps one server message.
func (s *stream) handle(m *genai.LiveServerMessage) {
	if m == nil {
		return
	}
	if u := m.SessionResumptionUpdate; u != nil && u.Resumable && u.NewHandle != "" {
		s.dc.Handle = u.NewHandle
	}
	if u := m.UsageMetadata; u != nil {
		s.usage.InputTokens += int64(u.PromptTokenCount)
		s.usage.OutputTokens += int64(u.ResponseTokenCount) + int64(u.ThoughtsTokenCount)
	}
	if g := m.GoAway; g != nil {
		// The Live session is about to hit its limit: open the next one
		// (resuming this one) while this keeps working.
		s.log.Info("gemini live go-away; rotating the connection", "time_left", g.TimeLeft)
		s.startDial()
	}
	c := m.ServerContent
	if c == nil {
		return
	}
	if t := c.InputTranscription; t != nil {
		if t.Text != "" {
			s.lastText = s.o.now()
			for _, ev := range s.seg.add(t.Text, t.LanguageCode, s.clock) {
				s.emit(ev)
			}
		}
		if t.Finished {
			s.finishSegment()
		}
	}
	if c.ModelTurn != nil {
		for _, p := range c.ModelTurn.Parts {
			if p != nil && p.Text != "" && !p.Thought {
				s.reply += p.Text
			}
		}
	}
	if t := c.OutputTranscription; t != nil {
		s.reply += t.Text
	}
	if s.reply != "" {
		s.seg.setTag(s.reply)
	}
	if c.TurnComplete || c.Interrupted {
		s.finishSegment()
		s.reply = ""
		if s.ended && c.TurnComplete {
			s.finished = true
		}
	}
}

func (s *stream) finishSegment() {
	if ev, ok := s.seg.finish(); ok {
		s.emit(ev)
	}
}

// lost handles a connection that failed: finalize what was heard, report
// it, and reconnect.
func (s *stream) lost(err error) {
	if s.conn == nil {
		return
	}
	_ = s.conn.Close()
	s.conn = nil
	s.gen++
	s.finishSegment()
	if s.ended && s.streamEnd {
		s.finished = true // the server closed after the end of the audio
		return
	}
	s.log.Warn("gemini live connection lost; reconnecting", "err", err)
	s.emit(domain.ASREvent{Err: fmt.Errorf("gemini: live connection lost, reconnecting: %w", err)})
	s.downSince = s.o.now()
	if len(s.batch) > 0 {
		// Unsent audio waits for the next connection.
		pcm := make([]int16, len(s.batch)/2)
		for i := range pcm {
			pcm[i] = int16(binary.LittleEndian.Uint16(s.batch[2*i:]))
		}
		f := domain.AudioFrame{PCM: pcm, T: s.clock - s.batchDur}
		s.buffered = append([]domain.AudioFrame{f}, s.buffered...)
		s.bufDur += s.batchDur
		s.batch, s.batchDur = nil, 0
	}
	if s.ended && !s.dialing {
		s.finished = true
		return
	}
	s.startDial()
}

// startDial opens the next connection in the background, unless one is
// already being opened or a retry is scheduled.
func (s *stream) startDial() {
	if s.dialing || s.retry != nil {
		return
	}
	s.dialing = true
	dc := s.dc
	go func() {
		c, err := dialCtx(s.ctx, s.o.dial, dc)
		select {
		case s.dials <- dialResult{c, err}:
		case <-s.done:
			if c != nil {
				_ = c.Close()
			}
		}
	}()
}

func (s *stream) dialed(d dialResult) {
	s.dialing = false
	if d.err != nil {
		if s.ctx.Err() != nil {
			return
		}
		s.retries++
		s.log.Warn("gemini live connect failed", "attempt", s.retries, "resume", s.dc.Handle != "", "err", d.err)
		if s.dc.Handle != "" && s.retries >= 2 {
			// The handle may have expired: start a fresh Live session.
			s.dc.Handle = ""
		}
		if s.retries > s.o.MaxRetries {
			s.finishSegment()
			s.emit(domain.ASREvent{Err: fmt.Errorf("gemini: giving up after %d failed reconnects: %w", s.retries, d.err)})
			s.finished = true
			return
		}
		if s.conn == nil && s.ended {
			s.finished = true
			return
		}
		delay := s.o.Backoff << min(s.retries-1, 16)
		s.retry = time.NewTimer(min(delay, s.o.MaxBackoff))
		return
	}
	s.retries = 0
	if old := s.conn; old != nil {
		// Rotation after GoAway: finish what the old connection heard.
		s.send()
		_ = old.Close()
		s.finishSegment()
		s.log.Info("gemini live connection rotated", "resumed", s.dc.Handle != "")
	}
	s.attach(d.conn)
}

// endInput starts draining: the server gets AudioStreamEnd and the stream
// ends once the last transcription is in, or after DrainTimeout.
func (s *stream) endInput() {
	s.ended = true
	now := s.o.now()
	s.endedAt, s.drainBy = now, now.Add(s.o.DrainTimeout)
	if s.conn == nil {
		if !s.dialing && s.retry == nil {
			s.finishSegment()
			s.finished = true
		}
		return
	}
	s.send()
	s.sendStreamEnd()
}

func (s *stream) sendStreamEnd() {
	if s.conn == nil || s.streamEnd {
		return
	}
	if err := s.conn.SendAudioStreamEnd(); err != nil {
		s.lost(err)
		return
	}
	s.streamEnd = true
}

func (s *stream) onTick() {
	now := s.o.now()
	if s.seg.open() && now.Sub(s.lastText) >= s.o.IdleFinal {
		s.finishSegment()
	}
	if s.ended {
		quiet := now.Sub(s.lastText) >= s.o.IdleFinal && now.Sub(s.endedAt) >= s.o.IdleFinal
		if !now.Before(s.drainBy) || (s.conn != nil && !s.seg.open() && quiet) {
			s.finishSegment()
			s.finished = true
			return
		}
	}
	s.flushUsage(false)
}

// flushUsage reports pending usage on its own event every usageEvery, or
// now when force is set.
func (s *stream) flushUsage(force bool) {
	if s.usage == (domain.Usage{}) || (!force && s.o.now().Sub(s.usageAt) < usageEvery) {
		return
	}
	s.emit(domain.ASREvent{})
}

// emit sends ev with the usage accumulated since the last event.
func (s *stream) emit(ev domain.ASREvent) {
	ev.Usage = ev.Usage.Add(s.usage)
	s.usage = domain.Usage{}
	s.usageAt = s.o.now()
	select {
	case s.out <- ev:
	case <-s.ctx.Done():
	}
}
