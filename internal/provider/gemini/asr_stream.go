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

// defaultTick is how often the stream checks safety flushes, rotation, the
// end of the audio and pending usage.
const defaultTick = 250 * time.Millisecond

// usageEvery is how often usage is reported when no caption carries it.
const usageEvery = time.Second

// stream is one ASR stream: a goroutine that owns the Live connections,
// feeds the current one audio and maps their messages to events.
//
//	in ─frames─▶ run ─audio─▶ cur ─messages─▶ reader ─▶ run ─events─▶ out
//	              └─(no cur) buffer, up to MaxBuffer; older audio is a logged gap
//
// A transcription session streams for at most 10 minutes. RotateAfter into
// a connection, the stream opens the next one (next) and switches the audio
// to it at the first pause, when cur has no open utterance, or at
// RotateGrace regardless. The replaced connection (old) gets AudioStreamEnd
// and keeps delivering its last transcription for up to DrainTimeout. Every
// frame goes to exactly one connection, so nothing is lost or transcribed
// twice.
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

	cur  *conn // takes the audio
	next *conn // opened for a rotation, takes over at the next pause
	old  *conn // replaced by a rotation, finishing its last utterance

	gen      int // generation counter; messages of dropped connections are ignored
	msgs     chan connMsg
	dials    chan dialResult
	dialing  bool
	retries  int // failed dials in a row
	retry    *time.Timer
	rotateAt time.Time // when cur is due for rotation
	forceAt  time.Time // set while rotating: switch even mid-utterance at this time

	batch    []byte // audio not yet sent to cur
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

	lastText time.Time // wall time of the last transcription
	usage    domain.Usage
	usageAt  time.Time // wall time usage was last reported
	ended    bool      // the input channel closed
	endedAt  time.Time
	drainBy  time.Time
	finished bool
}

