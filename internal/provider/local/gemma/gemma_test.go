// SPDX-License-Identifier: Apache-2.0

package gemma

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fakeOllama is an httptest Ollama /api/chat. It streams reply as NDJSON
// chunks, one per word.
type fakeOllama struct {
	*httptest.Server
	reply  string
	status int    // non-200 answers with {"error": errMsg}
	errMsg string // with status 200: an error line mid-stream
	hold   chan struct{}

	mu       sync.Mutex
	reqs     []chatRequest
	inflight atomic.Int32
	peak     atomic.Int32
}

func newFakeOllama(t *testing.T, reply string) *fakeOllama {
	f := &fakeOllama{reply: reply}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeOllama) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad json"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	n := f.inflight.Add(1)
	defer f.inflight.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-r.Context().Done():
			return
		}
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		fmt.Fprintf(w, `{"error":%q}`, f.errMsg)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	if len(req.Messages) == 0 { // load only
		_ = enc.Encode(map[string]any{"model": req.Model, "done": true, "done_reason": "load"})
		return
	}
	words := strings.SplitAfter(f.reply, " ")
	for i, word := range words {
		if f.errMsg != "" && i == 1 {
			_ = enc.Encode(map[string]any{"error": f.errMsg})
			return
		}
		_ = enc.Encode(map[string]any{"model": req.Model, "message": map[string]string{"role": "assistant", "content": word}, "done": false})
		w.(http.Flusher).Flush()
	}
	_ = enc.Encode(map[string]any{"model": req.Model, "message": map[string]string{"role": "assistant", "content": ""},
		"done": true, "done_reason": "stop", "prompt_eval_count": 40, "eval_count": 6})
}

func (f *fakeOllama) requests() []chatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

var enToEs = domain.TranslateRequest{SessionID: "main", SegmentID: "s0", Text: "Hello everyone.", From: "en", To: "es", Final: true,
	Context: []string{"Welcome."}}

func TestTranslate(t *testing.T) {
	tests := []struct {
		name     string
		reply    string
		status   int
		errMsg   string
		want     string
		partials []string
		wantErr  string
	}{
		{name: "streamed", reply: "Hola a todos.", want: "Hola a todos.", partials: []string{"Hola ", "Hola a ", "Hola a todos."}},
		{name: "cleaned", reply: `"Hola."`, want: "Hola.", partials: []string{`"Hola."`}},
		{name: "model missing", status: http.StatusNotFound, errMsg: "model 'gemma3:4b' not found", wantErr: "model 'gemma3:4b' not found (HTTP 404"},
		{name: "error mid-stream", reply: "Hola a todos.", errMsg: "out of memory", wantErr: "ollama: out of memory"},
		{name: "empty reply", reply: " ", wantErr: "empty reply"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeOllama(t, tt.reply)
			f.status, f.errMsg = tt.status, tt.errMsg
			tr := &Translator{URL: f.URL}
			var partials []string
			res, err := tr.TranslateStream(t.Context(), enToEs, func(s string) { partials = append(partials, s) })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != tt.want || res.Usage != (domain.Usage{}) {
				t.Errorf("result = %+v, want text %q and no usage", res, tt.want)
			}
			if !slices.Equal(partials, tt.partials) {
				t.Errorf("partials = %q, want %q", partials, tt.partials)
			}
			reqs := f.requests()
			if len(reqs) != 1 {
				t.Fatalf("requests = %d", len(reqs))
			}
			r := reqs[0]
			if r.Model != DefaultModel || !r.Stream || r.KeepAlive != "30m0s" || r.Think == nil || *r.Think {
				t.Errorf("request = %+v", r)
			}
			if len(r.Messages) != 2 || r.Messages[0].Role != "system" || r.Messages[1].Role != "user" ||
				!strings.Contains(r.Messages[0].Content, "from English to Spanish") ||
				!strings.Contains(r.Messages[1].Content, "Welcome.") || !strings.HasSuffix(r.Messages[1].Content, "Hello everyone.") {
				t.Errorf("messages = %+v", r.Messages)
			}
		})
	}
}

func TestTranslateUnreachable(t *testing.T) {
	f := newFakeOllama(t, "x")
	url := f.URL
	f.Close()
	_, err := (&Translator{URL: url}).Translate(t.Context(), enToEs)
	if err == nil || !strings.Contains(err.Error(), "ollama unreachable at "+url) {
		t.Errorf("err = %v", err)
	}
}

