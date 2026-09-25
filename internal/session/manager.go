// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/metrics"
)

// Provider is one AI provider: its ASR and its translator.
type Provider struct {
	ASR        domain.ASRProvider
	Translator domain.Translator
}

// Options configure a Manager. Sessions, Bus and IngestSource are required.
type Options struct {
	Sessions domain.SessionStore
	// Captions stores final captions; nil keeps them only on the bus.
	Captions domain.CaptionStore
	Bus      domain.CaptionBus
	// Providers by kind. A session whose provider is missing fails to start.
	Providers map[domain.ProviderKind]Provider
	// DefaultProvider resolves `provider: default` (AI-11, the
	// provider/selector rule); nil means mock (tests).
	DefaultProvider func(ctx context.Context) domain.ProviderKind
	// IngestSource returns the session's browser ingest source (/ws/ingest),
	// used when Start gets no source.
	IngestSource func(sessionID string) domain.AudioSource
	// IngestStatus reports ingest audio while no other source runs; optional.
	IngestStatus func(sessionID string) api.AudioStatus
	// ReleaseIngest forgets the session's ingest source after a run, so the
	// next run starts a fresh timeline; optional.
	ReleaseIngest func(sessionID string)
	// Recorder, if set, gets every frame of a running session (P3-07).
	Recorder domain.Recorder
	Clock    domain.Clock
	Logger   *slog.Logger
	// StatusInterval is how often running sessions publish their status on
	// the admin event stream (default 1 s).
	StatusInterval time.Duration
	// StopTimeout bounds how long Stop waits for pending captions to flush
	// (default 10 s).
	StopTimeout time.Duration
	// Settings, if set, is read at each start for
	// translation.contextSentences.
	Settings domain.SettingsStore
	// Glossaries, if set, loads the session's glossary at each start (AI-7).
	Glossaries domain.GlossaryStore
	// ContextSentences is how many previous final sentences go to the
	// translator as context when the settings don't say (default 3).
	ContextSentences int
	// TranslateTimeout bounds one translation call (default 15 s).
	TranslateTimeout time.Duration
	// Pricing estimates the cost of provider usage in
	// SessionStatus.usage (AI-9); nil uses metrics.DefaultPricing.
	Pricing *metrics.Pricing
	// Observer, if set, gets caption latencies and run errors as they
	// happen, for /metrics (P3-13).
	Observer Observer
	// Restart configures the automatic restart of a crashed provider
	// stream or a failed source (SES-5).
	Restart RestartPolicy
	// StreamCaptions, if set, is told when a run starts and when it
	// ends, and gives SessionStatus.streamCaptions (P3-16).
	StreamCaptions StreamCaptions
}

// Observer receives pipeline measurements (metrics.App implements it).
// Calls must be quick: they run on the pipeline goroutines.
type Observer interface {
	// CaptionLatency: a final caption on track was emitted ms after its
	// audio reached the server.
	CaptionLatency(provider domain.ProviderKind, track string, ms int)
	// SessionError: a run reported an error with this code.
	SessionError(provider domain.ProviderKind, code string)
}

// StreamCaptions sends a session's final captions to the live stream
// while it runs (internal/streamcc). It gets the captions from the bus.
type StreamCaptions interface {
	// RunStarted is called when a run starts (not on resume), before its
	// pipeline; RunEnded follows when it ends or fails to start.
	RunStarted(sess domain.Session)
	RunEnded(sessionID string)
	// Status is nil when there's nothing to show.
	Status(sessionID string) *api.StreamCaptionStatus
}

// Errors returned by Manager methods, besides domain.ErrNotFound.
var (
	// ErrState: the operation isn't allowed in the session's current state
	// (it wraps domain.ErrConflict).
	ErrState = fmt.Errorf("%w: session state", domain.ErrConflict)
	// ErrUnavailable: the provider or the audio source can't start. The
	// session is left in the error state with the reason in its status.
	ErrUnavailable = errors.New("session: provider or source unavailable")
)

