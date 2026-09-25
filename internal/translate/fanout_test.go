// SPDX-License-Identifier: Apache-2.0

package translate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fakeTranslator answers "<to>:<text>". With gate set, each call waits for
// a value on gate (or ctx). partials are streamed before the result when
// the fake is used as a StreamingTranslator.
type fakeTranslator struct {
	gate     chan struct{}
	started  chan domain.TranslateRequest // optional; gets each request as it starts
	fail     func(req domain.TranslateRequest) error
	partials func(req domain.TranslateRequest) []string

	mu    sync.Mutex
	reqs  []domain.TranslateRequest
	warms int
}

func (f *fakeTranslator) Kind() domain.ProviderKind { return api.ProviderKindMock }

func (f *fakeTranslator) Translate(ctx context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- req
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return domain.TranslateResult{}, ctx.Err()
		}
	}
	if f.fail != nil {
		if err := f.fail(req); err != nil {
			return domain.TranslateResult{}, err
		}
	}
	return domain.TranslateResult{Text: req.To + ":" + req.Text, Usage: domain.Usage{InputTokens: 1, OutputTokens: 2}}, nil
}

func (f *fakeTranslator) requests() []domain.TranslateRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

func (f *fakeTranslator) Warm(context.Context) error {
	f.mu.Lock()
	f.warms++
	f.mu.Unlock()
	return nil
}

// streamingFake adds TranslateStream to fakeTranslator.
type streamingFake struct{ *fakeTranslator }

func (s streamingFake) TranslateStream(ctx context.Context, req domain.TranslateRequest, partial func(string)) (domain.TranslateResult, error) {
	if s.partials != nil {
		for _, p := range s.partials(req) {
			partial(p)
		}
	}
	return s.Translate(ctx, req)
}

// recorder collects published captions.
type recorder struct {
	mu     sync.Mutex
	out    []api.Caption
	usage  domain.Usage
	failed []string
}

func (r *recorder) publish(c api.Caption) {
	r.mu.Lock()
	r.out = append(r.out, c)
	r.mu.Unlock()
}

func (r *recorder) addUsage(u domain.Usage) {
	r.mu.Lock()
	r.usage = r.usage.Add(u)
	r.mu.Unlock()
}

func (r *recorder) fail(lang domain.LanguageCode, c api.Caption, _ error) {
	r.mu.Lock()
	r.failed = append(r.failed, lang+"/"+c.SegmentId)
	r.mu.Unlock()
}

// lines renders the published captions as "lang seg F|I text".
func (r *recorder) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.out {
		kind := "I"
		if c.Final {
			kind = "F"
		}
		out = append(out, fmt.Sprintf("%s %s %s %s", c.Lang, c.SegmentId, kind, c.Text))
	}
	return out
}

var quiet = slog.New(slog.DiscardHandler)

func caption(seg, text string, final bool, src domain.LanguageCode) api.Caption {
	return api.Caption{SessionId: "main", Lang: domain.SourceTrack, SegmentId: seg, Text: text, Final: final, SourceLang: src}
}

func newTestFanout(ctx context.Context, tr domain.Translator, rec *recorder, targets []string, k int) *Fanout {
	return New(ctx, Options{
		SessionID:        "main",
		Translator:       tr,
		Targets:          targets,
		ContextSentences: k,
		PartialInterval:  -1,
		Publish:          rec.publish,
		Usage:            rec.addUsage,
		Failed:           rec.fail,
		Logger:           quiet,
	})
}

func TestFanoutFinalsInOrder(t *testing.T) {
	tests := []struct {
		name    string
		targets []string
		src     domain.LanguageCode
		want    []string
		calls   int
	}{
		{
			name:    "translated",
			targets: []string{"es"},
			src:     "en",
			want:    []string{"es s0 F es:line 0", "es s1 F es:line 1", "es s2 F es:line 2"},
			calls:   3,
		},
		{
			name:    "passthrough when the target is the source",
			targets: []string{"en"},
			src:     "en",
			want:    []string{"en s0 F line 0", "en s1 F line 1", "en s2 F line 2"},
		},
		{
			name:    "duplicates, empty and source targets are ignored",
			targets: []string{"es", "", "source", "es"},
			src:     "en",
			want:    []string{"es s0 F es:line 0", "es s1 F es:line 1", "es s2 F es:line 2"},
			calls:   3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &fakeTranslator{}
			rec := &recorder{}
			f := newTestFanout(t.Context(), tr, rec, tt.targets, 3)
			for i := range 3 {
				f.Push(caption(fmt.Sprintf("s%d", i), fmt.Sprintf("line %d", i), true, tt.src))
			}
			f.Close()
			if got := rec.lines(); !slices.Equal(got, tt.want) {
				t.Errorf("published = %q, want %q", got, tt.want)
			}
			if got := len(tr.requests()); got != tt.calls {
				t.Errorf("translator calls = %d, want %d", got, tt.calls)
			}
			if want := int64(tt.calls); rec.usage.InputTokens != want {
				t.Errorf("usage input tokens = %d, want %d", rec.usage.InputTokens, want)
			}
		})
	}
}