func TestSettings(t *testing.T) {
	f := newFakeOllama(t, "Hola.")
	settings := func(url, model string, err error) func(context.Context) (api.Settings, error) {
		return func(context.Context) (api.Settings, error) {
			var s api.Settings
			s.Providers.Local.OllamaUrl, s.Providers.Local.GemmaModel = url, model
			return s, err
		}
	}
	tests := []struct {
		name      string
		tr        *Translator
		wantModel string
		wantErr   bool
	}{
		{name: "from settings", tr: &Translator{Settings: settings(f.URL, "gemma3:1b", nil)}, wantModel: "gemma3:1b"},
		{name: "empty settings use the fields", tr: &Translator{URL: f.URL, Model: "m", Settings: settings("", "", nil)}, wantModel: "m"},
		{name: "not saved yet", tr: &Translator{URL: f.URL, Settings: settings("", "", domain.ErrNotFound)}, wantModel: DefaultModel},
		{name: "read error", tr: &Translator{URL: f.URL, Settings: settings("", "", errors.New("disk"))}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := len(f.requests())
			_, err := tt.tr.Translate(t.Context(), enToEs)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := f.requests()[n].Model; got != tt.wantModel {
				t.Errorf("model = %q, want %q", got, tt.wantModel)
			}
		})
	}
}

func TestResolveCache(t *testing.T) {
	now := time.Unix(0, 0)
	var reads int
	tr := &Translator{
		Now: func() time.Time { return now },
		Settings: func(context.Context) (api.Settings, error) {
			reads++
			return api.Settings{}, nil
		},
	}
	for range 3 {
		if _, _, err := tr.resolve(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(configTTL + time.Second)
	url, model, _ := tr.resolve(t.Context())
	if reads != 2 || url != DefaultURL || model != DefaultModel {
		t.Errorf("reads = %d, url = %q, model = %q; want 2, defaults", reads, url, model)
	}
}

func TestWarm(t *testing.T) {
	f := newFakeOllama(t, "")
	tr := &Translator{URL: f.URL, Model: "gemma3:1b", KeepAlive: time.Hour}
	if err := tr.Warm(t.Context()); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 1 || len(reqs[0].Messages) != 0 || reqs[0].Stream || reqs[0].KeepAlive != "1h0m0s" || reqs[0].Model != "gemma3:1b" {
		t.Errorf("warm-up requests = %+v", reqs)
	}

	f.status, f.errMsg = http.StatusNotFound, "model not found"
	if err := tr.Warm(t.Context()); err == nil {
		t.Error("warm-up of a missing model: want an error")
	}
}

func TestConcurrencyLimit(t *testing.T) {
	tests := []struct {
		limit, calls int
		want         int32
	}{
		{limit: 1, calls: 4, want: 1},
		{limit: 2, calls: 4, want: 2},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("limit=%d", tt.limit), func(t *testing.T) {
			f := newFakeOllama(t, "Hola.")
			f.hold = make(chan struct{})
			tr := &Translator{URL: f.URL, Concurrency: tt.limit}
			var wg sync.WaitGroup
			for range tt.calls {
				wg.Go(func() {
					if _, err := tr.Translate(t.Context(), enToEs); err != nil {
						t.Error(err)
					}
				})
			}
			// Let the allowed requests arrive, then release them one by one.
			deadline := time.After(5 * time.Second)
			for f.inflight.Load() < tt.want {
				select {
				case <-deadline:
					t.Fatalf("in flight = %d, want %d", f.inflight.Load(), tt.want)
				case <-time.After(time.Millisecond):
				}
			}
			time.Sleep(20 * time.Millisecond) // a request over the limit would arrive now
			for range tt.calls {
				f.hold <- struct{}{}
			}
			wg.Wait()
			if got := f.peak.Load(); got != tt.want {
				t.Errorf("peak in flight = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCancelWhileWaiting(t *testing.T) {
	f := newFakeOllama(t, "Hola.")
	f.hold = make(chan struct{})
	tr := &Translator{URL: f.URL, Concurrency: 1}
	go func() { _, _ = tr.Translate(t.Context(), enToEs) }() // takes the only slot
	deadline := time.After(5 * time.Second)
	for f.inflight.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("first request didn't arrive")
		case <-time.After(time.Millisecond):
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := tr.Translate(ctx, enToEs); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
	close(f.hold)
}
