// SPDX-License-Identifier: Apache-2.0

package streamcc

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

// Error codes in StreamCaptionStatus.error, API errors and admin log
// events (UI-4).
const (
	CodeNoURL       = "streamcc.no_url"
	CodeInvalidURL  = "streamcc.url_invalid"
	CodeUnreachable = "streamcc.unreachable"
	CodeServerError = "streamcc.server_error"
	CodeRejected    = "streamcc.rejected"
	CodeDropped     = "streamcc.dropped"

	CodeOBSUnreachable = "streamcc.obs_unreachable"
	CodeOBSAuthFailed  = "streamcc.obs_auth_failed"
	CodeOBSRejected    = "streamcc.obs_rejected"
)

// Errors returned by Service methods.
var (
	// ErrInvalidURL: the ingestion URL isn't an absolute http(s) URL.
	ErrInvalidURL = errors.New("streamcc: invalid caption ingestion URL")
	// ErrNoURL: the session has no ingestion URL to send a test caption to.
	ErrNoURL = errors.New("streamcc: no caption ingestion URL set")
)

// Defaults for Options.
const (
	DefaultMaxAge       = 60 * time.Second
	DefaultQueueSize    = 64
	DefaultDrainTimeout = 10 * time.Second
	DefaultMinBackoff   = 500 * time.Millisecond
	DefaultMaxBackoff   = 10 * time.Second
	DefaultTimeout      = 10 * time.Second
	// DefaultTestText is sent by SendTest when no text is given.
	DefaultTestText = "Live Subtitles: test caption / subtítulo de prueba"
)

// Options configure a Service. Secrets is required.
type Options struct {
	Secrets domain.SecretStore
	// Settings gives obs.websocketUrl for the OBS target; nil uses
	// DefaultOBSURL.
	Settings domain.SettingsStore
	// Redactor learns the ingestion URLs the service reads, so logs mask
	// them even outside the secret store's own reads; nil is fine.
	Redactor *secrets.Redactor
	// Client posts to YouTube; nil uses a client with DefaultTimeout.
	Client *http.Client
	Clock  domain.Clock
	Logger *slog.Logger
	// MaxAge drops cues whose speech ended longer ago than this instead of
	// sending them late, e.g. after an outage (default DefaultMaxAge; about
	// the broadcast delay YouTube recommends for HTTP captions).
	MaxAge time.Duration
	// QueueSize bounds the captions waiting per session; the oldest is
	// dropped when it's full (default DefaultQueueSize).
	QueueSize int
	// DrainTimeout bounds how long a stopped run keeps sending its queue
	// (default DefaultDrainTimeout).
	DrainTimeout time.Duration
	// MinBackoff and MaxBackoff bound the retry delay after a failed POST.
	MinBackoff, MaxBackoff time.Duration
	// OBSMinGap and OBSMaxGap bound how long one OBS caption stays up
	// (defaults DefaultOBSMinGap, DefaultOBSMaxGap).
	OBSMinGap, OBSMaxGap time.Duration
}

// Service sends the final captions of one chosen track per running session
// to the live stream as closed captions (OUT-10, CC-1..CC-3).
//
// It sees captions through Tap, a wrapper of the caption bus the session
// manager publishes to, so the pipeline doesn't change and the sink isn't
// counted as a viewer. The manager tells it when a run starts and ends
// (session.Options.StreamCaptions) and shows Status in SessionStatus.
type Service struct {
	opts Options
	log  *slog.Logger
	obs  *obs // the OBS target, shared: one OBS streams at a time

	mu       sync.Mutex
	sessions map[string]*sessionState
	live     map[*sink]struct{} // running sinks, including draining ones
	wg       sync.WaitGroup     // sink goroutines

	bindMu   sync.Mutex
	events   func(api.AdminEvent)
	statusOf func(id string) api.SessionStatus
}

// sessionState is what the service keeps per session across runs.
type sessionState struct {
	seq    atomic.Int64 // last seq used; YouTube wants it increasing
	yt     *youtube
	status *api.StreamCaptionStatus // nil until a run or a test
	sink   *sink                    // the running run's sink
}

