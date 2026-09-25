// SPDX-License-Identifier: Apache-2.0

package loadtest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Config is one load test run against a server.
type Config struct {
	BaseURL  string        // http://host:port of the server
	Token    string        // admin bearer token (LIVESUBS_ADMIN_TOKEN)
	Sessions int           // sessions to create, each with a file source
	Viewers  int           // /ws/captions viewers per session
	Duration time.Duration // how long to measure, after the warm-up
	// Warmup is how long the sources play before measuring starts, so the
	// burst ffmpeg sends first doesn't count (default 3 s; negative: none).
	Warmup time.Duration
	// FileURI is the audio the file source plays in a loop, as the server
	// resolves it (a path under its data dir or ./testdata).
	FileURI string
	// Langs are the tracks each viewer subscribes to (default source, es).
	Langs []string
	// PID is the server process to sample for CPU and RSS; 0 skips it.
	PID int
	// DialConcurrency bounds viewers connecting at once (default 100).
	DialConcurrency int
	// Log gets progress lines; nil discards them.
	Log io.Writer
}

// Report is what a run measured.
type Report struct {
	Sessions int           `json:"sessions"`
	Viewers  int           `json:"viewersPerSession"`
	Measured time.Duration `json:"measuredNs"`

	Connected    int   `json:"viewersConnected"`
	DialFailures int64 `json:"dialFailures"`
	// Drops are viewers the server closed with 1013 because they fell
	// behind (the bus's slow-consumer rule); Disconnects are other errors.
	Drops       int64 `json:"drops"`
	Disconnects int64 `json:"disconnects"`
	// ServerViewers is livesubs_ws_clients{endpoint="captions"} just
	// before the viewers left; -1 when /metrics wasn't readable.
	ServerViewers float64 `json:"serverViewers"`

	Messages        int64   `json:"messages"`
	Bytes           int64   `json:"bytes"`
	MessagesPerSec  float64 `json:"messagesPerSec"`
	BytesPerSec     float64 `json:"bytesPerSec"`
	EventsPerSecond float64 `json:"captionEventsPerSessionPerSec"`

	// Lag is how much later than the session's fastest final caption a
	// viewer read a final, both measured against the audio clock (the
	// session's startedAt plus the caption's end). With a real-time source
	// and an idle server it is only jitter; queueing anywhere between the
	// provider and the viewer's socket shows up here. (The absolute audio
	// to viewer time can't be read off the wire: ffmpeg sends the first
	// half second of a file in a burst, and the mock provider emits each
	// word when it starts, 300 ms early, then delays it by 300 ms.)
	Lag Summary `json:"lagMs"`
	// Fanout is how long after the session's first viewer each viewer
	// read the same caption event (interims and finals).
	Fanout Summary `json:"fanoutMs"`
	// Pipeline is the caption's own latencyMs (audio reaching the server
	// to the caption being emitted), for finals.
	Pipeline Summary `json:"pipelineMs"`

	Proc *ProcReport `json:"proc,omitempty"`
}

// ProcReport is the server process's resource use.
type ProcReport struct {
	BaselineRSS int64   `json:"baselineRssBytes"` // before any session
	PeakRSS     int64   `json:"peakRssBytes"`
	CPUPercent  float64 `json:"cpuPercent"` // of one core, while measuring
	// Children are the ffmpeg processes of the file sources.
	Children        int     `json:"children"`
	ChildCPUPercent float64 `json:"childCpuPercent"`
	ChildRSS        int64   `json:"childRssBytes"`
}

// sample is one caption event read by a viewer.
type sample struct {
	recv   int64 // unix ns
	key    uint64
	end    float64 // seconds on the session clock
	pipeMs int32   // latencyMs, -1 when absent
	final  bool
}

type viewer struct {
	samples []sample
}

type sessionRun struct {
	id      string
	origin  atomic.Int64 // startedAt, unix ns
	viewers []*viewer
}

type runner struct {
	cfg    Config
	http   *http.Client
	logf   func(format string, args ...any)
	start  atomic.Int64 // unix ns when measuring started; samples before are ignored
	stop   atomic.Int64 // unix ns when measuring ended
	msgs   atomic.Int64
	bytes  atomic.Int64
	dialKO atomic.Int64
	drops  atomic.Int64
	disc   atomic.Int64
	conns  atomic.Int64
}

