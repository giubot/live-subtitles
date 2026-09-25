// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"slices"

	"github.com/iencodev/live-subtitles/internal/api"
)

// LatencyWindow is how many recent samples the percentiles cover.
const LatencyWindow = 200

// Latency keeps the recent latencies of one caption track (AI-9, 5.1).
// The zero value is ready to use; it isn't safe for concurrent use.
type Latency struct {
	ring  []int
	next  int
	last  int
	count int
}

// Add records one latency in milliseconds.
func (l *Latency) Add(ms int) {
	l.last = ms
	l.count++
	if len(l.ring) < LatencyWindow {
		l.ring = append(l.ring, ms)
		return
	}
	l.ring[l.next] = ms
	l.next = (l.next + 1) % LatencyWindow
}

// Count is how many latencies were ever added.
func (l *Latency) Count() int { return l.count }

// Stats are the p50, p95 and last latency over the window.
func (l *Latency) Stats() api.LatencyStats {
	s := slices.Clone(l.ring)
	slices.Sort(s)
	last := l.last
	return api.LatencyStats{P50Ms: Percentile(s, 50), P95Ms: Percentile(s, 95), LastMs: &last}
}

// Percentile is the nearest-rank p-th percentile (0 < p ≤ 100) of sorted
// values: the smallest value with at least p% of the values at or below
// it. It's 0 for no values.
func Percentile(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	i := (p*len(sorted) + 99) / 100 // ceil(p/100 × n)
	return sorted[min(max(i, 1), len(sorted))-1]
}
