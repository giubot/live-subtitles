// SPDX-License-Identifier: Apache-2.0

package bus

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Defaults for the Bus options.
const (
	// DefaultHistorySize is the number of final captions kept per track; it
	// matches the maximum `history` a /ws/captions client may ask for.
	DefaultHistorySize = 500
	// DefaultHistory is how many final captions per track a subscriber gets
	// on connect when it doesn't ask for a number.
	DefaultHistory = 50
	// DefaultSubscriberBuffer is how many messages a subscriber may lag
	// behind before it is dropped.
	DefaultSubscriberBuffer = 256
	// DefaultViewersInterval is the minimum time between two `viewers`
	// messages of one session.
	DefaultViewersInterval = time.Second
)

// Option configures a Bus.
type Option func(*Bus)

// WithHistorySize sets the number of final captions kept per track.
func WithHistorySize(n int) Option {
	return func(b *Bus) {
		if n > 0 {
			b.historySize = n
		}
	}
}

// WithSubscriberBuffer sets the per-subscriber channel capacity. A
// subscriber whose channel is full when a message is published is dropped.
func WithSubscriberBuffer(n int) Option {
	return func(b *Bus) {
		if n > 0 {
			b.bufferSize = n
		}
	}
}

// WithViewersInterval sets the minimum time between two `viewers` messages
// of one session; changes in between are coalesced into one message.
func WithViewersInterval(d time.Duration) Option {
	return func(b *Bus) {
		if d > 0 {
			b.viewersInterval = d
		}
	}
}

// Bus is an in-process domain.CaptionBus keyed by (session, track).
//
// History semantics, per track:
//   - Final captions go into a ring buffer of the last HistorySize finals. A
//     final whose segmentId is already in the ring (a correction or an admin
//     edit) replaces that entry in place and is delivered again. Once an
//     entry is edited (ADM-4), only another edited final replaces it: a
//     provider's late re-final is dropped so it can't undo the correction.
//   - The track keeps at most one interim caption: the latest one. A newer
//     interim replaces it whatever its segment; the final of its segment
//     clears it. An interim for a segment that is already final in the ring
//     is late and is dropped (neither stored nor delivered), so a final is
//     never overwritten by an interim.
//   - The `history` message a subscriber gets first holds the last n finals
//     of each requested track merged in start-time order, without the ones
//     an admin hid, followed by the current interim of each track
//     (final=false), so a viewer joining mid-sentence sees the sentence in
//     progress. Hidden finals stay in the ring and still count towards n.
//
// Delivery never blocks the publisher: each subscriber has a bounded
// channel, and a subscriber whose channel is full is dropped (its channel
// is closed) instead.
//
// Every subscriber counts as a viewer. A `viewers` message goes to the whole
// session when the count changes, at most once per viewers interval
// (leading edge plus one trailing message with the final count).
type Bus struct {
	historySize     int
	bufferSize      int
	viewersInterval time.Duration

	mu       sync.Mutex
	sessions map[string]*session
}

var _ domain.CaptionBus = (*Bus)(nil)