// Run creates the sessions, connects the viewers, plays the file source
// on every session for cfg.Duration, then stops and deletes the sessions.
func Run(ctx context.Context, cfg Config) (Report, error) {
	if cfg.Sessions < 1 || cfg.Viewers < 0 || cfg.Duration <= 0 {
		return Report{}, errors.New("loadtest: need sessions ≥ 1, viewers ≥ 0 and a duration")
	}
	if len(cfg.Langs) == 0 {
		cfg.Langs = []string{"source", "es"}
	}
	if cfg.Warmup == 0 {
		cfg.Warmup = 3 * time.Second
	}
	if cfg.DialConcurrency <= 0 {
		cfg.DialConcurrency = 100
	}
	logw := cfg.Log
	if logw == nil {
		logw = io.Discard
	}
	r := &runner{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second},
		logf: func(f string, a ...any) { _, _ = fmt.Fprintf(logw, f+"\n", a...) }}
	rep := Report{Sessions: cfg.Sessions, Viewers: cfg.Viewers, ServerViewers: -1}

	var baseline ProcSample
	var haveProc bool
	if cfg.PID > 0 {
		var err error
		if baseline, err = SampleProc(ctx, cfg.PID); err != nil {
			r.logf("CPU/RSS sampling off: %v", err)
		} else {
			haveProc = true
		}
	}

	// Sessions, cleaned up whatever happens next.
	prefix := "lt-" + randHex(3)
	sessions := make([]*sessionRun, cfg.Sessions)
	var created []string
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		for _, id := range created {
			_ = r.admin(cctx, "POST", "/api/sessions/"+id+"/stop", nil, nil)
			if err := r.admin(cctx, "DELETE", "/api/sessions/"+id, nil, nil); err != nil {
				r.logf("delete %s: %v", id, err)
			}
		}
	}()
	for i := range sessions {
		id := fmt.Sprintf("%s-%d", prefix, i+1)
		body := map[string]any{"slug": id, "name": "Load test " + strconv.Itoa(i+1), "provider": "mock",
			"sourceLanguage": "en", "targetLanguages": []string{"es"}, "recordingEnabled": false}
		if err := r.admin(ctx, "POST", "/api/sessions", body, nil); err != nil {
			return rep, fmt.Errorf("create session %s: %w", id, err)
		}
		created = append(created, id)
		sessions[i] = &sessionRun{id: id}
	}
	r.logf("created %d sessions (%s-*)", len(sessions), prefix)

	// Viewers, all connected before any audio plays.
	vctx, stopViewers := context.WithCancel(ctx)
	defer stopViewers()
	var dialed, running sync.WaitGroup
	sem := make(chan struct{}, cfg.DialConcurrency)
	for _, s := range sessions {
		s.viewers = make([]*viewer, cfg.Viewers)
		for j := range s.viewers {
			v := &viewer{}
			s.viewers[j] = v
			dialed.Add(1)
			running.Go(func() { r.view(vctx, s, v, sem, &dialed) })
		}
	}
	dialed.Wait()
	rep.Connected = int(r.conns.Load())
	r.logf("%d/%d viewers connected (%d failed)", rep.Connected, cfg.Sessions*cfg.Viewers, r.dialKO.Load())
	if ctx.Err() != nil {
		return rep, ctx.Err()
	}

	// Sources.
	for _, s := range sessions {
		var st struct {
			StartedAt *time.Time `json:"startedAt"`
		}
		if err := r.admin(ctx, "POST", "/api/sessions/"+s.id+"/sources/file",
			map[string]any{"uri": cfg.FileURI, "loop": true}, &st); err != nil {
			return rep, fmt.Errorf("start file source on %s: %w", s.id, err)
		}
		at := time.Now()
		if st.StartedAt != nil {
			at = *st.StartedAt
		}
		s.origin.Store(at.UnixNano())
	}
	r.logf("sources started; warming up for %s, then measuring for %s", max(cfg.Warmup, 0), cfg.Duration)
	if !sleep(ctx, max(cfg.Warmup, 0)) {
		return rep, ctx.Err()
	}
	r.start.Store(time.Now().UnixNano())

	// Sample the server while the sources play.
	var peakRSS atomic.Int64
	peakRSS.Store(baseline.RSS)
	var first ProcSample
	sctx, stopSampling := context.WithCancel(ctx)
	var sampling sync.WaitGroup
	if haveProc {
		first, _ = SampleProc(ctx, cfg.PID)
		sampling.Go(func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-sctx.Done():
					return
				case <-t.C:
					if s, err := SampleProc(sctx, cfg.PID); err == nil && s.RSS > peakRSS.Load() {
						peakRSS.Store(s.RSS)
					}
				}
			}
		})
	}
	progress := time.NewTicker(5 * time.Second)
	deadline := time.NewTimer(cfg.Duration)
