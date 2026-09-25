// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/metrics"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestArrivals(t *testing.T) {
	t0 := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	// Three 20 ms frames arriving 20 ms apart, then one after a 1 s stall.
	var a arrivals
	a.add(ms(20), t0)
	a.add(ms(40), t0.Add(ms(20)))
	a.add(ms(60), t0.Add(ms(40)))
	a.add(ms(40), t0.Add(ms(900))) // not forward on the session clock: ignored
	a.add(ms(80), t0.Add(ms(1040)))

	for _, c := range []struct {
		name string
		end  time.Duration
		want time.Duration // after t0
	}{
		{"inside the first frame", ms(5), 0},
		{"end of the first frame", ms(20), 0},
		{"inside the second frame", ms(30), ms(20)},
		{"end of the third frame", ms(60), ms(40)},
		{"after the stall", ms(70), ms(1040)},
		{"past the newest frame", ms(500), ms(1040)},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := a.at(c.end)
			if !ok || got.Sub(t0) != c.want {
				t.Errorf("at(%v) = %v (%v), want t0+%v", c.end, got.Sub(t0), ok, c.want)
			}
		})
	}

	if _, ok := (&arrivals{}).at(ms(10)); ok {
		t.Error("an empty map answered")
	}

	// Once the window is full, older audio is extrapolated from the oldest frame.
	var full arrivals
	for i := range arrivalWindow + 10 {
		full.add(ms(20*(i+1)), t0.Add(ms(20*i)))
	}
	if full.len() != arrivalWindow {
		t.Fatalf("window holds %d", full.len())
	}
	oldest := full.get(0)
	if oldest.end != ms(20*11) {
		t.Errorf("oldest frame ends at %v", oldest.end)
	}
	if got, _ := full.at(ms(20)); got != oldest.at.Add(ms(20)-oldest.end) {
		t.Errorf("extrapolated %v, want %v", got, oldest.at.Add(ms(20)-oldest.end))
	}
}

// stepEvent is one final of stepASR: its audio end, how long recognition
// takes and what the provider reports.
type stepEvent struct {
	end   time.Duration
	delay time.Duration
	usage domain.Usage
}

// stepASR takes in all the audio, then emits one final per step in
// lockstep with the test: it advances the fake clock by the step's delay,
// emits, and calls wait before the next step.
type stepASR struct {
	kind  domain.ProviderKind
	clock *fakeClock
	steps []stepEvent
	wait  func(i int)
}

func (a *stepASR) Kind() domain.ProviderKind { return a.kind }

func (a *stepASR) Start(ctx context.Context, _ domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	in, out := make(chan domain.AudioFrame, 16), make(chan domain.ASREvent)
	go func() {
		defer close(out)
		for range in {
		}
		start := time.Duration(0)
		for i, s := range a.steps {
			a.clock.Advance(s.delay)
			ev := domain.ASREvent{SegmentID: fmt.Sprintf("s-%d", i), Text: fmt.Sprintf("sentence %d", i), Final: true,
				Start: start, End: s.end, Lang: "en", Usage: s.usage}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
			a.wait(i)
			start = s.end
		}
	}()
	return in, out, nil
}

// stepTranslator translates in `delay` of fake time, once the pass-through
// track has published its caption (so it measures no translation time).
type stepTranslator struct {
	kind  domain.ProviderKind
	clock *fakeClock
	delay time.Duration
	usage domain.Usage
	ready func(segment string)
}

func (s *stepTranslator) Kind() domain.ProviderKind { return s.kind }

func (s *stepTranslator) Translate(ctx context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	s.ready(req.SegmentID)
	s.clock.Advance(s.delay)
	return domain.TranslateResult{Text: "[" + req.To + "] " + req.Text, Usage: s.usage}, nil
}

