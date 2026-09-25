// SPDX-License-Identifier: Apache-2.0

package streamcc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

// cid stands for the secret part of a YouTube ingestion URL.
const cid = "SECRET-cid-9f8e7d6c"

// fakeYouTube is a caption ingestion endpoint. statuses[n] answers the
// n-th request (0 or past the end: 200 with the server time).
type fakeYouTube struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	statuses []int
	posts    []ytPost
	reply    func() string // body of a 200; default: now on the server clock
	hold     chan struct{} // when set, requests wait for it to close
}

type ytPost struct {
	seq  int64
	cid  string
	body string
}

func newFakeYouTube(t *testing.T) *fakeYouTube {
	f := &fakeYouTube{t: t}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeYouTube) url() string { return f.srv.URL + "/closedcaption?cid=" + cid }

func (f *fakeYouTube) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	hold := f.hold
	f.mu.Unlock()
	if hold != nil {
		<-hold
	}
	seq, err := strconv.ParseInt(r.URL.Query().Get("seq"), 10, 64)
	if err != nil || r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/plain") {
		f.t.Errorf("bad request: %s %s %q", r.Method, r.URL.RawQuery, r.Header.Get("Content-Type"))
	}
	f.mu.Lock()
	n := len(f.posts)
	f.posts = append(f.posts, ytPost{seq: seq, cid: r.URL.Query().Get("cid"), body: string(body)})
	status := 200
	if n < len(f.statuses) && f.statuses[n] != 0 {
		status = f.statuses[n]
	}
	reply := f.reply
	f.mu.Unlock()
	if status != 200 {
		http.Error(w, "nope", status)
		return
	}
	if reply != nil {
		_, _ = io.WriteString(w, reply())
		return
	}
	_, _ = io.WriteString(w, time.Now().UTC().Format(youtubeTimeLayout)+"\n")
}

func (f *fakeYouTube) got() []ytPost {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ytPost(nil), f.posts...)
}

func (f *fakeYouTube) set(fn func(f *fakeYouTube)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// memSecrets is an in-memory domain.SecretStore.
type memSecrets struct {
	mu     sync.Mutex
	values map[string]string
	env    map[string]string
}

func (m *memSecrets) GetSecret(_ context.Context, name string) (string, domain.SecretSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.env[name]; ok {
		return v, domain.SecretFromEnv, nil
	}
	if v, ok := m.values[name]; ok {
		return v, domain.SecretFromKeychain, nil
	}
	return "", "", secrets.ErrNotFound
}

func (m *memSecrets) SetSecret(_ context.Context, name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.env[name]; ok {
		return secrets.ErrReadOnly
	}
	m.values[name] = value
	return nil
}

func (m *memSecrets) DeleteSecret(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.values[name]; !ok {
		return secrets.ErrNotFound
	}
	delete(m.values, name)
	return nil
}

func (m *memSecrets) SecretInfo(ctx context.Context, name string) (domain.SecretInfo, error) {
	info := domain.SecretInfo{Name: api.SecretName(name)}
	if v, _, err := m.GetSecret(ctx, name); err == nil {
		hint := secrets.Hint(v)
		info.Set, info.Hint = true, &hint
	}
	return info, nil
}

// fakeClock is a settable local clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// recBus records what goes through the tap to the real bus.
type recBus struct {
	mu   sync.Mutex
	msgs []domain.BusMessage
}

func (b *recBus) Publish(_ string, msg domain.BusMessage) {
	b.mu.Lock()
	b.msgs = append(b.msgs, msg)
	b.mu.Unlock()
}

func (b *recBus) Subscribe(context.Context, string, []string) <-chan domain.BusMessage { return nil }
func (b *recBus) Viewers(string) int                                                   { return 0 }

// eventLog records admin events.
type eventLog struct {
	mu  sync.Mutex
	evs []api.AdminEvent
}

func (e *eventLog) publish(ev api.AdminEvent) {
	e.mu.Lock()
	e.evs = append(e.evs, ev)
	e.mu.Unlock()
}

// states are the stream-caption states in the events, in order.
func (e *eventLog) states() []api.StreamCaptionStatusState {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []api.StreamCaptionStatusState
	for _, ev := range e.evs {
		if ev.Type == api.AdminEventTypeStreamCaptionStatus && ev.Status != nil && ev.Status.StreamCaptions != nil {
			out = append(out, ev.Status.StreamCaptions.State)
		}
	}
	return out
}

func (e *eventLog) logCodes() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.evs {
		if ev.Type == api.AdminEventTypeLog && ev.Log != nil {
			out = append(out, ev.Log.Code)
		}
	}
	return out
}