// New returns an empty Bus.
func New(opts ...Option) *Bus {
	b := &Bus{
		historySize:     DefaultHistorySize,
		bufferSize:      DefaultSubscriberBuffer,
		viewersInterval: DefaultViewersInterval,
		sessions:        map[string]*session{},
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

type session struct {
	mu     sync.Mutex
	closed bool // removed by CloseSession
	tracks map[string]*track
	subs   map[*subscriber]struct{}

	// Viewer broadcast throttling.
	sentViewers  int // count in the last `viewers` message
	lastViewers  time.Time
	viewersTimer *time.Timer
}

type subscriber struct {
	ch     chan domain.BusMessage
	tracks map[string]bool
	stop   func() bool // unregisters the context.AfterFunc
}

type track struct {
	ring  []domain.CaptionEvent // grows to the history size, then wraps
	next  int                   // slot of the next write once full
	seq   uint64                // number of finals ever pushed
	final map[string]uint64     // segmentId → seq of its entry in ring
	// interim is the current interim caption, if any.
	interim *domain.CaptionEvent
}

// lockSession returns the session state, created if needed, with its
// mutex held.
func (b *Bus) lockSession(id string) *session {
	for {
		b.mu.Lock()
		s, ok := b.sessions[id]
		if !ok {
			s = &session{tracks: map[string]*track{}, subs: map[*subscriber]struct{}{}}
			b.sessions[id] = s
		}
		b.mu.Unlock()
		s.mu.Lock()
		if !s.closed {
			return s
		}
		s.mu.Unlock() // lost a race with CloseSession; take the new one
	}
}

// Publish implements domain.CaptionBus. Caption messages update the
// track's history and go to the track's subscribers; other messages go to
// every subscriber of the session. A `caption` message without a caption,
// or a late interim for a finalized segment, is dropped.
func (b *Bus) Publish(sessionID string, msg domain.BusMessage) {
	s := b.lockSession(sessionID)
	defer s.mu.Unlock()
	if msg.Type != api.CaptionsServerMessageTypeCaption {
		b.broadcast(s, msg, "")
		return
	}
	if msg.Caption == nil {
		return
	}
	c := *msg.Caption
	t := s.tracks[c.Lang]
	if t == nil {
		t = &track{ring: make([]domain.CaptionEvent, 0, min(b.historySize, 64)), final: map[string]uint64{}}
		s.tracks[c.Lang] = t
	}
	if !t.add(c, b.historySize) {
		return
	}
	msg.Caption = &c // subscribers share this copy, not the publisher's
	b.broadcast(s, msg, c.Lang)
}

// add records c in the history; it reports false if c must be dropped.
func (t *track) add(c domain.CaptionEvent, size int) bool {
	if !c.Final {
		if _, done := t.final[c.SegmentId]; done {
			return false
		}
		t.interim = &c
		return true
	}
	if t.interim != nil && t.interim.SegmentId == c.SegmentId {
		t.interim = nil
	}
	if seq, ok := t.final[c.SegmentId]; ok {
		slot := &t.ring[seq%uint64(size)] // a final's slot is its seq modulo size
		if isTrue(slot.Edited) && !isTrue(c.Edited) {
			return false
		}
		*slot = c
		return true
	}
	if len(t.ring) < size {
		t.ring = append(t.ring, c)
	} else {
		old := t.ring[t.next]
		if t.final[old.SegmentId] == t.seq-uint64(size) {
			delete(t.final, old.SegmentId)
		}
		t.ring[t.next] = c
		t.next = (t.next + 1) % size
	}
	t.final[c.SegmentId] = t.seq
	t.seq++
	return true
}

// last returns up to n most recent finals, oldest first.
func (t *track) last(n int) []domain.CaptionEvent {
	n = min(n, len(t.ring))
	out := make([]domain.CaptionEvent, 0, n)
	// The oldest entry is at t.next (0 until the ring is full).
	start := len(t.ring) - n
	for i := range n {
		out = append(out, t.ring[(t.next+start+i)%len(t.ring)])
	}
	return out
}

func isTrue(b *bool) bool { return b != nil && *b }

// broadcast delivers msg to the subscribers of track, or to every
// subscriber when track is empty, dropping those that can't keep up.
// Called with s.mu held.
func (b *Bus) broadcast(s *session, msg domain.BusMessage, track string) {
	dropped := false
	for sub := range s.subs {
		if track != "" && !sub.tracks[track] {
			continue
		}
		select {
		case sub.ch <- msg:
		default:
			s.remove(sub)
			dropped = true
		}
	}
	if dropped {
		b.viewersChanged(s)
	}
}

// remove unregisters sub and closes its channel. Called with s.mu held.
func (s *session) remove(sub *subscriber) {
	if _, ok := s.subs[sub]; !ok {
		return
	}
	delete(s.subs, sub)
	sub.stop()
	close(sub.ch)
}

// Subscribe implements domain.CaptionBus with DefaultHistory finals per
// track in the history message. Messages are shared between subscribers
// and must not be modified.
func (b *Bus) Subscribe(ctx context.Context, sessionID string, tracks []string) <-chan domain.BusMessage {
	return b.SubscribeHistory(ctx, sessionID, tracks, DefaultHistory)
}

// SubscribeHistory is Subscribe with the number of recent finals per track
// to put in the first (`history`) message; n <= 0 sends only the current
// interims.
func (b *Bus) SubscribeHistory(ctx context.Context, sessionID string, tracks []string, n int) <-chan domain.BusMessage {
	sub := &subscriber{ch: make(chan domain.BusMessage, b.bufferSize), tracks: map[string]bool{}}
	for _, t := range tracks {
		sub.tracks[t] = true
	}
	s := b.lockSession(sessionID)
	defer s.mu.Unlock()

	sub.ch <- s.history(tracks, n)
	if ctx.Err() != nil {
		close(sub.ch)
		return sub.ch
	}
	s.subs[sub] = struct{}{}
	sub.stop = context.AfterFunc(ctx, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subs[sub]; ok {
			s.remove(sub)
			b.viewersChanged(s)
		}
	})
	b.viewersChanged(s)
	return sub.ch
}

// history builds the first message of a subscription. Called with s.mu held.
func (s *session) history(tracks []string, n int) domain.BusMessage {
	var finals, interims []domain.CaptionEvent
	seen := map[string]bool{}
	for _, name := range tracks {
		t := s.tracks[name]
		if t == nil || seen[name] {
			continue
		}
		seen[name] = true
		if n > 0 {
			for _, c := range t.last(n) {
				if !isTrue(c.Hidden) {
					finals = append(finals, c)
				}
			}
		}
		if t.interim != nil {
			interims = append(interims, *t.interim)
		}
	}
	slices.SortStableFunc(finals, func(a, b domain.CaptionEvent) int { return cmp.Compare(a.Start, b.Start) })
	all := append(finals, interims...)
	if all == nil {
		all = []domain.CaptionEvent{}
	}
	return domain.BusMessage{Type: api.CaptionsServerMessageTypeHistory, Captions: &all}
}

// Viewers implements domain.CaptionBus: the number of live subscribers of
// the session.
func (b *Bus) Viewers(sessionID string) int {
	b.mu.Lock()
	s := b.sessions[sessionID]
	b.mu.Unlock()
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

// viewersChanged publishes a `viewers` message now if none was sent within
// the throttle interval, else schedules one for the end of the interval.
// Called with s.mu held.
func (b *Bus) viewersChanged(s *session) {
	if s.viewersTimer != nil || s.closed {
		return // a trailing message is already scheduled
	}
	wait := b.viewersInterval - time.Since(s.lastViewers)
	if wait <= 0 {
		b.sendViewers(s)
		return
	}
	s.viewersTimer = time.AfterFunc(wait, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.viewersTimer = nil
		b.sendViewers(s)
	})
}

// sendViewers broadcasts the viewer count if it changed since the last
// `viewers` message. Called with s.mu held.
func (b *Bus) sendViewers(s *session) {
	n := len(s.subs)
	if n == s.sentViewers || s.closed {
		return
	}
	s.sentViewers = n
	s.lastViewers = time.Now()
	// Drops caused by this broadcast change the count again; broadcast then
	// schedules a trailing message, since lastViewers is now.
	b.broadcast(s, domain.BusMessage{Type: api.CaptionsServerMessageTypeViewers, Viewers: &n}, "")
}

// CloseSession closes every subscription of the session and forgets its
// history. Call it when a session is deleted.
func (b *Bus) CloseSession(sessionID string) {
	b.mu.Lock()
	s := b.sessions[sessionID]
	delete(b.sessions, sessionID)
	b.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for sub := range s.subs {
		s.remove(sub)
	}
	if s.viewersTimer != nil {
		s.viewersTimer.Stop()
		s.viewersTimer = nil
	}
}