// New returns a Service.
func New(opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = domain.SystemClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: DefaultTimeout}
	}
	if opts.MaxAge <= 0 {
		opts.MaxAge = DefaultMaxAge
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = DefaultQueueSize
	}
	if opts.DrainTimeout <= 0 {
		opts.DrainTimeout = DefaultDrainTimeout
	}
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = DefaultMinBackoff
	}
	if opts.MaxBackoff < opts.MinBackoff {
		opts.MaxBackoff = max(DefaultMaxBackoff, opts.MinBackoff)
	}
	if opts.OBSMinGap <= 0 {
		opts.OBSMinGap = DefaultOBSMinGap
	}
	if opts.OBSMaxGap < opts.OBSMinGap {
		opts.OBSMaxGap = max(DefaultOBSMaxGap, opts.OBSMinGap)
	}
	return &Service{opts: opts, log: opts.Logger, obs: newOBS(opts), sessions: map[string]*sessionState{}, live: map[*sink]struct{}{}}
}

// Bind connects the admin event stream: status changes are published as
// streamCaptionStatus events carrying statusOf(id) with the new stream
// caption status. Call it before sessions run.
func (s *Service) Bind(events func(api.AdminEvent), statusOf func(id string) api.SessionStatus) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	s.events, s.statusOf = events, statusOf
}

// secretName is the per-session secret holding the ingestion URL.
func secretName(sessionID string) string { return "session:" + sessionID + ":youtube_url" }

// state returns the session's state, creating it. Called with mu held.
func (s *Service) state(id string) *sessionState {
	st := s.sessions[id]
	if st == nil {
		st = &sessionState{yt: &youtube{client: s.opts.Client, clock: s.opts.Clock}}
		s.sessions[id] = st
	}
	return st
}

// Tap wraps bus: captions published through it also reach the running
// sinks. Everything else passes through.
func (s *Service) Tap(bus domain.CaptionBus) domain.CaptionBus { return tap{bus, s} }

type tap struct {
	domain.CaptionBus
	s *Service
}

func (t tap) Publish(sessionID string, msg domain.BusMessage) {
	t.CaptionBus.Publish(sessionID, msg)
	if msg.Type == api.CaptionsServerMessageTypeCaption && msg.Caption != nil && msg.Caption.Final {
		t.s.offer(sessionID, *msg.Caption)
	}
}

// offer queues a final caption on the session's sink if it carries the
// sink's track.
func (s *Service) offer(id string, c api.Caption) {
	s.mu.Lock()
	var sk *sink
	if st := s.sessions[id]; st != nil {
		sk = st.sink
	}
	s.mu.Unlock()
	if sk != nil && sk.track == c.Lang {
		sk.enqueue(c)
	}
}

// RunStarted starts the session's sink when its stream captions are
// enabled. The configuration is read once per run.
func (s *Service) RunStarted(sess domain.Session) {
	cfg := api.StreamCaptionsConfig{}
	if sess.StreamCaptions != nil {
		cfg = *sess.StreamCaptions
	}
	target := api.YoutubeHttp
	if cfg.Target != nil && cfg.Target.Valid() {
		target = *cfg.Target
	}
	if cfg.Enabled == nil || !*cfg.Enabled {
		s.setStatus(sess.Id, func(st *api.StreamCaptionStatus) {
			*st = api.StreamCaptionStatus{State: api.StreamCaptionStatusStateDisabled, Target: &target}
		})
		return
	}
	track := "en"
	if cfg.Track != nil && *cfg.Track != "" {
		track = *cfg.Track
	}
	maxChars := lineLength(cfg, target)

	url := ""
	if target == api.YoutubeHttp {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		url = s.readURL(ctx, sess.Id)
		cancel()
	}

	s.mu.Lock()
	st := s.state(sess.Id)
	sk := newSink(s, sess.Id, st, target, track, maxChars)
	sk.setURL(url)
	old := st.sink
	st.sink = sk
	s.live[sk] = struct{}{}
	s.wg.Add(1)
	s.mu.Unlock()
	if old != nil { // a run that ended without RunEnded
		old.close(s.opts.DrainTimeout)
	}

	state := api.StreamCaptionStatusStateIdle
	var e *api.Error
	if url == "" && target == api.YoutubeHttp {
		state, e = api.StreamCaptionStatusStateError, &api.Error{Code: CodeNoURL, Message: "no caption ingestion URL is set"}
	}
	s.setStatus(sess.Id, func(st *api.StreamCaptionStatus) {
		st.State, st.Target, st.Error = state, &target, e
	})
	s.log.Info("stream captions on", "session", sess.Id, "target", target, "track", track)
	go func() {
		defer s.wg.Done()
		sk.run()
		s.mu.Lock()
		delete(s.live, sk)
		s.mu.Unlock()
	}()
}