type harness struct {
	svc    *Service
	bus    domain.CaptionBus
	inner  *recBus
	yt     *fakeYouTube
	sec    *memSecrets
	clock  *fakeClock
	events *eventLog
	logs   *bytes.Buffer
}

var t0 = time.Date(2026, 9, 25, 13, 30, 0, 0, time.UTC)

func newHarness(t *testing.T, withURL bool, opts Options) *harness {
	t.Helper()
	h := &harness{yt: newFakeYouTube(t), sec: &memSecrets{values: map[string]string{}}, clock: &fakeClock{t: t0},
		inner: &recBus{}, events: &eventLog{}, logs: &bytes.Buffer{}}
	if withURL {
		h.sec.values[secretName("main")] = h.yt.url()
	}
	opts.Secrets, opts.Clock = h.sec, h.clock
	opts.Logger = slog.New(slog.NewTextHandler(&lockedWriter{w: h.logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if opts.MinBackoff == 0 {
		opts.MinBackoff, opts.MaxBackoff = 5*time.Millisecond, 20*time.Millisecond
	}
	h.svc = New(opts)
	// YouTube's clock matches ours unless a test says otherwise.
	h.yt.reply = func() string { return h.clock.Now().Format(youtubeTimeLayout) + "\n" }
	h.svc.Bind(h.events.publish, func(id string) api.SessionStatus {
		return api.SessionStatus{SessionId: id, State: api.SessionStateLive}
	})
	h.bus = h.svc.Tap(h.inner)
	t.Cleanup(h.svc.Close)
	return h
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (h *harness) logText() string {
	// The handler writes under lockedWriter's lock; reading after the
	// service settled is enough for tests.
	return h.logs.String()
}

func enabledSession(track string) domain.Session {
	on := true
	return domain.Session{Id: "main", StreamCaptions: &api.StreamCaptionsConfig{Enabled: &on, Track: &track}}
}

func (h *harness) final(track, seg, text string) {
	h.bus.Publish("main", domain.BusMessage{Type: api.CaptionsServerMessageTypeCaption, Caption: &api.Caption{
		SessionId: "main", Lang: track, SegmentId: seg, Final: true, Text: text, Start: 1, End: 2, SourceLang: "en"}})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (h *harness) waitPosts(t *testing.T, n int) []ytPost {
	t.Helper()
	waitFor(t, fmt.Sprintf("%d posts", n), func() bool { return len(h.yt.got()) >= n })
	return h.yt.got()
}

func (h *harness) waitState(t *testing.T, want api.StreamCaptionStatusState) api.StreamCaptionStatus {
	t.Helper()
	var st *api.StreamCaptionStatus
	waitFor(t, "state "+string(want), func() bool {
		st = h.svc.Status("main")
		return st != nil && st.State == want
	})
	return *st
}

func TestSinkSendsFinalsOfItsTrack(t *testing.T) {
	h := newHarness(t, true, Options{})
	h.svc.RunStarted(enabledSession("en"))

	h.bus.Publish("main", domain.BusMessage{Type: api.CaptionsServerMessageTypeCaption, Caption: &api.Caption{
		SessionId: "main", Lang: "en", SegmentId: "s-0", Final: false, Text: "Hel"}})
	h.final("es", "s-1", "Hola a todos.")
	h.final("en", "s-1", "Hello everyone.")
	h.final("en", "s-1", "Hello, everyone.") // a correction: already on the stream
	h.final("en", "s-2", "Today we talk about observability in Kubernetes clusters, and more.")
	posts := h.waitPosts(t, 2)

	h.svc.RunEnded("main")
	h.waitState(t, api.StreamCaptionStatusStateIdle)
	if got := h.yt.got(); len(got) != 2 {
		t.Fatalf("got %d posts, want 2: %+v", len(got), got)
	}
	for i, p := range posts {
		if p.seq != int64(i+1) || p.cid != cid {
			t.Errorf("post %d: seq %d cid %q, want seq %d and the URL's cid", i, p.seq, p.cid, i+1)
		}
	}
	// Speech from 1 s to 2 s of audio ended when it arrived (no latency).
	if want := t0.Add(-time.Second).Format(youtubeTimeLayout) + "\nHello everyone.\n"; posts[0].body != want {
		t.Errorf("first body = %q, want %q", posts[0].body, want)
	}
	lines := strings.Split(strings.TrimSpace(posts[1].body), "\n")
	if len(lines) != 4 || lines[1] != "Today we talk about<br>observability in Kubernetes" || lines[3] != "clusters, and more." {
		t.Errorf("second body = %q, want two cues of two lines", posts[1].body)
	}
	if n := len(h.inner.msgs); n != 5 {
		t.Errorf("the real bus got %d messages, want all 5", n)
	}
	st := h.svc.Status("main")
	if st.LastSeq == nil || *st.LastSeq != 2 || st.LastSentAt == nil || st.Target == nil || *st.Target != api.YoutubeHttp {
		t.Errorf("status = %+v, want last seq 2, a last-sent time and target youtube_http", st)
	}
	if states := h.events.states(); len(states) == 0 || states[len(states)-1] != api.StreamCaptionStatusStateIdle {
		t.Errorf("event states = %v, want ending in idle", states)
	}
}

func TestClockOffsetFromYouTubeReply(t *testing.T) {
	h := newHarness(t, true, Options{})
	// YouTube's clock is 2 s behind ours.
	h.yt.set(func(f *fakeYouTube) {
		f.reply = func() string { return h.clock.Now().Add(-2*time.Second).Format(youtubeTimeLayout) + "\n" }
	})
	h.svc.RunStarted(enabledSession("en"))
	h.final("en", "s-1", "one")
	h.waitPosts(t, 1)
	st := h.waitState(t, api.StreamCaptionStatusStateOk)
	waitFor(t, "clock offset", func() bool {
		st = *h.svc.Status("main")
		return st.ClockOffsetMs != nil
	})
	if *st.ClockOffsetMs != 2000 {
		t.Fatalf("clock offset = %d ms, want 2000", *st.ClockOffsetMs)
	}
	h.final("en", "s-2", "two")
	posts := h.waitPosts(t, 2)
	if want := t0.Add(-3*time.Second).Format(youtubeTimeLayout) + "\ntwo\n"; posts[1].body != want {
		t.Errorf("body after the offset = %q, want %q (on YouTube's clock)", posts[1].body, want)
	}
}

func TestSinkRetries(t *testing.T) {
	tests := []struct {
		name       string
		statuses   []int
		wantPosts  int
		wantSeqs   []int64
		wantState  api.StreamCaptionStatusState
		wantCode   string
		wantStates api.StreamCaptionStatusState // seen on the way
	}{
		{"server errors pass", []int{503, 500, 0, 0}, 4, []int64{1, 1, 1, 2}, api.StreamCaptionStatusStateOk, "", api.StreamCaptionStatusStateRetrying},
		{"rate limited", []int{429, 0, 0}, 3, []int64{1, 1, 2}, api.StreamCaptionStatusStateOk, "", api.StreamCaptionStatusStateRetrying},
		{"rejected is not retried", []int{403, 0}, 2, []int64{1, 2}, api.StreamCaptionStatusStateOk, "", api.StreamCaptionStatusStateError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, true, Options{})
			h.yt.set(func(f *fakeYouTube) { f.statuses = tt.statuses })
			h.svc.RunStarted(enabledSession("en"))
			h.final("en", "s-1", "first")
			waitFor(t, "first caption settled", func() bool {
				st := h.svc.Status("main")
				return st != nil && (st.State == api.StreamCaptionStatusStateOk || st.State == api.StreamCaptionStatusStateError)
			})
			h.final("en", "s-2", "second")
			posts := h.waitPosts(t, tt.wantPosts)
			h.waitState(t, tt.wantState)
			for i, p := range posts {
				if p.seq != tt.wantSeqs[i] {
					t.Errorf("post %d seq = %d, want %d", i, p.seq, tt.wantSeqs[i])
				}
			}
			found := false
			for _, s := range h.events.states() {
				found = found || s == tt.wantStates
			}
			if !found {
				t.Errorf("states %v never went through %s", h.events.states(), tt.wantStates)
			}
		})
	}
}

func TestSinkReportsRejection(t *testing.T) {
	h := newHarness(t, true, Options{})
	h.yt.set(func(f *fakeYouTube) { f.statuses = []int{400} })
	h.svc.RunStarted(enabledSession("en"))
	h.final("en", "s-1", "first")
	st := h.waitState(t, api.StreamCaptionStatusStateError)
	if st.Error == nil || st.Error.Code != CodeRejected || st.Error.Params == nil || (*st.Error.Params)["status"] != 400 {
		t.Fatalf("error = %+v, want %s with status 400", st.Error, CodeRejected)
	}
}

func TestStaleCaptionsAreNotReplayed(t *testing.T) {
	h := newHarness(t, true, Options{MaxAge: 30 * time.Second})
	outage := make([]int, 1000)
	for i := range outage {
		outage[i] = 503
	}
	h.yt.set(func(f *fakeYouTube) { f.statuses = outage })
	h.svc.RunStarted(enabledSession("en"))
	h.final("en", "s-1", "during the outage")
	h.final("en", "s-2", "also during the outage")
	h.waitState(t, api.StreamCaptionStatusStateRetrying)

	// A long outage: both captions are now older than MaxAge.
	h.clock.Advance(time.Minute)
	waitFor(t, "stale captions dropped", func() bool {
		for _, c := range h.events.logCodes() {
			if c == CodeDropped {
				return true
			}
		}
		return false
	})
	h.yt.set(func(f *fakeYouTube) { f.statuses = nil; f.posts = nil })
	h.final("en", "s-3", "after the outage")
	posts := h.waitPosts(t, 1)
	h.waitState(t, api.StreamCaptionStatusStateOk)
	if len(posts) != 1 || !strings.HasSuffix(posts[0].body, "\nafter the outage\n") {
		t.Fatalf("posts after the outage = %+v, want only the fresh caption", posts)
	}
}

func TestQueueIsBounded(t *testing.T) {
	h := newHarness(t, true, Options{QueueSize: 2})
	hold := make(chan struct{})
	h.yt.set(func(f *fakeYouTube) { f.hold = hold })
	h.svc.RunStarted(enabledSession("en"))
	h.final("en", "s-1", "in flight")
	waitFor(t, "first post in flight", func() bool {
		h.svc.mu.Lock()
		defer h.svc.mu.Unlock()
		k := h.svc.sessions["main"].sink
		k.mu.Lock()
		defer k.mu.Unlock()
		return len(k.queue) == 0
	})
	for i := 2; i <= 5; i++ {
		h.final("en", fmt.Sprintf("s-%d", i), fmt.Sprintf("caption %d", i))
	}
	close(hold)
	posts := h.waitPosts(t, 3)
	h.waitState(t, api.StreamCaptionStatusStateOk)
	var texts []string
	for _, p := range h.yt.got() {
		texts = append(texts, strings.Split(strings.TrimSpace(p.body), "\n")[1])
	}
	if len(posts) != 3 || texts[0] != "in flight" || texts[1] != "caption 4" || texts[2] != "caption 5" {
		t.Fatalf("delivered %q, want the in-flight caption and the newest two", texts)
	}
}

func TestRunEndedDrainsTheQueue(t *testing.T) {
	h := newHarness(t, true, Options{})
	hold := make(chan struct{})
	h.yt.set(func(f *fakeYouTube) { f.hold = hold })
	h.svc.RunStarted(enabledSession("source"))
	for i := 1; i <= 3; i++ {
		h.final("source", fmt.Sprintf("s-%d", i), fmt.Sprintf("caption %d", i))
	}
	h.svc.RunEnded("main")
	h.final("source", "s-4", "after the end") // not sent
	close(hold)
	h.waitPosts(t, 3)
	h.svc.wg.Wait() // the sink has stopped
	if st := h.svc.Status("main"); st.State != api.StreamCaptionStatusStateIdle {
		t.Errorf("state after the drain = %s, want idle", st.State)
	}
	if got := h.yt.got(); len(got) != 3 {
		t.Fatalf("got %d posts after the run ended, want the 3 queued", len(got))
	}
}

func TestNoURLUntilSet(t *testing.T) {
	h := newHarness(t, false, Options{})
	h.svc.RunStarted(enabledSession("en"))
	st := h.svc.Status("main")
	if st == nil || st.State != api.StreamCaptionStatusStateError || st.Error == nil || st.Error.Code != CodeNoURL {
		t.Fatalf("status without a URL = %+v, want error %s", st, CodeNoURL)
	}
	if _, err := h.svc.SetYouTubeURL(t.Context(), "main", "  "+h.yt.url()+"\n"); err != nil {
		t.Fatal(err)
	}
	h.final("en", "s-1", "now it works")
	h.waitPosts(t, 1)
	h.waitState(t, api.StreamCaptionStatusStateOk)
	if err := h.svc.DeleteYouTubeURL(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	h.final("en", "s-2", "no URL again")
	st2 := h.waitState(t, api.StreamCaptionStatusStateError)
	if st2.Error.Code != CodeNoURL || len(h.yt.got()) != 1 {
		t.Fatalf("after removing the URL: status %+v, %d posts; want %s and no new post", st2, len(h.yt.got()), CodeNoURL)
	}
}

func TestDisabledSessionSendsNothing(t *testing.T) {
	off := false
	tests := []struct {
		name string
		cfg  *api.StreamCaptionsConfig
	}{
		{"no config", nil},
		{"disabled", &api.StreamCaptionsConfig{Enabled: &off}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, true, Options{})
			h.svc.RunStarted(domain.Session{Id: "main", StreamCaptions: tt.cfg})
			h.final("en", "s-1", "hello")
			h.svc.RunEnded("main")
			if st := h.svc.Status("main"); st == nil || st.State != api.StreamCaptionStatusStateDisabled {
				t.Fatalf("status = %+v, want disabled", st)
			}
			time.Sleep(20 * time.Millisecond)
			if n := len(h.yt.got()); n != 0 {
				t.Fatalf("a disabled session posted %d captions", n)
			}
		})
	}
}

func TestSendTest(t *testing.T) {
	tests := []struct {
		name      string
		withURL   bool
		closed    bool // the endpoint is down
		statuses  []int
		text      string
		wantErr   error
		wantState api.StreamCaptionStatusState
		wantCode  string
		wantBody  string
	}{
		{"no URL", false, false, nil, "", ErrNoURL, "", "", ""},
		{"default text", true, false, nil, "", nil, api.StreamCaptionStatusStateOk, "", "\nLive Subtitles: test caption /<br>subtítulo de prueba\n"},
		{"custom text", true, false, nil, "Probando 1 2 3", nil, api.StreamCaptionStatusStateOk, "", "\nProbando 1 2 3\n"},
		{"server error", true, false, []int{500}, "x", nil, api.StreamCaptionStatusStateError, CodeServerError, ""},
		{"unreachable", true, true, nil, "x", nil, api.StreamCaptionStatusStateError, CodeUnreachable, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.withURL, Options{})
			h.yt.set(func(f *fakeYouTube) { f.statuses = tt.statuses })
			if tt.closed {
				h.yt.srv.Close()
			}
			st, err := h.svc.SendTest(t.Context(), domain.Session{Id: "main"}, tt.text)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if st.State != tt.wantState || (tt.wantCode != "" && (st.Error == nil || st.Error.Code != tt.wantCode)) {
				t.Fatalf("status = %+v (error %+v), want %s %s", st, st.Error, tt.wantState, tt.wantCode)
			}
			if st.Error != nil && strings.Contains(st.Error.Message, cid) {
				t.Errorf("error message %q reveals the URL", st.Error.Message)
			}
			if tt.wantBody != "" {
				posts := h.yt.got()
				if len(posts) != 1 || !strings.HasSuffix(posts[0].body, tt.wantBody) || posts[0].seq != 1 {
					t.Errorf("posts = %+v, want one with seq 1 ending in %q", posts, tt.wantBody)
				}
			}
			if strings.Contains(h.logText(), cid) {
				t.Errorf("the log reveals the URL:\n%s", h.logText())
			}
		})
	}
}