// Latency per track through the pipeline on a fake clock. The unpaced
// source delivers all its audio at t0, so each caption's latency is the
// fake time elapsed since then: recognition for `source` and the
// pass-through `en` track, plus 300 ms of translation for `es`.
func TestLatencyAndUsagePerTrack(t *testing.T) {
	steps := []stepEvent{
		{end: 1 * time.Second, delay: 500 * time.Millisecond, usage: domain.Usage{InputTokens: 1000, OutputTokens: 100}},
		{end: 2 * time.Second, delay: 200 * time.Millisecond, usage: domain.Usage{InputTokens: 1000, OutputTokens: 100}},
		{end: 3 * time.Second, delay: 800 * time.Millisecond, usage: domain.Usage{InputTokens: 1000, OutputTokens: 100}},
	}
	// Elapsed fake time at each caption: 500, 800 (es), 1000, 1300 (es), 2100, 2400 (es).
	wantSource := api.LatencyStats{P50Ms: 1000, P95Ms: 2100, LastMs: ptr(2100)}
	wantES := api.LatencyStats{P50Ms: 1300, P95Ms: 2400, LastMs: ptr(2400)}
	// Tokens: 3 × ASR (1000 in, 100 out) + 3 × es translation (50 in, 20 out);
	// 3 s of audio sent.
	pricing := metrics.Pricing{Gemini: metrics.Prices{InputPerMTok: 1, OutputPerMTok: 10, AudioPerMin: 0.6}}
	geminiCost := 3150e-6*1 + 360e-6*10 + 3.0/60*0.6

	for _, c := range []struct {
		kind     domain.ProviderKind
		wantCost float64
	}{
		{api.ProviderKindGemini, geminiCost},
		{api.ProviderKindLocal, 0},
		{api.ProviderKindMock, 0},
	} {
		t.Run(string(c.kind), func(t *testing.T) {
			clock := &fakeClock{now: time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)}
			var e *env
			before := 0 // finals per track stored by earlier runs
			waitFinals := func(track string, n int) {
				deadline := time.Now().Add(10 * time.Second)
				for len(finals(t, e.store, "main", track)) < before+n {
					if time.Now().After(deadline) {
						t.Errorf("%s: fewer than %d finals", track, n)
						return
					}
					time.Sleep(2 * time.Millisecond)
				}
			}
			asr := &stepASR{kind: c.kind, clock: clock, steps: steps}
			asr.wait = func(i int) { waitFinals("es", i+1) }
			tr := &stepTranslator{kind: c.kind, clock: clock, delay: 300 * time.Millisecond,
				usage: domain.Usage{InputTokens: 50, OutputTokens: 20}}
			tr.ready = func(segment string) {
				var i int
				_, _ = fmt.Sscanf(segment[strings.LastIndex(segment, "s-"):], "s-%d", &i)
				waitFinals("en", i+1)
			}
			s := sess("main", "es", "en")
			s.Provider = api.ProviderChoice(c.kind)
			e = newEnv(t, Options{Clock: clock, Pricing: &pricing, StatusInterval: time.Hour,
				Providers: map[domain.ProviderKind]Provider{c.kind: {ASR: asr, Translator: tr}}}, s)
			events := e.m.Events().Subscribe(t.Context())

			if _, err := e.m.Start(t.Context(), "main", &fake.Source{Duration: 3 * time.Second}); err != nil {
				t.Fatal(err)
			}
			waitState(t, e.m, "main", api.SessionStateIdle)

			// Every final carries its latency.
			for track, want := range map[string][]int{"source": {500, 1000, 2100}, "en": {500, 1000, 2100}, "es": {800, 1300, 2400}} {
				got := finals(t, e.store, "main", track)
				if len(got) != len(want) {
					t.Fatalf("%s: %d finals", track, len(got))
				}
				for i, f := range got {
					if f.LatencyMs == nil || *f.LatencyMs != want[i] {
						t.Errorf("%s final %d latency %v, want %d", track, i, f.LatencyMs, want[i])
					}
				}
			}

			// The stopped session keeps its stats, in the status and on /ws/admin.
			st := e.m.StatusOf("main")
			checkStats(t, st, map[string]api.LatencyStats{"source": wantSource, "en": wantSource, "es": wantES}, 3, 3150, 360, c.wantCost)
			var last *api.SessionStatus
			for last == nil || last.State != api.SessionStateIdle {
				select {
				case ev := <-events:
					if ev.Type == api.AdminEventTypeSessionStatus {
						last = ev.Status
					}
				case <-time.After(5 * time.Second):
					t.Fatal("no idle status event")
				}
			}
			checkStats(t, *last, map[string]api.LatencyStats{"source": wantSource, "en": wantSource, "es": wantES}, 3, 3150, 360, c.wantCost)

			// A second run adds to the session's usage; its latencies replace the first run's.
			before = len(steps)
			if _, err := e.m.Start(t.Context(), "main", &fake.Source{Duration: 3 * time.Second}); err != nil {
				t.Fatal(err)
			}
			waitState(t, e.m, "main", api.SessionStateIdle)
			st = e.m.StatusOf("main")
			if u := st.Usage; u == nil || *u.AudioSeconds != 6 || *u.InputTokens != 6300 ||
				math.Abs(float64(*u.EstimatedCostUsd)-2*c.wantCost) > 1e-6 {
				t.Errorf("usage after two runs %+v", u)
			}
			if l := (*st.Latency)["es"]; l.LastMs == nil || *l.LastMs != 2400 {
				t.Errorf("es latency after the second run %+v", l)
			}

			// Forgetting the session drops them.
			if err := e.m.Forget("main"); err != nil {
				t.Fatal(err)
			}
			if st := e.m.StatusOf("main"); st.Usage != nil || st.Latency != nil {
				t.Errorf("forgotten session still has stats: %+v", st)
			}
		})
	}
}