// lineLength is the session's line length for target: OBS encodes CEA-608,
// whose lines hold 32 characters at most.
func lineLength(cfg api.StreamCaptionsConfig, target api.StreamCaptionTarget) int {
	n := DefaultMaxChars
	if cfg.MaxCharsPerLine != nil && *cfg.MaxCharsPerLine > 0 {
		n = *cfg.MaxCharsPerLine
	}
	if target == api.ObsWebsocket {
		n = min(n, OBSMaxChars)
	}
	return n
}

// RunEnded stops the session's sink after it sends what's queued (up to
// DrainTimeout).
func (s *Service) RunEnded(id string) {
	s.mu.Lock()
	var sk *sink
	if st := s.sessions[id]; st != nil {
		sk, st.sink = st.sink, nil
	}
	s.mu.Unlock()
	if sk != nil {
		sk.close(s.opts.DrainTimeout)
	}
}

// sinkDone settles the status once a sink has stopped, unless a new run
// has started meanwhile.
func (s *Service) sinkDone(id string) {
	s.mu.Lock()
	running := s.sessions[id] != nil && s.sessions[id].sink != nil
	s.mu.Unlock()
	if running {
		return
	}
	s.setStatus(id, func(st *api.StreamCaptionStatus) {
		if st.State != api.StreamCaptionStatusStateDisabled {
			st.State, st.Error = api.StreamCaptionStatusStateIdle, nil
		}
	})
	s.log.Info("stream captions off", "session", id)
}

// Status is the session's stream-caption status, nil before its first run
// or test.
func (s *Service) Status(id string) *api.StreamCaptionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[id]
	if st == nil || st.status == nil {
		return nil
	}
	return copyStatus(st.status)
}

func copyStatus(in *api.StreamCaptionStatus) *api.StreamCaptionStatus {
	out := *in
	if in.Error != nil {
		e := *in.Error
		out.Error = &e
	}
	return &out
}

// setStatus changes the session's status with f and tells admins.
func (s *Service) setStatus(id string, f func(*api.StreamCaptionStatus)) {
	s.mu.Lock()
	st := s.state(id)
	if st.status == nil {
		st.status = &api.StreamCaptionStatus{State: api.StreamCaptionStatusStateIdle}
	}
	before := *copyStatus(st.status)
	f(st.status)
	if off, ok := st.yt.measuredOffset(); ok {
		ms := int(off.Milliseconds())
		st.status.ClockOffsetMs = &ms
	}
	changed := !sameStatus(before, *st.status)
	s.mu.Unlock()
	if changed {
		s.publish(id)
	}
}

func sameStatus(a, b api.StreamCaptionStatus) bool {
	eq := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	eqi := func(x, y *int) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	eqt := func(x, y *time.Time) bool { return (x == nil) == (y == nil) && (x == nil || x.Equal(*y)) }
	eqe := func(x, y *api.Error) bool {
		return (x == nil) == (y == nil) && (x == nil || (x.Code == y.Code && x.Message == y.Message))
	}
	eqg := func(x, y *api.StreamCaptionTarget) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.State == b.State && eq(a.LastSeq, b.LastSeq) && eqi(a.ClockOffsetMs, b.ClockOffsetMs) &&
		eqt(a.LastSentAt, b.LastSentAt) && eqe(a.Error, b.Error) && eqg(a.Target, b.Target)
}

