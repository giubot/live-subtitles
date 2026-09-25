// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRegistryWrite(t *testing.T) {
	for _, c := range []struct {
		name  string
		build func(r *Registry)
		want  string
	}{
		{
			name:  "empty counter family",
			build: func(r *Registry) { r.Counter("x_total", "Things.", "kind") },
			want:  "# HELP x_total Things.\n# TYPE x_total counter\n",
		},
		{
			name: "counter series sorted by labels",
			build: func(r *Registry) {
				c := r.Counter("x_total", "Things.", "kind", "code")
				c.Inc("b", "1")
				c.Add(2.5, "a", "2")
				c.Inc("b", "1")
			},
			want: "# HELP x_total Things.\n# TYPE x_total counter\n" +
				"x_total{kind=\"a\",code=\"2\"} 2.5\n" +
				"x_total{kind=\"b\",code=\"1\"} 2\n",
		},
		{
			name: "unlabelled counter",
			build: func(r *Registry) {
				r.Counter("up_total", "Up.").Inc()
			},
			want: "# HELP up_total Up.\n# TYPE up_total counter\nup_total 1\n",
		},
		{
			name: "label and help escaping",
			build: func(r *Registry) {
				r.Counter("x_total", "Line one\nback\\slash", "v").Inc("say \"hi\"\n\\")
			},
			want: "# HELP x_total Line one\\nback\\\\slash\n# TYPE x_total counter\n" +
				"x_total{v=\"say \\\"hi\\\"\\n\\\\\"} 1\n",
		},
		{
			name: "histogram buckets are cumulative, bounds inclusive",
			build: func(r *Registry) {
				h := r.Histogram("lat_seconds", "Latency.", []float64{0.5, 1, 2}, "track")
				for _, v := range []float64{0.2, 0.5, 1.5, 3} {
					h.Observe(v, "es")
				}
			},
			want: "# HELP lat_seconds Latency.\n# TYPE lat_seconds histogram\n" +
				"lat_seconds_bucket{track=\"es\",le=\"0.5\"} 2\n" +
				"lat_seconds_bucket{track=\"es\",le=\"1\"} 2\n" +
				"lat_seconds_bucket{track=\"es\",le=\"2\"} 3\n" +
				"lat_seconds_bucket{track=\"es\",le=\"+Inf\"} 4\n" +
				"lat_seconds_sum{track=\"es\"} 5.2\n" +
				"lat_seconds_count{track=\"es\"} 4\n",
		},
		{
			name: "gauge func sorted, in registration order",
			build: func(r *Registry) {
				r.GaugeFunc("b", "B.", []string{"s"}, func(_ context.Context, emit func(float64, ...string)) error {
					emit(2, "y")
					emit(1, "x")
					return nil
				})
				r.GaugeFunc("a", "A.", nil, func(_ context.Context, emit func(float64, ...string)) error {
					emit(math.Inf(1))
					return nil
				})
			},
			want: "# HELP b B.\n# TYPE b gauge\nb{s=\"x\"} 1\nb{s=\"y\"} 2\n" +
				"# HELP a A.\n# TYPE a gauge\na +Inf\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := NewRegistry()
			c.build(r)
			var b strings.Builder
			if err := r.Write(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			if b.String() != c.want {
				t.Errorf("got\n%s\nwant\n%s", b.String(), c.want)
			}
		})
	}
}

func TestRegistryGaugeError(t *testing.T) {
	r := NewRegistry()
	boom := errors.New("boom")
	r.GaugeFunc("bad", "Bad.", nil, func(context.Context, func(float64, ...string)) error { return boom })
	r.Counter("good_total", "Good.").Inc()
	var b strings.Builder
	err := r.Write(context.Background(), &b)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
	if !strings.Contains(b.String(), "good_total 1\n") {
		t.Errorf("the other families are missing:\n%s", b.String())
	}
}

func TestRegistryHandler(t *testing.T) {
	r := NewRegistry()
	r.Counter("x_total", "X.").Inc()
	var failed error
	r.GaugeFunc("bad", "Bad.", nil, func(context.Context, func(float64, ...string)) error { return errors.New("boom") })
	rec := httptest.NewRecorder()
	r.Handler(func(_ *http.Request, err error) { failed = err }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != ContentType {
		t.Errorf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "x_total 1") || failed == nil {
		t.Errorf("body %q, onError %v", rec.Body.String(), failed)
	}
}

func TestRegistryMisuse(t *testing.T) {
	for _, c := range []struct {
		name string
		f    func(r *Registry)
	}{
		{"duplicate name", func(r *Registry) { r.Counter("x", "X."); r.Counter("x", "X.") }},
		{"wrong label count", func(r *Registry) { r.Counter("x", "X.", "a").Inc() }},
		{"negative counter", func(r *Registry) { r.Counter("x", "X.").Add(-1) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			c.f(NewRegistry())
		})
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("x_total", "X.", "k")
	h := r.Histogram("h", "H.", []float64{1}, "k")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				c.Inc("a")
				h.Observe(0.5, "a")
				_ = r.Write(context.Background(), &strings.Builder{})
			}
		})
	}
	wg.Wait()
	if c.Value("a") != 800 || h.Count("a") != 800 {
		t.Errorf("counter %v, histogram count %d", c.Value("a"), h.Count("a"))
	}
}