// Error codes reported in SessionStatus.error and admin log events (UI-4).
const (
	CodeProviderUnavailable = "provider.unavailable"
	CodeProviderError       = "provider.error"
	CodeSourceUnavailable   = "source.unavailable"
	CodeSourceFailed        = "source.failed"
	CodeTranslationFailed   = "translation.failed"
)

// Manager runs sessions: one pipeline per running session (source → ASR →
// translator fan-out → caption bus + store) with the state machine
// idle → starting → live ⇄ paused → stopping → idle, or error (SES-2,
// SES-3, SES-4).
//
// Runtime state lives here, not in the store: after a restart every
// session is idle.
type Manager struct {
	opts    Options
	log     *slog.Logger
	events  *Events
	pricing metrics.Pricing

	mu      sync.Mutex
	runs    map[string]*run                         // running sessions, including paused and stopping
	failed  map[string]api.Error                    // why a session's last start or run failed
	clocks  map[string]time.Duration                // session clock at the end of the last run
	totals  map[string]totals                       // usage of the finished runs, per session
	latency map[string]*map[string]api.LatencyStats // per-track latency of the last run
	closed  bool
	done    chan struct{} // closed by Close; stops the status ticker
}

// New returns a Manager. Call Close to stop every session.
func New(opts Options) *Manager {
	if opts.Clock == nil {
		opts.Clock = domain.SystemClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.StatusInterval <= 0 {
		opts.StatusInterval = time.Second
	}
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = 10 * time.Second
	}
	if opts.ContextSentences <= 0 {
		opts.ContextSentences = 3
	}
	if opts.TranslateTimeout <= 0 {
		opts.TranslateTimeout = 15 * time.Second
	}
	opts.Restart = opts.Restart.withDefaults()
	pricing := metrics.DefaultPricing()
	if opts.Pricing != nil {
		pricing = *opts.Pricing
	}
	m := &Manager{
		opts:    opts,
		log:     opts.Logger,
		events:  NewEvents(),
		pricing: pricing,
		runs:    map[string]*run{},
		failed:  map[string]api.Error{},
		clocks:  map[string]time.Duration{},
		totals:  map[string]totals{},
		latency: map[string]*map[string]api.LatencyStats{},
		done:    make(chan struct{}),
	}
	go m.tick()
	return m
}

// Events is the admin event stream (/ws/admin).
func (m *Manager) Events() *Events { return m.events }

// Start starts the session with src as its audio, or with its browser
// ingest source when src is nil. With a nil src a paused session resumes.
// It returns ErrState while the session is running (or paused, for a new
// src), and ErrUnavailable when the provider or source can't start.
func (m *Manager) Start(ctx context.Context, id string, src domain.AudioSource) (api.SessionStatus, error) {
	sess, err := m.opts.Sessions.GetSession(ctx, id)
	if err != nil {
		return api.SessionStatus{}, err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return api.SessionStatus{}, ErrState
	}
	if r := m.runs[id]; r != nil {
		defer m.mu.Unlock()
		if r.state() != api.SessionStatePaused || src != nil {
			return api.SessionStatus{}, ErrState
		}
		r.setState(api.SessionStateLive)
		m.log.Info("session resumed", "session", id)
		return m.changed(r), nil
	}
	r := newRun(m, sess)
	r.prior = m.totals[id]
	m.runs[id] = r
	delete(m.failed, id)
	m.mu.Unlock()
	m.changed(r)

	if err := m.launch(ctx, r, src); err != nil {
		if sc := m.opts.StreamCaptions; sc != nil {
			sc.RunEnded(id)
		}
		m.mu.Lock()
		delete(m.runs, id)
		m.failed[id] = r.lastError()
		m.mu.Unlock()
		r.cancel()
		m.log.Warn("session failed to start", "session", id, "err", err)
		return m.publishStatus(ctx, id), err
	}
	m.log.Info("session live", "session", id, "provider", r.provider, "source", r.source.Kind())
	return m.changed(r), nil
}