// publish sends a streamCaptionStatus admin event.
func (s *Service) publish(id string) {
	s.bindMu.Lock()
	events, statusOf := s.events, s.statusOf
	s.bindMu.Unlock()
	if events == nil {
		return
	}
	st := api.SessionStatus{SessionId: id, State: api.SessionStateIdle}
	if statusOf != nil {
		st = statusOf(id)
	}
	st.StreamCaptions = s.Status(id)
	events(api.AdminEvent{Type: api.AdminEventTypeStreamCaptionStatus, At: s.opts.Clock.Now(), SessionId: &id, Status: &st})
}

// logEvent sends a translatable log line to admins.
func (s *Service) logEvent(level api.AdminEventLogLevel, code, id string, params map[string]any) {
	s.bindMu.Lock()
	events := s.events
	s.bindMu.Unlock()
	if events == nil {
		return
	}
	ev := api.AdminEvent{Type: api.AdminEventTypeLog, At: s.opts.Clock.Now(), SessionId: &id}
	ev.Log = &struct {
		Code      string                  `json:"code"`
		Level     api.AdminEventLogLevel  `json:"level"`
		Params    *map[string]interface{} `json:"params,omitempty"`
		SessionId *api.Slug               `json:"sessionId,omitempty"`
	}{Code: code, Level: level, SessionId: &id}
	if params != nil {
		ev.Log.Params = &params
	}
	events(ev)
}

// readURL returns the session's ingestion URL, "" when none is stored.
func (s *Service) readURL(ctx context.Context, id string) string {
	v, _, err := s.opts.Secrets.GetSecret(ctx, secretName(id))
	switch {
	case errors.Is(err, secrets.ErrNotFound):
		return ""
	case err != nil:
		s.log.Warn("read the caption ingestion URL", "session", id, "secret", secretName(id), "err", err)
		return ""
	}
	s.opts.Redactor.Add(v)
	return strings.TrimSpace(v)
}

// SetYouTubeURL stores the session's caption ingestion URL (CC-2) and
// hands it to the running sink. It returns only masked info. Errors:
// ErrInvalidURL, secrets.ErrReadOnly, secrets.ErrNoBackend.
func (s *Service) SetYouTubeURL(ctx context.Context, sessionID, raw string) (api.SecretInfo, error) {
	raw = strings.TrimSpace(raw)
	if err := ValidateURL(raw); err != nil {
		return api.SecretInfo{}, err
	}
	s.opts.Redactor.Add(raw)
	if err := s.opts.Secrets.SetSecret(ctx, secretName(sessionID), raw); err != nil {
		return api.SecretInfo{}, err
	}
	s.mu.Lock()
	if st := s.sessions[sessionID]; st != nil && st.sink != nil {
		st.sink.setURL(raw)
	}
	s.mu.Unlock()
	s.log.Info("caption ingestion URL stored", "session", sessionID, "secret", secretName(sessionID))
	return s.urlInfo(ctx, sessionID)
}

// DeleteYouTubeURL removes the session's ingestion URL. Errors:
// secrets.ErrNotFound, secrets.ErrReadOnly.
func (s *Service) DeleteYouTubeURL(ctx context.Context, sessionID string) error {
	if err := s.opts.Secrets.DeleteSecret(ctx, secretName(sessionID)); err != nil {
		return err
	}
	s.mu.Lock()
	if st := s.sessions[sessionID]; st != nil && st.sink != nil {
		st.sink.setURL("")
	}
	s.mu.Unlock()
	s.log.Info("caption ingestion URL removed", "session", sessionID, "secret", secretName(sessionID))
	return nil
}