func checkStats(t *testing.T, st api.SessionStatus, latency map[string]api.LatencyStats, audio float32, in, out int64, cost float64) {
	t.Helper()
	if st.Latency == nil {
		t.Fatalf("no latency in %+v", st)
	}
	for track, want := range latency {
		got, ok := (*st.Latency)[track]
		if !ok || got.P50Ms != want.P50Ms || got.P95Ms != want.P95Ms || got.LastMs == nil || *got.LastMs != *want.LastMs {
			t.Errorf("%s latency %+v (last %v), want %+v (last %d)", track, got, got.LastMs, want, *want.LastMs)
		}
	}
	u := st.Usage
	if u == nil || u.AudioSeconds == nil || u.InputTokens == nil || u.OutputTokens == nil || u.EstimatedCostUsd == nil {
		t.Fatalf("usage %+v", u)
	}
	if *u.AudioSeconds != audio || *u.InputTokens != in || *u.OutputTokens != out ||
		math.Abs(float64(*u.EstimatedCostUsd)-cost) > 1e-6 {
		t.Errorf("usage audio %v in %d out %d cost %v, want %v %d %d %v",
			*u.AudioSeconds, *u.InputTokens, *u.OutputTokens, *u.EstimatedCostUsd, audio, in, out, cost)
	}
}

func TestStatusJSON(t *testing.T) {
	st := api.SessionStatus{SessionId: "main", State: api.SessionStateLive, Viewers: 3}
	lat := map[string]api.LatencyStats{"source": {P50Ms: 900, P95Ms: 1500, LastMs: ptr(800)}}
	st.Latency = &lat
	st.Usage = totals{usage: domain.Usage{AudioSeconds: 90}, costUSD: 0.25}.stats()
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"latency":{"source":{"lastMs":800,"p50Ms":900,"p95Ms":1500}},"sessionId":"main","state":"live",` +
		`"usage":{"audioSeconds":90,"estimatedCostUsd":0.25},"viewers":3}`
	if string(b) != want {
		t.Errorf("status JSON\n got %s\nwant %s", b, want)
	}
}

// The mock provider costs nothing, even at Gemini prices.
func TestMockIsFree(t *testing.T) {
	e := newEnv(t, Options{Providers: map[domain.ProviderKind]Provider{
		api.ProviderKindMock: {ASR: &mock.ASR{}, Translator: &mock.Translator{}},
	}}, sess("main", "es"))
	if _, err := e.m.Start(t.Context(), "main", &fake.Source{Duration: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.m, "main", api.SessionStateIdle)
	u := e.m.StatusOf("main").Usage
	if u == nil || *u.AudioSeconds != 5 || *u.EstimatedCostUsd != 0 {
		t.Errorf("usage %+v", u)
	}
}

func ptr[T any](v T) *T { return &v }
