// SPDX-License-Identifier: Apache-2.0

// Package loadtest drives a Live Subtitles server with many sessions and
// caption viewers and reports delivery latency, drops, throughput and the
// server's CPU and memory (P4-05). cmd/loadtest is its command line.
package loadtest

import (
	"fmt"
	"math"
	"slices"
)

// Summary describes a set of samples (milliseconds, in this package).
type Summary struct {
	N    int     `json:"n"`
	Min  float64 `json:"min"`
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

// Summarize sorts samples in place and describes them. An empty set gives
// the zero Summary.
func Summarize(samples []float64) Summary {
	if len(samples) == 0 {
		return Summary{}
	}
	slices.Sort(samples)
	var sum float64
	for _, v := range samples {
		sum += v
	}
	return Summary{
		N:    len(samples),
		Min:  samples[0],
		Mean: sum / float64(len(samples)),
		P50:  Percentile(samples, 50),
		P90:  Percentile(samples, 90),
		P95:  Percentile(samples, 95),
		P99:  Percentile(samples, 99),
		Max:  samples[len(samples)-1],
	}
}

// Percentile is the p-th percentile (0–100) of sorted, by linear
// interpolation between the closest ranks (the method spreadsheets and
// NumPy use by default). It is NaN for an empty slice.
func Percentile(sorted []float64, p float64) float64 {
	switch n := len(sorted); {
	case n == 0:
		return math.NaN()
	case n == 1 || p <= 0:
		return sorted[0]
	case p >= 100:
		return sorted[n-1]
	}
	rank := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	frac := rank - float64(lo)
	if lo+1 >= len(sorted) {
		return sorted[lo]
	}
	return sorted[lo] + frac*(sorted[lo+1]-sorted[lo])
}

// String is a one-line "p50/p95/p99/max" view in milliseconds.
func (s Summary) String() string {
	if s.N == 0 {
		return "no samples"
	}
	return fmt.Sprintf("p50 %s  p95 %s  p99 %s  max %s  (n=%d)", ms(s.P50), ms(s.P95), ms(s.P99), ms(s.Max), s.N)
}

func ms(v float64) string {
	if v < 10 {
		return fmt.Sprintf("%.2f ms", v)
	}
	return fmt.Sprintf("%.0f ms", v)
}