// YouTubeURLSet reports whether the session has an ingestion URL.
func (s *Service) YouTubeURLSet(ctx context.Context, sessionID string) bool {
	info, err := s.opts.Secrets.SecretInfo(ctx, secretName(sessionID))
	return err == nil && info.Set
}

func (s *Service) urlInfo(ctx context.Context, sessionID string) (api.SecretInfo, error) {
	info, err := s.opts.Secrets.SecretInfo(ctx, secretName(sessionID))
	if err != nil {
		return api.SecretInfo{}, err
	}
	info.Name = api.YoutubeCaptionUrl
	return info, nil
}

// Forget drops what the service keeps about a deleted session, including
// its ingestion URL.
func (s *Service) Forget(ctx context.Context, sessionID string) {
	s.RunEnded(sessionID)
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	if err := s.opts.Secrets.DeleteSecret(ctx, secretName(sessionID)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		s.log.Warn("remove the caption ingestion URL of a deleted session", "session", sessionID, "secret", secretName(sessionID), "err", err)
	}
}

// SendTest sends text (DefaultTestText when empty) to the session's
// target now, whether or not the session runs or has stream captions
// enabled, and returns the resulting status. ErrNoURL when there's no
// ingestion URL; a failed delivery is reported in the status.
func (s *Service) SendTest(ctx context.Context, sess domain.Session, text string) (api.StreamCaptionStatus, error) {
	cfg := api.StreamCaptionsConfig{}
	if sess.StreamCaptions != nil {
		cfg = *sess.StreamCaptions
	}
	target := api.YoutubeHttp
	if cfg.Target != nil && cfg.Target.Valid() {
		target = *cfg.Target
	}
	if strings.TrimSpace(text) == "" {
		text = DefaultTestText
	}
	url := ""
	if target == api.YoutubeHttp {
		if url = s.readURL(ctx, sess.Id); url == "" {
			return api.StreamCaptionStatus{}, ErrNoURL
		}
	}
	s.mu.Lock()
	st := s.state(sess.Id)
	s.mu.Unlock()

	now := s.opts.Clock.Now()
	it := item{segment: "test", cues: cues(text, lineLength(cfg, target), now, now), end: now}
	seq := st.seq.Add(1)
	var err error
	if target == api.ObsWebsocket {
		err = s.obs.send(ctx, &it)
	} else {
		err = st.yt.post(ctx, url, seq, it)
	}
	s.recordDelivery(sess.Id, target, seq, err, false)
	return *s.Status(sess.Id), nil
}

// recordDelivery updates the status after one delivery attempt; retry
// says a failed attempt will be tried again.
func (s *Service) recordDelivery(id string, target api.StreamCaptionTarget, seq int64, err error, retry bool) {
	now := s.opts.Clock.Now()
	s.setStatus(id, func(st *api.StreamCaptionStatus) {
		st.Target = &target
		if err == nil {
			st.State, st.Error = api.StreamCaptionStatusStateOk, nil
			st.LastSeq, st.LastSentAt = &seq, &now
			return
		}
		de := asDelivery(err)
		st.State = api.StreamCaptionStatusStateError
		if retry && de.retryable {
			st.State = api.StreamCaptionStatusStateRetrying
		}
		e := api.Error{Code: de.code, Message: de.message}
		if de.params != nil {
			p := de.params
			e.Params = &p
		}
		st.Error = &e
	})
}

func asDelivery(err error) *deliveryError {
	var de *deliveryError
	if errors.As(err, &de) {
		return de
	}
	return &deliveryError{code: CodeUnreachable, message: err.Error(), retryable: true}
}

// Close stops every sink without draining and waits for them.
func (s *Service) Close() {
	s.mu.Lock()
	sinks := make([]*sink, 0, len(s.live))
	for sk := range s.live {
		sinks = append(sinks, sk)
	}
	for _, st := range s.sessions {
		st.sink = nil
	}
	s.mu.Unlock()
	for _, sk := range sinks {
		sk.close(0)
	}
	s.wg.Wait()
	s.obs.close()
}