func TestFanoutTwoTargetsIndependent(t *testing.T) {
	tr := &fakeTranslator{}
	rec := &recorder{}
	f := newTestFanout(t.Context(), tr, rec, []string{"es", "en"}, 3)
	f.Push(caption("s0", "hola", true, "es"))
	f.Push(caption("s1", "hello", true, "en"))
	f.Close()
	got := rec.lines()
	slices.Sort(got)
	want := []string{"en s0 F en:hola", "en s1 F hello", "es s0 F hola", "es s1 F es:hello"}
	if !slices.Equal(got, want) {
		t.Errorf("published = %q, want %q", got, want)
	}
	if !slices.Equal(f.Targets(), []string{"es", "en"}) {
		t.Errorf("Targets() = %q", f.Targets())
	}
}

// Interims are debounced to the translator's pace: while one translation
// is in flight only the newest interim waits, and a final supersedes the
// pending interim of its segment. Finals are never dropped.
func TestFanoutDebounce(t *testing.T) {
	tests := []struct {
		name  string
		after []api.Caption // pushed while the first interim is in flight
		want  []string      // translated texts, in call order
	}{
		{
			name:  "newest interim wins",
			after: []api.Caption{caption("s0", "a b", false, "en"), caption("s0", "a b c", false, "en")},
			want:  []string{"a", "a b c"},
		},
		{
			name: "final supersedes its pending interim",
			after: []api.Caption{
				caption("s0", "a b", false, "en"), caption("s0", "a b c.", true, "en"),
			},
			want: []string{"a", "a b c."},
		},
		{
			name: "finals go before the interim of the next segment",
			after: []api.Caption{
				caption("s0", "a b.", true, "en"), caption("s1", "d", false, "en"),
				caption("s1", "d e.", true, "en"), caption("s2", "f", false, "en"),
			},
			want: []string{"a", "a b.", "d e.", "f"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &fakeTranslator{gate: make(chan struct{}), started: make(chan domain.TranslateRequest, 16)}
			rec := &recorder{}
			f := newTestFanout(t.Context(), tr, rec, []string{"es"}, 3)
			f.Push(caption("s0", "a", false, "en"))
			<-tr.started // the first interim is in flight
			for _, c := range tt.after {
				f.Push(c)
			}
			for range tt.want {
				tr.gate <- struct{}{}
			}
			// Close drops interims that are still pending, so wait for the last call.
			deadline := time.After(5 * time.Second)
			for len(tr.requests()) < len(tt.want) {
				select {
				case <-deadline:
					t.Fatalf("calls = %d, want %d", len(tr.requests()), len(tt.want))
				case <-time.After(time.Millisecond):
				}
			}
			f.Close()
			var got []string
			for _, r := range tr.requests() {
				got = append(got, r.Text)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("translated = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFanoutContextWindow(t *testing.T) {
	tests := []struct {
		name string
		k    int
		want [][]string // context of each request
	}{
		{name: "k=2", k: 2, want: [][]string{nil, {"one."}, {"one.", "two."}, {"one.", "two."}, {"two.", "three."}}},
		{name: "k=0 sends no context", k: 0, want: [][]string{nil, nil, nil, nil, nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &fakeTranslator{}
			rec := &recorder{}
			f := newTestFanout(t.Context(), tr, rec, []string{"es"}, tt.k)
			f.Push(caption("s0", "one.", true, "en"))
			f.Push(caption("s1", "two.", true, "en"))
			// Interims get the context but don't add to it. Wait for it so
			// the next final doesn't supersede it.
			waitCalls(t, tr, 2)
			f.Push(caption("s2", "thr", false, "en"))
			waitCalls(t, tr, 3)
			f.Push(caption("s2", "three.", true, "en"))
			f.Push(caption("s3", "four.", true, "en"))
			f.Close()
			reqs := tr.requests()
			if len(reqs) != len(tt.want) {
				t.Fatalf("requests = %d, want %d", len(reqs), len(tt.want))
			}
			for i, r := range reqs {
				if !slices.Equal(r.Context, tt.want[i]) {
					t.Errorf("request %d (%q) context = %q, want %q", i, r.Text, r.Context, tt.want[i])
				}
			}
		})
	}
}

func waitCalls(t *testing.T, tr *fakeTranslator, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for len(tr.requests()) < n {
		select {
		case <-deadline:
			t.Fatalf("calls = %d, want %d", len(tr.requests()), n)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestFanoutRequestFields(t *testing.T) {
	tr := &fakeTranslator{}
	rec := &recorder{}
	g := &domain.Glossary{Id: "g", DoNotTranslate: []string{"Kubernetes"}}
	f := New(t.Context(), Options{SessionID: "main", Translator: tr, Targets: []string{"es"}, Glossary: g, Publish: rec.publish})
	f.Push(caption("s0", "hello", true, "en"))
	f.Close()
	reqs := tr.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	r := reqs[0]
	if r.SessionID != "main" || r.SegmentID != "s0" || r.From != "en" || r.To != "es" || !r.Final || r.Glossary != g {
		t.Errorf("request = %+v", r)
	}
}

func TestFanoutErrors(t *testing.T) {
	boom := errors.New("boom")
	tr := &fakeTranslator{fail: func(req domain.TranslateRequest) error {
		if strings.HasPrefix(req.Text, "bad") {
			return boom
		}
		return nil
	}}
	rec := &recorder{}
	f := newTestFanout(t.Context(), tr, rec, []string{"es"}, 3)
	f.Push(caption("s0", "bad interim", false, "en"))
	waitCalls(t, tr, 1)
	f.Push(caption("s0", "bad final.", true, "en"))
	f.Push(caption("s1", "good.", true, "en"))
	f.Close()
	if want := []string{"es/s0"}; !slices.Equal(rec.failed, want) {
		t.Errorf("failed = %q, want %q (interims aren't reported)", rec.failed, want)
	}
	if got, want := rec.lines(), []string{"es s1 F es:good."}; !slices.Equal(got, want) {
		t.Errorf("published = %q, want %q", got, want)
	}
}

func TestFanoutCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	tr := &fakeTranslator{gate: make(chan struct{}), started: make(chan domain.TranslateRequest, 4)}
	rec := &recorder{}
	f := newTestFanout(ctx, tr, rec, []string{"es"}, 3)
	f.Push(caption("s0", "one.", true, "en"))
	f.Push(caption("s1", "two.", true, "en"))
	<-tr.started
	cancel()
	done := make(chan struct{})
	go func() { f.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close didn't return after cancel")
	}
	if len(rec.failed) != 0 || len(rec.lines()) != 0 {
		t.Errorf("failed = %q, published = %q; want nothing after cancel", rec.failed, rec.lines())
	}
}

func TestFanoutTimeout(t *testing.T) {
	tr := &fakeTranslator{gate: make(chan struct{})} // never released
	rec := &recorder{}
	f := New(t.Context(), Options{Translator: tr, Targets: []string{"es"}, Timeout: 10 * time.Millisecond, Logger: quiet,
		Publish: rec.publish, Failed: rec.fail})
	f.Push(caption("s0", "one.", true, "en"))
	f.Close()
	if want := []string{"es/s0"}; !slices.Equal(rec.failed, want) {
		t.Errorf("failed = %q, want %q", rec.failed, want)
	}
}

func TestFanoutStreamingFinal(t *testing.T) {
	tests := []struct {
		name     string
		interim  bool // an interim translation is shown first
		partials []string
		want     []string
	}{
		{
			name:     "partials become interims",
			partials: []string{"es:a", "es:a b"},
			want:     []string{"es s0 I es:a", "es s0 I es:a b", "es s0 F es:a b c."},
		},
		{
			name:     "partials never shrink the shown interim",
			interim:  true,
			partials: []string{"es:a", "es:a b", "es:a b c"},
			want:     []string{"es s0 I es:a b", "es s0 I es:a b c", "es s0 F es:a b c."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &fakeTranslator{partials: func(req domain.TranslateRequest) []string {
				if req.Final {
					return tt.partials
				}
				return nil
			}}
			rec := &recorder{}
			f := newTestFanout(t.Context(), streamingFake{base}, rec, []string{"es"}, 3)
			if tt.interim {
				f.Push(caption("s0", "a b", false, "en"))
				waitCalls(t, base, 1)
			}
			f.Push(caption("s0", "a b c.", true, "en"))
			f.Close()
			if got := rec.lines(); !slices.Equal(got, tt.want) {
				t.Errorf("published = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFanoutWarm(t *testing.T) {
	tr := &fakeTranslator{}
	f := New(t.Context(), Options{Translator: tr, Targets: []string{"es"}, Publish: func(api.Caption) {}})
	deadline := time.After(5 * time.Second)
	for {
		tr.mu.Lock()
		n := tr.warms
		tr.mu.Unlock()
		if n == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Warm not called")
		case <-time.After(time.Millisecond):
		}
	}
	f.Close()
}
