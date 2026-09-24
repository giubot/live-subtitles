// SPDX-License-Identifier: Apache-2.0

package session

import (
	"slices"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
)

// latencyWindow is how many recent finals the percentiles cover.
const latencyWindow = 200

// latency keeps the recent caption latencies of one track (AI-9; P2-08
// refines the measurement).
type latency struct {
	ring []int
	next int
	last int
}

func (l *latency) add(ms int) {
	l.last = ms
	if len(l.ring) < latencyWindow {
		l.ring = append(l.ring, ms)
		return
	}
	l.ring[l.next] = ms
	l.next = (l.next + 1) % latencyWindow
}

func (l *latency) stats() api.LatencyStats {
	s := slices.Clone(l.ring)
	slices.Sort(s)
	last := l.last
	return api.LatencyStats{P50Ms: percentile(s, 50), P95Ms: percentile(s, 95), LastMs: &last}
}

// percentile is the nearest-rank percentile of sorted values.
func percentile(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	i := (p*len(sorted) + 99) / 100 // ceil(p/100 × n)
	return sorted[max(i, 1)-1]
}

func secondsToDuration(s float32) time.Duration {
	return time.Duration(float64(s) * float64(time.Second))
}
