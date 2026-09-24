// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"sync"

	"github.com/iencodev/live-subtitles/internal/api"
)

// eventBuffer is how many events an admin subscriber may lag behind
// before it is dropped (it reconnects and gets a fresh snapshot).
const eventBuffer = 256

// Events fans admin events (/ws/admin) out to subscribers. Publish never
// blocks: a subscriber whose buffer is full is dropped, like caption
// viewers on the bus.
type Events struct {
	mu     sync.Mutex
	subs   map[chan api.AdminEvent]struct{}
	closed bool
}

// NewEvents returns an empty event stream.
func NewEvents() *Events {
	return &Events{subs: map[chan api.AdminEvent]struct{}{}}
}

// Subscribe returns a channel of events, closed when ctx ends, the
// subscriber falls behind or the stream is closed.
func (e *Events) Subscribe(ctx context.Context) <-chan api.AdminEvent {
	ch := make(chan api.AdminEvent, eventBuffer)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || ctx.Err() != nil {
		close(ch)
		return ch
	}
	e.subs[ch] = struct{}{}
	context.AfterFunc(ctx, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.remove(ch)
	})
	return ch
}

// Publish delivers ev to every subscriber.
func (e *Events) Publish(ev api.AdminEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		select {
		case ch <- ev:
		default:
			e.remove(ch)
		}
	}
}

// Close ends every subscription.
func (e *Events) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	for ch := range e.subs {
		e.remove(ch)
	}
}

// remove closes ch once. Called with mu held.
func (e *Events) remove(ch chan api.AdminEvent) {
	if _, ok := e.subs[ch]; ok {
		delete(e.subs, ch)
		close(ch)
	}
}
