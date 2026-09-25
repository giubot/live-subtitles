// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// ContentType is the Prometheus text exposition format, version 0.0.4.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// Registry holds metrics and writes them in the Prometheus text exposition
// format (ADM-5). It covers the three kinds the server needs: counters and
// histograms updated as things happen, and gauges read from the services
// at scrape time. It is safe for concurrent use.
type Registry struct {
	mu      sync.Mutex
	metrics []metric
	names   map[string]bool
}

type metric interface {
	desc() *desc
	write(ctx context.Context, w *bufio.Writer) error
}

// desc is a metric family's name, help, type and label names.
type desc struct {
	name, help, kind string
	labels           []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{names: map[string]bool{}} }

func (r *Registry) register(m metric) {
	d := m.desc()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.names[d.name] {
		panic("metrics: duplicate metric " + d.name) // a programming error, caught by tests
	}
	r.names[d.name] = true
	r.metrics = append(r.metrics, m)
}

// Counter registers a counter family with the given label names.
func (r *Registry) Counter(name, help string, labels ...string) *Counter {
	c := &Counter{d: desc{name, help, "counter", labels}, series: map[string]*series{}}
	r.register(c)
	return c
}

// Histogram registers a histogram family with upper bounds buckets (sorted
// ascending; +Inf is implied) and the given label names.
func (r *Registry) Histogram(name, help string, buckets []float64, labels ...string) *Histogram {
	h := &Histogram{d: desc{name, help, "histogram", labels}, buckets: slices.Clone(buckets), series: map[string]*histSeries{}}
	r.register(h)
	return h
}

// GaugeFunc registers a gauge family whose values collect reports at
// scrape time, one call to emit per series (label values in the order of
// labels). An error from collect is returned by Write after the other
// families are written; the series emitted before it are kept.
func (r *Registry) GaugeFunc(name, help string, labels []string, collect func(ctx context.Context, emit func(v float64, labelValues ...string)) error) {
	r.register(&gaugeFunc{d: desc{name, help, "gauge", labels}, collect: collect})
}

// Write writes every family in registration order.
func (r *Registry) Write(ctx context.Context, w io.Writer) error {
	r.mu.Lock()
	ms := slices.Clone(r.metrics)
	r.mu.Unlock()
	bw := bufio.NewWriter(w)
	var errs []error
	for _, m := range ms {
		d := m.desc()
		_, _ = fmt.Fprintf(bw, "# HELP %s %s\n# TYPE %s %s\n", d.name, escapeHelp(d.help), d.name, d.kind)
		if err := m.write(ctx, bw); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.name, err))
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return errors.Join(errs...)
}

// Handler serves the registry. A failing gauge is reported to onError
// (which may be nil) and the rest is served anyway, so one broken source
// doesn't blank the dashboard.
func (r *Registry) Handler(onError func(*http.Request, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", ContentType)
		w.Header().Set("Cache-Control", "no-store")
		if err := r.Write(req.Context(), w); err != nil && onError != nil {
			onError(req, err)
		}
	})
}

// series is one counter series.
type series struct {
	labels []string
	value  float64
}

// Counter is a family of monotonically increasing values.
type Counter struct {
	d      desc
	mu     sync.Mutex
	series map[string]*series
}

func (c *Counter) desc() *desc { return &c.d }

// Inc adds 1 to the series with labelValues.
func (c *Counter) Inc(labelValues ...string) { c.Add(1, labelValues...) }

// Add adds v (which must not be negative) to the series with labelValues.
func (c *Counter) Add(v float64, labelValues ...string) {
	if v < 0 {
		panic("metrics: counter " + c.d.name + " can't decrease")
	}
	key := seriesKey(c.d, labelValues)
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.series[key]
	if s == nil {
		s = &series{labels: slices.Clone(labelValues)}
		c.series[key] = s
	}
	s.value += v
}

// Value is the current value of the series with labelValues.
func (c *Counter) Value(labelValues ...string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := c.series[seriesKey(c.d, labelValues)]; s != nil {
		return s.value
	}
	return 0
}