wait:
	for {
		select {
		case <-ctx.Done():
			break wait
		case <-deadline.C:
			break wait
		case <-progress.C:
			el := time.Since(time.Unix(0, r.start.Load())).Seconds()
			r.logf("  %3.0fs  %d msgs (%.0f/s)  drops %d  disconnects %d", el, r.msgs.Load(),
				float64(r.msgs.Load())/el, r.drops.Load(), r.disc.Load())
		}
	}
	progress.Stop()
	end := time.Now()
	r.stop.Store(end.UnixNano())
	rep.Measured = end.Sub(time.Unix(0, r.start.Load()))
	stopSampling()
	sampling.Wait()
	if haveProc {
		if last, err := SampleProc(context.WithoutCancel(ctx), cfg.PID); err == nil {
			rep.Proc = &ProcReport{
				BaselineRSS:     baseline.RSS,
				PeakRSS:         max(peakRSS.Load(), last.RSS),
				CPUPercent:      CPUPercent(first, last, false),
				Children:        last.Children,
				ChildCPUPercent: CPUPercent(first, last, true),
				ChildRSS:        last.ChildRSS,
			}
		}
	}
	if n, err := r.captionClients(context.WithoutCancel(ctx)); err == nil {
		rep.ServerViewers = n
	}
	stopViewers()
	running.Wait()

	r.summarize(&rep, sessions)
	return rep, ctx.Err()
}

// view keeps one viewer connected until ctx ends, reconnecting after a
// drop, and records every caption event it reads.
func (r *runner) view(ctx context.Context, s *sessionRun, v *viewer, sem chan struct{}, dialed *sync.WaitGroup) {
	q := url.Values{"lang": r.cfg.Langs}
	u := "ws" + strings.TrimPrefix(r.cfg.BaseURL, "http") + "/ws/captions/" + s.id + "?" + q.Encode()
	once := sync.OnceFunc(dialed.Done)
	defer once()
	for ctx.Err() == nil {
		sem <- struct{}{}
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		c, _, err := websocket.Dial(dctx, u, nil)
		cancel()
		<-sem
		if err != nil {
			if ctx.Err() == nil {
				r.dialKO.Add(1)
			}
			once()
			if !sleep(ctx, time.Second) {
				return
			}
			continue
		}
		c.SetReadLimit(4 << 20)
		r.conns.Add(1)
		once()
		err = r.read(ctx, c, s, v)
		_ = c.CloseNow()
		if ctx.Err() != nil {
			return
		}
		if websocket.CloseStatus(err) == websocket.StatusTryAgainLater {
			r.drops.Add(1)
		} else {
			r.disc.Add(1)
		}
		if !sleep(ctx, 250*time.Millisecond) {
			return
		}
	}
}

type wireMessage struct {
	Type    string `json:"type"`
	Caption *struct {
		Lang      string  `json:"lang"`
		SegmentID string  `json:"segmentId"`
		Final     bool    `json:"final"`
		Text      string  `json:"text"`
		End       float64 `json:"end"`
		LatencyMs *int    `json:"latencyMs"`
	} `json:"caption"`
}

