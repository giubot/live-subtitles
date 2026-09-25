// SPDX-License-Identifier: Apache-2.0

// Package backoff computes capped exponential retry delays with jitter, for
// restarting provider streams and sources and retrying sidecar calls
// (SES-5, 5.5).
package backoff

import (
	"context"
	"math/rand/v2"
	"time"
)

// Policy is capped exponential backoff: Base, 2×Base, 4×Base… up to Max,
// each shortened by a random share of up to Jitter of itself, so that the
// retries of several sessions don't hit a recovering server in lockstep.
type Policy struct {
	Base, Max time.Duration
	// Jitter is the random share of each delay, from 0 (none) to 1
	// (anywhere between 0 and the delay).
	Jitter float64
}

// Delay is the wait before retry number attempt (from 1).
func (p Policy) Delay(attempt int) time.Duration {
	if p.Base <= 0 {
		return 0
	}
	d := p.Base << min(max(attempt-1, 0), 30)
	if d <= 0 || (p.Max > 0 && d > p.Max) { // d <= 0: overflow
		d = p.Max
	}
	if j := min(max(p.Jitter, 0), 1); j > 0 && d > 0 {
		span := time.Duration(float64(d) * j)
		d -= time.Duration(rand.Int64N(int64(span) + 1))
	}
	return d
}

// Wait sleeps for d, returning ctx's error if it ends first.
func Wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