func (c *Counter) write(_ context.Context, w *bufio.Writer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range sortedKeys(c.series) {
		s := c.series[key]
		writeSample(w, c.d.name, c.d.labels, s.labels, "", "", s.value)
	}
	return nil
}

type histSeries struct {
	labels []string
	counts []uint64 // per bucket, not cumulative; the last one is +Inf
	sum    float64
	count  uint64
}

// Histogram is a family of distributions over fixed buckets.
type Histogram struct {
	d       desc
	buckets []float64
	mu      sync.Mutex
	series  map[string]*histSeries
}

func (h *Histogram) desc() *desc { return &h.d }

// Observe records v in the series with labelValues.
func (h *Histogram) Observe(v float64, labelValues ...string) {
	key := seriesKey(h.d, labelValues)
	i, _ := slices.BinarySearch(h.buckets, v) // first bucket with bound ≥ v
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.series[key]
	if s == nil {
		s = &histSeries{labels: slices.Clone(labelValues), counts: make([]uint64, len(h.buckets)+1)}
		h.series[key] = s
	}
	s.counts[i]++
	s.sum += v
	s.count++
}

// Count is how many values the series with labelValues has observed.
func (h *Histogram) Count(labelValues ...string) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.series[seriesKey(h.d, labelValues)]; s != nil {
		return s.count
	}
	return 0
}

func (h *Histogram) write(_ context.Context, w *bufio.Writer) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, key := range sortedKeys(h.series) {
		s := h.series[key]
		var cum uint64
		for i, n := range s.counts {
			cum += n
			le := "+Inf"
			if i < len(h.buckets) {
				le = formatFloat(h.buckets[i])
			}
			writeSample(w, h.d.name+"_bucket", h.d.labels, s.labels, "le", le, float64(cum))
		}
		writeSample(w, h.d.name+"_sum", h.d.labels, s.labels, "", "", s.sum)
		writeSample(w, h.d.name+"_count", h.d.labels, s.labels, "", "", float64(s.count))
	}
	return nil
}

type gaugeFunc struct {
	d       desc
	collect func(ctx context.Context, emit func(v float64, labelValues ...string)) error
}

func (g *gaugeFunc) desc() *desc { return &g.d }

func (g *gaugeFunc) write(ctx context.Context, w *bufio.Writer) error {
	type sample struct {
		labels []string
		v      float64
	}
	var samples []sample
	err := g.collect(ctx, func(v float64, labelValues ...string) {
		_ = seriesKey(g.d, labelValues) // checks the label count
		samples = append(samples, sample{slices.Clone(labelValues), v})
	})
	slices.SortStableFunc(samples, func(a, b sample) int { return slices.Compare(a.labels, b.labels) })
	for _, s := range samples {
		writeSample(w, g.d.name, g.d.labels, s.labels, "", "", s.v)
	}
	return err
}

// seriesKey identifies a series by its label values. A wrong number of
// label values is a programming error.
func seriesKey(d desc, values []string) string {
	if len(values) != len(d.labels) {
		panic(fmt.Sprintf("metrics: %s wants %d label values, got %d", d.name, len(d.labels), len(values)))
	}
	return strings.Join(values, "\xff")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// writeSample writes `name{l1="v1",…,extra="extraValue"} value`. A write
// error sticks to w and comes back from its Flush.
func writeSample(w *bufio.Writer, name string, labels, values []string, extra, extraValue string, v float64) {
	var b strings.Builder
	b.WriteString(name)
	if len(labels) > 0 || extra != "" {
		b.WriteByte('{')
		sep := ""
		for i, l := range labels {
			fmt.Fprintf(&b, `%s%s="%s"`, sep, l, escapeLabel(values[i]))
			sep = ","
		}
		if extra != "" {
			fmt.Fprintf(&b, `%s%s="%s"`, sep, extra, escapeLabel(extraValue))
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(formatFloat(v))
	b.WriteByte('\n')
	_, _ = w.WriteString(b.String())
}

func formatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

var (
	labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }
func escapeHelp(s string) string  { return helpEscaper.Replace(s) }
