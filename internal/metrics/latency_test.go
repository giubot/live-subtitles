// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"testing"
)

func TestPercentile(t *testing.T) {
	ten := []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	hundred := make([]int, 100)
	for i := range hundred {
		hundred[i] = i + 1
	}
	for _, c := range []struct {
		name   string
		sorted []int
		p      int
		want   int
	}{
		{"empty", nil, 50, 0},
		{"one value p50", []int{7}, 50, 7},
		{"one value p95", []int{7}, 95, 7},
		{"two values p50", []int{1, 9}, 50, 1},
		{"two values p95", []int{1, 9}, 95, 9},
		{"ten p50", ten, 50, 50},
		{"ten p95", ten, 95, 100},
		{"ten p90", ten, 90, 90},
		{"ten p100", ten, 100, 100},
		{"ten p1", ten, 1, 10},
		{"hundred p50", hundred, 50, 50},
		{"hundred p95", hundred, 95, 95},
		{"hundred p99", hundred, 99, 99},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Percentile(c.sorted, c.p); got != c.want {
				t.Errorf("Percentile(%v, %d) = %d, want %d", c.sorted, c.p, got, c.want)
			}
		})
	}
}

func TestLatencyWindow(t *testing.T) {
	for _, c := range []struct {
		name          string
		add           []int
		p50, p95, lst int
	}{
		{"unordered", []int{300, 100, 200}, 200, 300, 200},
		{"single", []int{1500}, 1500, 1500, 1500},
		{
			// The first LatencyWindow samples (all 10 s) fall out of the window.
			"old samples fall out",
			append(repeat(10_000, LatencyWindow), repeat(1_000, LatencyWindow)...),
			1_000, 1_000, 1_000,
		},
		{
			"half the window replaced",
			append(repeat(10_000, LatencyWindow), repeat(1_000, LatencyWindow/2)...),
			1_000, 10_000, 1_000,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var l Latency
			for _, ms := range c.add {
				l.Add(ms)
			}
			s := l.Stats()
			if s.P50Ms != c.p50 || s.P95Ms != c.p95 || s.LastMs == nil || *s.LastMs != c.lst {
				t.Errorf("stats p50 %d p95 %d last %v, want %d %d %d", s.P50Ms, s.P95Ms, s.LastMs, c.p50, c.p95, c.lst)
			}
			if l.Count() != len(c.add) {
				t.Errorf("count %d, want %d", l.Count(), len(c.add))
			}
		})
	}
}

func repeat(v, n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = v
	}
	return s
}