// launch resolves the provider and starts the source, the ASR stream and
// the pipeline goroutines. On error r holds the reason.
func (m *Manager) launch(ctx context.Context, r *run, src domain.AudioSource) error {
	kind := m.resolve(ctx, r.sess.Provider)
	p, ok := m.opts.Providers[kind]
	if !ok || p.ASR == nil || p.Translator == nil {
		r.fail(api.Error{Code: CodeProviderUnavailable, Message: fmt.Sprintf("provider %q is not available", kind),
			Params: &map[string]any{"provider": kind}})
		return fmt.Errorf("%w: provider %s", ErrUnavailable, kind)
	}
	if src == nil {
		src = m.opts.IngestSource(r.id)
	}
	offset := m.clockOrigin(ctx, r.id)
	r.mu.Lock() // status() and SourceKind read these concurrently
	r.provider, r.asr, r.translator = kind, p.ASR, p.Translator
	r.source, r.offset = src, offset
	r.mu.Unlock()
	// Before the pipeline starts, so its end (finished) comes after.
	if sc := m.opts.StreamCaptions; sc != nil {
		sc.RunStarted(r.sess)
	}
	if err := r.start(ctx); err != nil {
		return err
	}
	r.goLive()
	return nil
}

// resolve maps the session's provider choice to a provider kind.
func (m *Manager) resolve(ctx context.Context, choice api.ProviderChoice) domain.ProviderKind {
	switch choice {
	case api.ProviderChoiceGemini, api.ProviderChoiceLocal, api.ProviderChoiceMock:
		return domain.ProviderKind(choice)
	}
	if m.opts.DefaultProvider != nil {
		return m.opts.DefaultProvider(ctx)
	}
	return api.ProviderKindMock
}

// clockOrigin is where a new run starts on the session clock: after the
// end of the previous run, so captions of later runs never overlap earlier
// ones. After a restart it's taken from the last stored caption.
func (m *Manager) clockOrigin(ctx context.Context, id string) time.Duration {
	m.mu.Lock()
	t, ok := m.clocks[id]
	m.mu.Unlock()
	if !ok && m.opts.Captions != nil {
		t = m.lastCaptionEnd(ctx, id)
	}
	if t == 0 {
		return 0
	}
	// Start on the next whole second, at least one second after the last run.
	return time.Duration(math.Ceil(t.Seconds())+1) * time.Second
}

func (m *Manager) lastCaptionEnd(ctx context.Context, id string) time.Duration {
	q := domain.CaptionQuery{SessionID: id, Track: domain.SourceTrack, Limit: 1000}
	var end float32
	for {
		items, next, err := m.opts.Captions.ListCaptions(ctx, q)
		if err != nil {
			m.log.Warn("read captions for the session clock", "session", id, "err", err)
			return 0
		}
		for _, c := range items {
			end = max(end, c.End)
		}
		if next == "" {
			return time.Duration(float64(end) * float64(time.Second))
		}
		q.Cursor = next
	}
}