// conn is one Live connection and the utterance it is transcribing.
type conn struct {
	c         liveConn
	gen       int
	opened    time.Time
	u         utterance
	audioEnd  time.Duration // session clock at the end of the audio it got
	streamEnd bool          // AudioStreamEnd was sent
	flushed   bool          // the last utterance ended by a safety flush
	closeBy   time.Time     // while draining as old
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
		for _, c := range []*conn{s.cur, s.next, s.old} {
			if c != nil {
				_ = c.c.Close()
			}
		}
		if s.retry != nil {
			s.retry.Stop()
		}
	}()
	s.attach(s.newConn(first))
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
			c := s.byGen(m.gen)
			if c == nil {
				continue // a connection we already dropped
			}
			if m.err != nil {
				s.lost(c, m.err)
			} else {
				s.handle(c, m.msg)
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

func (s *stream) newConn(c liveConn) *conn {
	s.gen++
	cc := &conn{c: c, gen: s.gen, opened: s.o.now()}
	go s.read(cc.gen, c)
	return cc
}

func (s *stream) byGen(gen int) *conn {
	for _, c := range []*conn{s.cur, s.next, s.old} {
		if c != nil && c.gen == gen {
			return c
		}
	}
	return nil
}

// attach makes c the current connection and sends it the buffered audio.
func (s *stream) attach(c *conn) {
	s.cur = c
	s.rotateAt, s.forceAt = c.opened.Add(s.o.RotateAfter), time.Time{}
	if !s.downSince.IsZero() {
		attrs := []any{"down", s.o.now().Sub(s.downSince).Round(time.Millisecond)}
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
		if s.cur != c { // the send failed: keep the rest for the next connection
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
	if s.cur == c && s.ended {
		s.sendStreamEnd(c)
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
	if s.cur == nil {
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

// send sends the batched audio to the current connection.
func (s *stream) send() {
	c := s.cur
	if c == nil || len(s.batch) == 0 {
		return
	}
	dur := s.batchDur
	if err := c.c.SendAudio(s.batch); err != nil {
		s.lost(c, err)
		return
	}
	s.usage.AudioSeconds += dur.Seconds()
	s.batch, s.batchDur = nil, 0
	c.audioEnd = s.clock
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

// clockOf is the audio clock of c: where the audio it has heard ends.
func (s *stream) clockOf(c *conn) time.Duration {
	if c == s.cur {
		return s.clock
	}
	return c.audioEnd
}

// handle maps one server message of c.
func (s *stream) handle(c *conn, m *genai.LiveServerMessage) {
	if m == nil {
		return
	}
	if u := m.UsageMetadata; u != nil {
		// Audio is priced by the minute from AudioSeconds; only the text
		// the model wrote counts as tokens.
		s.usage.OutputTokens += int64(u.ResponseTokenCount) + int64(u.ThoughtsTokenCount)
	}
	if g := m.GoAway; g != nil && c == s.cur {
		s.log.Info("gemini live go-away; rotating the connection", "time_left", g.TimeLeft)
		s.rotate(s.o.now().Add(max(g.TimeLeft-time.Second, 0)))
	}
	if sc := m.ServerContent; sc != nil {
		s.content(c, sc)
	}
	s.maybeSwitch()
}

func (s *stream) content(c *conn, sc *genai.LiveServerContent) {
	now := s.o.now()
	if t := sc.InterimInputTranscription; t != nil {
		if ev, ok := s.seg.interim(&c.u, t.Text, t.LanguageCode, s.clockOf(c)); ok {
			s.lastText, c.flushed = now, false
			c.u.flushAt = now.Add(s.o.IdleFinal)
			s.emit(ev)
		}
	}
	if t := sc.InputTranscription; t != nil && (clean(t.Text) != "" || c.u.open()) {
		s.lastText = now
		if c.flushed && !c.u.open() {
			// The final of an utterance the safety flush already ended.
			s.log.Debug("gemini live final after a safety flush; dropped", "chars", len(t.Text))
		} else {
			for _, ev := range s.seg.final(&c.u, t.Text, t.LanguageCode, s.clockOf(c)) {
				s.emit(ev)
			}
		}
		c.flushed = false
	}
	if !sc.TurnComplete {
		return
	}
	switch {
	case c == s.old:
		s.retire(c)
	case c == s.cur && c.streamEnd:
		s.finish() // the server is done with the end of the audio
	case c.u.open():
		// The final usually comes with the end of the turn; if not, the
		// interim is finalized shortly.
		if by := now.Add(s.o.TurnGrace); by.Before(c.u.flushAt) {
			c.u.flushAt = by
		}
	}
}

// flush ends the open utterance of c with its interim text.
func (s *stream) flush(c *conn) {
	if !c.u.open() {
		return
	}
	for _, ev := range s.seg.final(&c.u, "", "", s.clockOf(c)) {
		s.emit(ev)
	}
	c.flushed = true
}

// finish ends the stream, finalizing what every connection heard.
func (s *stream) finish() {
	for _, c := range []*conn{s.cur, s.old} {
		if c != nil {
			s.flush(c)
		}
	}
	s.finished = true
}

// retire closes the replaced connection.
func (s *stream) retire(c *conn) {
	s.flush(c)
	_ = c.c.Close()
	if s.old == c {
		s.old = nil
	}
}

// rotate starts replacing cur: the next connection is opened now, and the
// switch happens at a pause or by forceBy.
func (s *stream) rotate(forceBy time.Time) {
	if s.ended || s.cur == nil {
		return
	}
	if s.forceAt.IsZero() || forceBy.Before(s.forceAt) {
		s.forceAt = forceBy
	}
	if s.next == nil {
		s.startDial()
	}
}

// maybeSwitch hands the audio to the next connection once cur is between
// utterances, or when the rotation can't wait any longer.
func (s *stream) maybeSwitch() {
	old, next := s.cur, s.next
	if old == nil || next == nil || (old.u.open() && s.o.now().Before(s.forceAt)) {
		return
	}
	s.send() // audio up to now belongs to the old connection
	if s.cur != old {
		return // the send failed and next already took over
	}
	s.next = nil
	if s.old != nil {
		s.retire(s.old)
	}
	old.closeBy = s.o.now().Add(s.o.DrainTimeout)
	s.old = old
	s.log.Info("gemini live connection rotated", "after", s.o.now().Sub(old.opened).Round(time.Second),
		"mid_utterance", old.u.open())
	s.attach(next)
	s.sendStreamEnd(old)
}

// lost handles a connection that failed.
func (s *stream) lost(c *conn, err error) {
	_ = c.c.Close()
	switch c {
	case s.old:
		s.retire(c)
		return
	case s.next:
		s.next = nil
		s.log.Warn("gemini live rotation connection lost", "err", err)
		return // onTick opens another one
	case s.cur:
	default:
		return
	}
	s.cur = nil
	s.flush(c)
	if s.ended && c.streamEnd {
		s.finish() // the server closed after the end of the audio
		return
	}
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
	if next := s.next; next != nil {
		// A rotation was under way: its connection takes over at once.
		s.next = nil
		s.log.Warn("gemini live connection lost; switched to the next one", "err", err)
		s.attach(next)
		return
	}
	s.log.Warn("gemini live connection lost; reconnecting", "err", err)
	s.emit(domain.ASREvent{Err: fmt.Errorf("gemini: live connection lost, reconnecting: %w", err)})
	s.downSince = s.o.now()
	s.retries = 0 // failed rotation dials don't count against reconnecting
	if s.ended && !s.dialing {
		s.finish()
		return
	}
	s.startDial()
}

// startDial opens a connection in the background, unless one is already
// being opened or a retry is scheduled.
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
		s.log.Warn("gemini live connect failed", "attempt", s.retries, "rotation", s.cur != nil, "err", d.err)
		if s.cur == nil {
			if s.retries > s.o.MaxRetries {
				s.emit(domain.ASREvent{Err: fmt.Errorf("gemini: giving up after %d failed reconnects: %w", s.retries, d.err)})
				s.finish()
				return
			}
			if s.ended {
				s.finish()
				return
			}
		} else if s.ended {
			return // no rotation needed any more
		}
		delay := s.o.Backoff << min(s.retries-1, 16)
		s.retry = time.NewTimer(min(delay, s.o.MaxBackoff))
		return
	}
	s.retries = 0
	if s.cur == nil {
		s.attach(s.newConn(d.conn))
		return
	}
	if s.ended || s.next != nil {
		_ = d.conn.Close()
		return
	}
	s.next = s.newConn(d.conn)
	s.maybeSwitch()
}

// endInput starts draining: the server gets AudioStreamEnd and the stream
// ends once the last transcription is in, or after DrainTimeout.
func (s *stream) endInput() {
	s.ended = true
	now := s.o.now()
	s.endedAt, s.drainBy = now, now.Add(s.o.DrainTimeout)
	if s.next != nil {
		_ = s.next.c.Close()
		s.next = nil
	}
	s.forceAt = time.Time{}
	if s.cur == nil {
		if !s.dialing && s.retry == nil {
			s.finish()
		}
		return
	}
	s.send()
	if s.cur != nil {
		s.sendStreamEnd(s.cur)
	}
}

func (s *stream) sendStreamEnd(c *conn) {
	if c.streamEnd {
		return
	}
	if err := c.c.SendAudioStreamEnd(); err != nil {
		s.lost(c, err)
		return
	}
	c.streamEnd = true
}

func (s *stream) onTick() {
	now := s.o.now()
	if s.cur != nil && !s.ended {
		if s.forceAt.IsZero() && !now.Before(s.rotateAt) {
			s.log.Info("gemini live connection near its time limit; rotating", "age", now.Sub(s.cur.opened).Round(time.Second))
			s.rotate(now.Add(s.o.RotateGrace))
		} else if !s.forceAt.IsZero() && s.next == nil {
			s.startDial() // the rotation connection failed or dropped
		}
		s.maybeSwitch()
	}
	for _, c := range []*conn{s.cur, s.old} {
		if c != nil && c.u.open() && !now.Before(c.u.flushAt) {
			s.flush(c)
		}
	}
	if s.old != nil && !now.Before(s.old.closeBy) {
		s.retire(s.old)
	}
	if s.ended {
		quiet := now.Sub(s.lastText) >= s.o.IdleFinal && now.Sub(s.endedAt) >= s.o.IdleFinal
		if !now.Before(s.drainBy) || (s.cur != nil && !s.cur.u.open() && s.old == nil && quiet) {
			s.finish()
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
