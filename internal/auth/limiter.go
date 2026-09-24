// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"sync"
	"time"
)

// limiter counts failed logins per client and blocks a client after max
// failures within window, until the oldest of them leaves the window.
type limiter struct {
	max    int
	window time.Duration

	mu    sync.Mutex
	fails map[string][]time.Time // oldest first
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, fails: map[string][]time.Time{}}
}

// retryAfter is how long client must wait before trying again; 0 means now.
func (l *limiter) retryAfter(client string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.prune(client, now)
	if len(f) < l.max {
		return 0
	}
	return f[len(f)-l.max].Add(l.window).Sub(now)
}

// fail records a failed attempt.
func (l *limiter) fail(client string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[client] = append(l.prune(client, now), now)
	if len(l.fails) > 1024 { // keep the map bounded when many clients fail once
		for c := range l.fails {
			l.prune(c, now)
		}
	}
}

// reset forgets the client's failures after a successful login.
func (l *limiter) reset(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, client)
}

// prune drops failures older than the window. Called with mu held.
func (l *limiter) prune(client string, now time.Time) []time.Time {
	f := l.fails[client]
	i := 0
	for i < len(f) && now.Sub(f[i]) >= l.window {
		i++
	}
	f = f[i:]
	if len(f) == 0 {
		delete(l.fails, client)
		return nil
	}
	l.fails[client] = f
	return f
}