// Pause stops sending audio to the provider and the recorder; the source
// keeps running so the session resumes instantly with Start.
func (m *Manager) Pause(ctx context.Context, id string) (api.SessionStatus, error) {
	if _, err := m.opts.Sessions.GetSession(ctx, id); err != nil {
		return api.SessionStatus{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[id]
	if r == nil || r.state() != api.SessionStateLive {
		return api.SessionStatus{}, ErrState
	}
	r.setState(api.SessionStatePaused)
	m.log.Info("session paused", "session", id)
	return m.changed(r), nil
}

// Stop ends the session's source and waits (up to StopTimeout) for the
// provider to flush its last captions. Stopping an idle session is a no-op.
func (m *Manager) Stop(ctx context.Context, id string) (api.SessionStatus, error) {
	if _, err := m.opts.Sessions.GetSession(ctx, id); err != nil {
		return api.SessionStatus{}, err
	}
	m.mu.Lock()
	r := m.runs[id]
	if r == nil {
		delete(m.failed, id) // stopping clears a start error
		m.mu.Unlock()
		return m.publishStatus(ctx, id), nil
	}
	first := r.state() != api.SessionStateStopping
	if first {
		r.setState(api.SessionStateStopping)
	}
	m.mu.Unlock()
	if first {
		m.changed(r)
		m.log.Info("session stopping", "session", id)
		r.stopSource()
	}
	select {
	case <-r.done:
	case <-time.After(m.opts.StopTimeout):
		m.log.Warn("session did not flush in time; cancelling", "session", id)
		r.cancel()
		<-r.done
	case <-ctx.Done():
		return m.status(id), ctx.Err()
	}
	return m.status(id), nil
}

// finished is called by a run when its pipeline has ended.
func (m *Manager) finished(r *run) {
	usage, latency := r.finalStats()
	m.mu.Lock()
	if m.runs[r.id] == r {
		delete(m.runs, r.id)
	}
	m.clocks[r.id] = r.clock()
	m.totals[r.id] = usage
	if latency != nil {
		m.latency[r.id] = latency
	}
	if e := r.lastError(); e.Code != "" && r.failedRun() {
		m.failed[r.id] = e
	}
	m.mu.Unlock()
	if m.opts.ReleaseIngest != nil {
		m.opts.ReleaseIngest(r.id)
	}
	if sc := m.opts.StreamCaptions; sc != nil {
		sc.RunEnded(r.id)
	}
	m.publishStatus(context.Background(), r.id)
	m.log.Info("session stopped", "session", r.id)
}

// SourceKind reports the audio source of a running session.
func (m *Manager) SourceKind(id string) (api.AudioSourceKind, bool) {
	m.mu.Lock()
	r := m.runs[id]
	m.mu.Unlock()
	if r == nil {
		return "", false
	}
	r.mu.Lock()
	src := r.source
	r.mu.Unlock()
	if src == nil {
		return "", false
	}
	return src.Kind(), true
}

// EffectiveProvider is the provider a session with this choice runs on.
func (m *Manager) EffectiveProvider(ctx context.Context, choice api.ProviderChoice) domain.ProviderKind {
	return m.resolve(ctx, choice)
}

// Forget drops what the manager remembers about a session that isn't
// running (its last error, its clock and its ingest source), before the
// session is deleted. ErrState if it's running.
func (m *Manager) Forget(id string) error {
	m.mu.Lock()
	if m.runs[id] != nil {
		m.mu.Unlock()
		return ErrState
	}
	delete(m.failed, id)
	delete(m.clocks, id)
	delete(m.totals, id)
	delete(m.latency, id)
	m.mu.Unlock()
	if m.opts.ReleaseIngest != nil {
		m.opts.ReleaseIngest(id)
	}
	return nil
}

// State is the session's runtime state (idle when it isn't running).
func (m *Manager) State(id string) api.SessionState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.runs[id]; r != nil {
		return r.state()
	}
	if _, ok := m.failed[id]; ok {
		return api.SessionStateError
	}
	return api.SessionStateIdle
}

// Status returns the session's live status; domain.ErrNotFound if the
// session doesn't exist.
func (m *Manager) Status(ctx context.Context, id string) (api.SessionStatus, error) {
	if _, err := m.opts.Sessions.GetSession(ctx, id); err != nil {
		return api.SessionStatus{}, err
	}
	return m.status(id), nil
}

// StatusOf is Status without the store lookup, for a session known to exist.
func (m *Manager) StatusOf(id string) api.SessionStatus { return m.status(id) }

func (m *Manager) status(id string) api.SessionStatus {
	m.mu.Lock()
	r := m.runs[id]
	failed, isFailed := m.failed[id]
	used, hasUsage := m.totals[id]
	latency := m.latency[id]
	m.mu.Unlock()
	if r != nil {
		return r.status()
	}
	st := api.SessionStatus{SessionId: id, State: api.SessionStateIdle, Viewers: m.opts.Bus.Viewers(id)}
	if isFailed {
		st.State, st.Error = api.SessionStateError, &failed
	}
	// A stopped session keeps showing what it used and its last latencies.
	if hasUsage {
		st.Usage = used.stats()
	}
	st.Latency = latency
	if sc := m.opts.StreamCaptions; sc != nil {
		st.StreamCaptions = sc.Status(id)
	}
	if m.opts.IngestStatus != nil {
		a := m.opts.IngestStatus(id)
		st.Audio = &a
	}
	return st
}

// changed publishes a state change of r to viewers and admins, and
// returns the new status.
func (m *Manager) changed(r *run) api.SessionStatus {
	st := r.status()
	m.opts.Bus.Publish(r.id, domain.BusMessage{Type: api.CaptionsServerMessageTypeState, State: &st.State, DetectedLanguage: st.DetectedLanguage})
	m.events.Publish(api.AdminEvent{Type: api.AdminEventTypeSessionStatus, At: m.opts.Clock.Now(), Status: &st, SessionId: &st.SessionId})
	return st
}

// publishStatus publishes the status of a session that isn't running.
func (m *Manager) publishStatus(_ context.Context, id string) api.SessionStatus {
	st := m.status(id)
	m.opts.Bus.Publish(id, domain.BusMessage{Type: api.CaptionsServerMessageTypeState, State: &st.State})
	m.events.Publish(api.AdminEvent{Type: api.AdminEventTypeSessionStatus, At: m.opts.Clock.Now(), Status: &st, SessionId: &id})
	return st
}

// Log sends a translatable log line to admins.
func (m *Manager) logEvent(level api.AdminEventLogLevel, code, sessionID string, params map[string]any) {
	ev := api.AdminEvent{Type: api.AdminEventTypeLog, At: m.opts.Clock.Now(), SessionId: &sessionID}
	ev.Log = &struct {
		Code      string                  `json:"code"`
		Level     api.AdminEventLogLevel  `json:"level"`
		Params    *map[string]interface{} `json:"params,omitempty"`
		SessionId *api.Slug               `json:"sessionId,omitempty"`
	}{Code: code, Level: level, SessionId: &sessionID}
	if params != nil {
		ev.Log.Params = &params
	}
	m.events.Publish(ev)
}

// Snapshot returns the status of every stored session, for a new admin
// event subscriber.
func (m *Manager) Snapshot(ctx context.Context) ([]api.SessionStatus, error) {
	list, err := m.opts.Sessions.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.SessionStatus, 0, len(list))
	for _, s := range list {
		out = append(out, m.status(s.Id))
	}
	return out, nil
}

// tick publishes the status of running sessions every StatusInterval, so
// the dashboard sees levels, viewers and latency move.
func (m *Manager) tick() {
	t := time.NewTicker(m.opts.StatusInterval)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
		}
		m.mu.Lock()
		runs := make([]*run, 0, len(m.runs))
		for _, r := range m.runs {
			runs = append(runs, r)
		}
		m.mu.Unlock()
		for _, r := range runs {
			st := r.status()
			m.events.Publish(api.AdminEvent{Type: api.AdminEventTypeSessionStatus, At: m.opts.Clock.Now(), Status: &st, SessionId: &st.SessionId})
		}
	}
}

// Close stops every running session (without waiting for flushes beyond
// StopTimeout) and the status ticker.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	close(m.done)
	ids := make([]string, 0, len(m.runs))
	for id := range m.runs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() { _, _ = m.Stop(context.Background(), id) })
	}
	wg.Wait()
	m.events.Close()
}
