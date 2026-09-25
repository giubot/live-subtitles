// SPDX-License-Identifier: Apache-2.0

package streamcc

import (
	"context"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
)

// seenSegments bounds the segment ids a sink remembers to skip a final
// delivered again (a correction or an edit): the stream already showed it.
const seenSegments = 256

// sink delivers one run's final captions of one track to the session's
// target, in order, from a bounded queue.
type sink struct {
	s        *Service
	id       string
	st       *sessionState
	target   api.StreamCaptionTarget
	track    string
	maxChars int

	ctx    context.Context // cancelled to abort, e.g. at the end of the drain
	cancel context.CancelFunc
	wake   chan struct{} // capacity 1: something was queued or changed

	mu      sync.Mutex
	url     string
	queue   []item
	closing bool
	seen    map[string]bool
	order   []string // seen, oldest first
	dropped int      // stale or overflowed items not yet reported
}

func newSink(s *Service, id string, st *sessionState, target api.StreamCaptionTarget, track string, maxChars int) *sink {
	ctx, cancel := context.WithCancel(context.Background())
	return &sink{s: s, id: id, st: st, target: target, track: track, maxChars: maxChars,
		ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), seen: map[string]bool{}}
}

func (k *sink) signal() {
	select {
	case k.wake <- struct{}{}:
	default:
	}
}

func (k *sink) setURL(u string) {
	k.mu.Lock()
	k.url = u
	k.mu.Unlock()
	k.signal()
}

func (k *sink) currentURL() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.url
}

// enqueue queues a final caption. It never blocks: when the queue is full
// the oldest item is dropped.
func (k *sink) enqueue(c api.Caption) {
	it := newItem(c, k.maxChars, k.s.opts.Clock.Now())
	if len(it.cues) == 0 {
		return
	}
	k.mu.Lock()
	if k.closing || k.seen[c.SegmentId] {
		k.mu.Unlock()
		return
	}
	k.seen[c.SegmentId] = true
	k.order = append(k.order, c.SegmentId)
	if len(k.order) > seenSegments {
		delete(k.seen, k.order[0])
		k.order = k.order[1:]
	}
	if len(k.queue) >= k.s.opts.QueueSize {
		k.queue = k.queue[1:]
		k.dropped++
	}
	k.queue = append(k.queue, it)
	k.mu.Unlock()
	k.signal()
}

// close stops accepting captions; the sink sends what's queued for up to
// drain, then stops.
func (k *sink) close(drain time.Duration) {
	k.mu.Lock()
	k.closing = true
	k.mu.Unlock()
	if drain <= 0 {
		k.cancel()
		return
	}
	k.signal()
	time.AfterFunc(drain, k.cancel)
}

// next waits for the next item; false when the sink is done.
func (k *sink) next() (item, bool) {
	for {
		k.mu.Lock()
		if len(k.queue) > 0 {
			it := k.queue[0]
			k.queue = k.queue[1:]
			k.mu.Unlock()
			return it, true
		}
		closing := k.closing
		k.mu.Unlock()
		if closing {
			return item{}, false
		}
		select {
		case <-k.wake:
		case <-k.ctx.Done():
			return item{}, false
		}
	}
}

func (k *sink) run() {
	defer k.s.sinkDone(k.id)
	defer k.cancel()
	for {
		it, ok := k.next()
		if !ok || k.ctx.Err() != nil {
			k.reportDropped()
			return
		}
		k.deliver(it)
		k.reportDropped()
	}
}

// stale reports an item whose speech ended too long ago to be useful on
// the stream: sending it after an outage would replay old lines.
func (k *sink) stale(it item) bool {
	return k.s.opts.Clock.Now().Sub(it.end) > k.s.opts.MaxAge
}

// deliver sends it, retrying with backoff while the failure may pass and
// the item isn't stale. Retries reuse the seq, so YouTube can tell a
// repeat from a new caption. (For OBS, seq only counts captions.)
func (k *sink) deliver(it item) {
	backoff := k.s.opts.MinBackoff
	var seq int64
	for {
		if k.stale(it) {
			k.mu.Lock()
			k.dropped++
			k.mu.Unlock()
			return
		}
		if seq == 0 {
			seq = k.st.seq.Add(1)
		}
		err := k.send(seq, &it)
		if k.ctx.Err() != nil {
			return // aborted: the run's drain is over
		}
		k.s.recordDelivery(k.id, k.target, seq, err, true)
		if err == nil || !asDelivery(err).retryable {
			if err != nil {
				k.s.log.Warn("stream caption rejected", "session", k.id, "seq", seq, "err", err)
			}
			return
		}
		k.s.log.Debug("stream caption delivery failed; retrying", "session", k.id, "seq", seq, "in", backoff, "err", err)
		t := time.NewTimer(backoff)
		select {
		case <-t.C:
		case <-k.ctx.Done():
			t.Stop()
			return
		}
		backoff = min(backoff*2, k.s.opts.MaxBackoff)
	}
}

// send delivers it once to the sink's target.
func (k *sink) send(seq int64, it *item) error {
	if k.target == api.ObsWebsocket {
		return k.s.obs.send(k.ctx, it)
	}
	url := k.currentURL()
	if url == "" {
		return &deliveryError{code: CodeNoURL, message: "no caption ingestion URL is set"}
	}
	return k.st.yt.post(k.ctx, url, seq, *it)
}

// reportDropped tells admins how many captions were skipped since the
// last report.
func (k *sink) reportDropped() {
	k.mu.Lock()
	n := k.dropped
	k.dropped = 0
	k.mu.Unlock()
	if n == 0 {
		return
	}
	k.s.log.Warn("stream captions dropped: too old to send", "session", k.id, "count", n)
	k.s.logEvent(api.AdminEventLogLevelWarn, CodeDropped, k.id, map[string]any{"count": n})
}
