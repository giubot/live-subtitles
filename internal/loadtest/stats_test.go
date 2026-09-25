// SPDX-License-Identifier: Apache-2.0

package loadtest

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	ten := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	tests := []struct {
		name   string
		sorted []float64
		p      float64
		want   float64
	}{
		{"one sample", []float64{42}, 99, 42},
		{"p0 is the minimum", ten, 0, 1},
		{"p100 is the maximum", ten, 100, 10},
		{"median of an even count interpolates", ten, 50, 5.5},
		{"p90", ten, 90, 9.1},
		{"p99", ten, 99, 9.91},
		{"median of an odd count is the middle", []float64{1, 2, 3}, 50, 2},
		{"p25 between two samples", []float64{0, 100}, 25, 25},
		{"above 100 clamps", ten, 150, 10},
		{"below 0 clamps", ten, -5, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Percentile(tt.sorted, tt.p); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Percentile(%v, %v) = %v, want %v", tt.sorted, tt.p, got, tt.want)
			}
		})
	}
	if !math.IsNaN(Percentile(nil, 50)) {
		t.Error("Percentile of no samples should be NaN")
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name    string
		samples []float64
		want    Summary
	}{
		{"empty", nil, Summary{}},
		{"unsorted input", []float64{3, 1, 2}, Summary{N: 3, Min: 1, Mean: 2, P50: 2, P90: 2.8, P95: 2.9, P99: 2.98, Max: 3}},
		{"all equal", []float64{5, 5, 5, 5}, Summary{N: 4, Min: 5, Mean: 5, P50: 5, P90: 5, P95: 5, P99: 5, Max: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Summarize(tt.samples)
			for _, f := range []struct {
				name      string
				got, want float64
			}{
				{"N", float64(got.N), float64(tt.want.N)}, {"Min", got.Min, tt.want.Min}, {"Mean", got.Mean, tt.want.Mean},
				{"P50", got.P50, tt.want.P50}, {"P90", got.P90, tt.want.P90}, {"P95", got.P95, tt.want.P95},
				{"P99", got.P99, tt.want.P99}, {"Max", got.Max, tt.want.Max},
			} {
				if math.Abs(f.got-f.want) > 1e-9 {
					t.Errorf("%s = %v, want %v", f.name, f.got, f.want)
				}
			}
		})
	}
}

func TestSessionStats(t *testing.T) {
	const origin = int64(1_000_000_000_000)
	ms := func(v float64) int64 { return origin + int64(v*1e6) }
	// Two viewers read the same two events; the second viewer is 5 ms
	// behind on the final. The final's audio ended at 1.0 s.
	a := &viewer{samples: []sample{
		{recv: ms(900), key: 1, end: 0.9, pipeMs: 40},
		{recv: ms(1100), key: 2, end: 1.0, pipeMs: 45, final: true},
	}}
	b := &viewer{samples: []sample{
		{recv: ms(902), key: 1, end: 0.9, pipeMs: 40},
		{recv: ms(1105), key: 2, end: 1.0, pipeMs: -1, final: true},
	}}
	lag, fan, pipe, events := sessionStats(origin, []*viewer{a, b})
	if events != 2 {
		t.Errorf("events = %d, want 2", events)
	}
	approx := func(name string, got, want []float64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
		for i := range got {
			if math.Abs(got[i]-want[i]) > 1e-6 {
				t.Errorf("%s = %v, want %v", name, got, want)
			}
		}
	}
	approx("lag", lag, []float64{0, 5})
	approx("fan-out", fan, []float64{0, 0, 2, 5})
	approx("pipeline", pipe, []float64{45}) // the missing latencyMs is skipped
}

func TestParseCPUTime(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"0:00.00", 0, false},
		{"1:02.34", time.Minute + 2340*time.Millisecond, false}, // macOS
		{"00:01:02", time.Minute + 2*time.Second, false},        // Linux
		{"12:00:00", 12 * time.Hour, false},
		{"2-01:00:00", 49 * time.Hour, false},
		{"42", 0, true},
		{"a:10", 0, true},
		{"1:xx", 0, true},
		{"x-01:00:00", 0, true},
		{"1:2:3:4", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseCPUTime(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if d := got - tt.want; d > time.Millisecond || d < -time.Millisecond {
				t.Errorf("ParseCPUTime(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParsePS(t *testing.T) {
	out := []byte(`    1     0  12000   1:00.00
  500     1  40000   0:10.50
  501   500  15000   0:01.00
  502   500  16000   0:02.00
  503     1    100   0:00.01
  bad lines are skipped
`)
	tests := []struct {
		name    string
		pid     int
		want    ProcSample
		wantErr bool
	}{
		{"server with two ffmpeg children", 500, ProcSample{CPU: 10500 * time.Millisecond, RSS: 40000 * 1024,
			Children: 2, ChildCPU: 3 * time.Second, ChildRSS: 31000 * 1024}, false},
		{"no children", 503, ProcSample{CPU: 10 * time.Millisecond, RSS: 100 * 1024}, false},
		{"missing", 999, ProcSample{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePS(out, tt.pid, time.Time{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parsePS = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCPUPercent(t *testing.T) {
	t0 := time.Unix(0, 0)
	from := ProcSample{At: t0, CPU: time.Second, ChildCPU: 0}
	to := ProcSample{At: t0.Add(10 * time.Second), CPU: 16 * time.Second, ChildCPU: time.Second}
	tests := []struct {
		name     string
		to       ProcSample
		children bool
		want     float64
	}{
		{"one and a half cores", to, false, 150},
		{"children", to, true, 10},
		{"no time passed", from, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CPUPercent(from, tt.to, tt.children); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("CPUPercent = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetricValue(t *testing.T) {
	body := `# HELP livesubs_ws_clients Open WebSocket connections by endpoint.
# TYPE livesubs_ws_clients gauge
livesubs_ws_clients{endpoint="admin"} 1
livesubs_ws_clients{endpoint="captions"} 5000
`
	tests := []struct {
		series  string
		want    float64
		wantErr bool
	}{
		{`livesubs_ws_clients{endpoint="captions"}`, 5000, false},
		{`livesubs_ws_clients{endpoint="admin"}`, 1, false},
		{`livesubs_ws_clients{endpoint="ingest"}`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.series, func(t *testing.T) {
			got, err := metricValue(strings.NewReader(body), tt.series)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("metricValue = %v, %v; want %v (err %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