func TestURLNeverLogged(t *testing.T) {
	h := newHarness(t, true, Options{})
	h.yt.srv.Close() // every delivery fails with a transport error
	h.svc.RunStarted(enabledSession("en"))
	h.final("en", "s-1", "hello")
	st := h.waitState(t, api.StreamCaptionStatusStateRetrying)
	h.svc.RunEnded("main")
	h.svc.Close()
	if strings.Contains(h.logText(), cid) || strings.Contains(st.Error.Message, cid) {
		t.Fatalf("the ingestion URL leaked:\nlog: %s\nstatus: %s", h.logText(), st.Error.Message)
	}
}

func TestURLSecret(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		env     bool
		wantErr error
	}{
		{"youtube URL", "http://upload.youtube.com/closedcaption?cid=abcd-efgh-1234", false, nil},
		{"https", "https://example.com/cc?x=1", false, nil},
		{"not a URL", "abcd-efgh", false, ErrInvalidURL},
		{"other scheme", "ftp://upload.youtube.com/closedcaption", false, ErrInvalidURL},
		{"no host", "http:///closedcaption", false, ErrInvalidURL},
		{"set by the environment", "http://upload.youtube.com/closedcaption?cid=x", true, secrets.ErrReadOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sec := &memSecrets{values: map[string]string{}}
			if tt.env {
				sec.env = map[string]string{secretName("main"): "http://env.example/cc"}
			}
			red := secrets.NewRedactor()
			svc := New(Options{Secrets: sec, Redactor: red, Logger: slog.New(slog.DiscardHandler)})
			info, err := svc.SetYouTubeURL(t.Context(), "main", tt.url)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if !tt.env && svc.YouTubeURLSet(t.Context(), "main") {
					t.Error("a rejected URL was stored")
				}
				return
			}
			if info.Name != api.YoutubeCaptionUrl || !info.Set || info.Hint == nil || strings.Contains(*info.Hint, "youtube") {
				t.Errorf("info = %+v, want a masked youtube_caption_url", info)
			}
			if got := red.Redact("posting to " + tt.url); strings.Contains(got, tt.url) {
				t.Errorf("the redactor doesn't know the URL: %q", got)
			}
			if !svc.YouTubeURLSet(t.Context(), "main") {
				t.Error("YouTubeURLSet = false after set")
			}
			svc.Forget(t.Context(), "main")
			if svc.YouTubeURLSet(t.Context(), "main") {
				t.Error("Forget kept the URL")
			}
			if err := svc.DeleteYouTubeURL(t.Context(), "main"); !errors.Is(err, secrets.ErrNotFound) {
				t.Errorf("delete after forget: %v, want not found", err)
			}
		})
	}
}