func (r *runner) read(ctx context.Context, c *websocket.Conn, s *sessionRun, v *viewer) error {
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return err
		}
		now := time.Now().UnixNano()
		start, stop := r.start.Load(), r.stop.Load()
		if start == 0 || now < start || (stop != 0 && now > stop) {
			continue
		}
		r.msgs.Add(1)
		r.bytes.Add(int64(len(b)))
		var m wireMessage
		if json.Unmarshal(b, &m) != nil || m.Type != "caption" || m.Caption == nil {
			continue
		}
		cp := m.Caption
		h := fnv.New64a()
		// Two interims can carry the same text (a translation that didn't
		// grow), so the key has the end time too.
		for _, part := range []string{cp.Lang, cp.SegmentID, strconv.FormatBool(cp.Final), cp.Text,
			strconv.FormatFloat(cp.End, 'f', -1, 64)} {
			_, _ = io.WriteString(h, part)
			_, _ = h.Write([]byte{0})
		}
		pipe := int32(-1)
		if cp.LatencyMs != nil {
			pipe = int32(*cp.LatencyMs)
		}
		v.samples = append(v.samples, sample{recv: now, key: h.Sum64(), end: cp.End, pipeMs: pipe, final: cp.Final})
	}
}

func (r *runner) summarize(rep *Report, sessions []*sessionRun) {
	secs := rep.Measured.Seconds()
	rep.Messages, rep.Bytes = r.msgs.Load(), r.bytes.Load()
	rep.DialFailures, rep.Drops, rep.Disconnects = r.dialKO.Load(), r.drops.Load(), r.disc.Load()
	if secs > 0 {
		rep.MessagesPerSec = float64(rep.Messages) / secs
		rep.BytesPerSec = float64(rep.Bytes) / secs
	}
	var lag, fan, pipe []float64
	var events int
	for _, s := range sessions {
		sLag, sFan, sPipe, n := sessionStats(s.origin.Load(), s.viewers)
		lag, fan, pipe = append(lag, sLag...), append(fan, sFan...), append(pipe, sPipe...)
		events += n
	}
	if secs > 0 && len(sessions) > 0 {
		rep.EventsPerSecond = float64(events) / float64(len(sessions)) / secs
	}
	rep.Lag, rep.Fanout, rep.Pipeline = Summarize(lag), Summarize(fan), Summarize(pipe)
}

// sessionStats turns one session's viewer samples into lag, fan-out and
// pipeline samples (ms), and counts the distinct caption events.
func sessionStats(origin int64, viewers []*viewer) (lag, fan, pipe []float64, events int) {
	firstSeen := map[uint64]int64{}
	best := math.Inf(1)
	audioLag := func(x sample) float64 { return float64(x.recv-origin)/1e6 - x.end*1000 }
	for _, v := range viewers {
		for _, x := range v.samples {
			if t, ok := firstSeen[x.key]; !ok || x.recv < t {
				firstSeen[x.key] = x.recv
			}
			if x.final {
				best = min(best, audioLag(x))
			}
		}
	}
	for _, v := range viewers {
		for _, x := range v.samples {
			fan = append(fan, float64(x.recv-firstSeen[x.key])/1e6)
			if !x.final {
				continue
			}
			lag = append(lag, audioLag(x)-best)
			if x.pipeMs >= 0 {
				pipe = append(pipe, float64(x.pipeMs))
			}
		}
	}
	return lag, fan, pipe, len(firstSeen)
}

// admin calls the admin API with the bearer token; out, if not nil, gets
// the JSON response.
func (r *runner) admin(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.cfg.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, bytes.TrimSpace(b))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// captionClients reads livesubs_ws_clients{endpoint="captions"} from /metrics.
func (r *runner) captionClients(ctx context.Context) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.cfg.BaseURL+"/metrics", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.Token)
	res, err := r.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("/metrics: %d", res.StatusCode)
	}
	return metricValue(res.Body, `livesubs_ws_clients{endpoint="captions"}`)
}

// metricValue finds one series in a Prometheus text exposition.
func metricValue(rd io.Reader, series string) (float64, error) {
	sc := bufio.NewScanner(rd)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), series+" "); ok {
			return strconv.ParseFloat(strings.TrimSpace(v), 64)
		}
	}
	return 0, fmt.Errorf("%s not in /metrics", series)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
