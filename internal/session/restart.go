// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/backoff"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Codes of automatic recovery (SES-5), in admin log events and, for the
// give-ups, in SessionStatus.error.
const (
	CodeProviderRestarting = "provider.restarting"
	CodeProviderRestarted  = "provider.restarted"
	// CodeProviderFailed: the provider stream kept crashing and the session
	// went to error.
	CodeProviderFailed   = "provider.failed"
	CodeSourceRestarting = "source.restarting"
	CodeSourceRestarted  = "source.restarted"
	// CodeAudioGap: audio that never reached the provider (a restart, or
	// the capture station reconnecting); captions mark it (gapBeforeMs).
	CodeAudioGap = "audio.gap"
)

// RestartPolicy configures the automatic restart of a crashed provider
// stream or a failed audio source (SES-5). Zero fields take the defaults.
type RestartPolicy struct {
	// MaxAttempts is how many restarts in a row may fail before the session
	// goes to error (default 5).
	MaxAttempts int
	// Backoff is the wait before each attempt (default 500 ms doubling to
	// 10 s, with 20 % jitter).
	Backoff backoff.Policy
	// HealthyAfter is how long a stream or source has to run for its next
	// crash to start counting attempts from 1 again (default 30 s).
	HealthyAfter time.Duration
}

func (p RestartPolicy) withDefaults() RestartPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 5
	}
	if p.Backoff.Base <= 0 {
		p.Backoff = backoff.Policy{Base: 500 * time.Millisecond, Max: 10 * time.Second, Jitter: 0.2}
	}
	if p.HealthyAfter <= 0 {
		p.HealthyAfter = 30 * time.Second
	}
	return p
}

// restartable is implemented by sources that say whether they may be
// started again after a failure: a file would replay from its beginning.
// Sources without it are restarted.
type restartable interface {
	Restartable() bool
}

// asrStream is one ASR stream of a run. A crashed stream is replaced by a
// new one; feed sends to whichever is attached (run.link).
type asrStream struct {
	in      chan<- domain.AudioFrame
	out     <-chan domain.ASREvent
	cancel  context.CancelFunc
	down    chan struct{} // closed when the stream has ended
	closed  bool          // in was closed; guarded by run.mu
	started time.Time
	prefix  string // of its segment IDs, unique within the session
}

// openASR starts an ASR stream with its own context, so a crashed one can
// be torn down without touching the run.
func (r *run) openASR() (*asrStream, error) {
	ctx, cancel := context.WithCancel(r.ctx)
	in, out, err := r.asr.Start(ctx, domain.ASRConfig{SessionID: r.id, SourceLanguage: r.sess.SourceLanguage, Glossary: r.glossary})
	if err != nil {
		cancel()
		return nil, err
	}
	return &asrStream{in: in, out: out, cancel: cancel, down: make(chan struct{}), started: r.m.opts.Clock.Now()}, nil
}

// providerStartError is the status error of a failed ASR Start: the
// provider's own code when it gives one, else provider.unavailable.
func (r *run) providerStartError(err error) api.Error {
	e := api.Error{Code: CodeProviderUnavailable, Message: err.Error(),
		Params: &map[string]any{"provider": r.provider}}
	if coded := (*domain.CodedError)(nil); errors.As(err, &coded) {
		e.Code = coded.Code
		maps.Copy(*e.Params, coded.Params)
	}
	return e
}

// attach makes st the stream feed sends to. If the source has ended
// already, its input is closed right away so it flushes and ends.
func (r *run) attach(st *asrStream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.link = st
	if r.sourceEnded {
		close(st.in)
		st.closed = true
	}
}

// detach tears down an ended stream.
func (r *run) detach(st *asrStream) {
	st.cancel()
	r.mu.Lock()
	if r.link == st {
		r.link = nil
	}
	r.mu.Unlock()
	close(st.down)
}

// endInput marks the end of the source: the attached stream's input is
// closed so the provider flushes its last finals.
func (r *run) endInput() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sourceEnded = true
	if st := r.link; st != nil && !st.closed {
		close(st.in)
		st.closed = true
	}
	close(r.ended)
}

// send hands a frame to the attached ASR stream. While there is none (the
// provider is restarting) the frame is lost and counted as a gap. It
// returns false when the run is cancelled.
func (r *run) send(f domain.AudioFrame) bool {
	r.mu.Lock()
	st := r.link
	if st == nil {
		r.lose(f)
		r.mu.Unlock()
		return true
	}
	g, gapped := r.resume(f.T)
	r.mu.Unlock()
	if gapped {
		r.logGap(g)
	}
	select {
	case st.in <- f:
		return true
	case <-st.down:
	case <-r.ctx.Done():
		return false
	}
	r.mu.Lock()
	r.lose(f)
	r.mu.Unlock()
	return true
}

// gap is audio the captions miss: dur of it, ending at at on the session clock.
type gap struct {
	at, dur time.Duration
}

// lose notes a frame that no provider got. Called with mu held.
func (r *run) lose(f domain.AudioFrame) {
	if !r.losing {
		r.losing, r.lostFrom = true, f.T
	}
}

// resume ends a stretch of lost frames at t. Called with mu held.
func (r *run) resume(t time.Duration) (gap, bool) {
	if !r.losing {
		return gap{}, false
	}
	r.losing = false
	return r.addGap(r.lostFrom, t)
}

// addGap records missing audio from..to for the next captions. Called
// with mu held.
func (r *run) addGap(from, to time.Duration) (gap, bool) {
	if to <= from {
		return gap{}, false
	}
	g := gap{at: to, dur: to - from}
	r.gaps = append(r.gaps, g)
	return g, true
}

func (r *run) logGap(g gap) {
	r.m.log.Warn("audio gap in the captions", "session", r.id, "at", g.at, "gap", g.dur)
	r.m.logEvent(api.AdminEventLogLevelWarn, CodeAudioGap, r.id, map[string]any{
		"gapMs": g.dur.Milliseconds(), "atSec": int(g.at / time.Second),
	})
}

// gapBefore is Caption.gapBeforeMs for a caption ending at end: the gaps
// before it that no caption has reported yet. A final reports them for
// good; interims only show them.
func (r *run) gapBefore(end time.Duration, final bool) *int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total time.Duration
	n := 0
	for ; n < len(r.gaps) && r.gaps[n].at < end; n++ {
		total += r.gaps[n].dur
	}
	if final {
		r.gaps = r.gaps[n:]
	}
	if total <= 0 {
		return nil
	}
	ms := int(total.Milliseconds())
	return &ms
}

func (r *run) setRecovering(rs *api.RecoveryStatus) {
	r.mu.Lock()
	r.recovering = rs
	r.mu.Unlock()
}

// wait sleeps d before a restart; false if the run stops or ends first.
func (r *run) wait(d time.Duration, ended <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-r.srcCtx.Done():
	case <-ended:
	}
	return false
}

// recovery tracks the restart attempts of one component.
type recovery struct {
	component api.RecoveryStatusComponent
	code      string // of the admin log event
	params    map[string]any
	since     time.Time
	cause     api.Error
}

// next counts the next attempt and publishes the recovery status; false
// when attempts are exhausted. attempts is the component's counter,
// guarded by mu.
func (r *run) next(rc *recovery, attempts *int) (int, time.Duration, bool) {
	pol := r.m.opts.Restart
	r.mu.Lock()
	*attempts++
	attempt := *attempts
	r.mu.Unlock()
	if attempt > pol.MaxAttempts {
		r.setRecovering(nil)
		return attempt, 0, false
	}
	delay := pol.Backoff.Delay(attempt)
	retryAt := r.m.opts.Clock.Now().Add(delay)
	cause := rc.cause
	r.setRecovering(&api.RecoveryStatus{
		Component: rc.component, Attempt: attempt, MaxAttempts: pol.MaxAttempts,
		Since: rc.since, RetryAt: &retryAt, Error: &cause,
	})
	params := maps.Clone(rc.params)
	params["attempt"], params["maxAttempts"], params["retryInMs"] = attempt, pol.MaxAttempts, delay.Milliseconds()
	r.m.logEvent(api.AdminEventLogLevelWarn, rc.code, r.id, params)
	r.m.changed(r)
	return attempt, delay, true
}

// restarted records a successful restart and publishes the status.
func (r *run) restarted(code string, params map[string]any, attempt int) int {
	r.mu.Lock()
	r.restarts++
	n := r.restarts
	r.recovering = nil
	r.mu.Unlock()
	params = maps.Clone(params)
	params["attempt"] = attempt
	r.m.logEvent(api.AdminEventLogLevelInfo, code, r.id, params)
	r.m.changed(r)
	return n
}

// crashed reports whether an ASR stream that ended did so on its own,
// while audio was still coming.
func (r *run) crashed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.sourceEnded && r.srcCtx.Err() == nil && r.ctx.Err() == nil
}

// restartProvider replaces a crashed ASR stream, with backoff, and returns
// the new one; nil when the run stops meanwhile or the attempts run out
// (then the run fails with provider.failed).
func (r *run) restartProvider(prev *asrStream) *asrStream {
	now := r.m.opts.Clock.Now()
	params := map[string]any{"provider": r.provider}
	rc := &recovery{component: api.RecoveryComponentProvider, code: CodeProviderRestarting, params: params, since: now,
		cause: api.Error{Code: CodeProviderError, Message: "the provider stream ended unexpectedly", Params: &map[string]any{"provider": r.provider}}}
	r.fail(rc.cause)
	r.m.log.Warn("provider stream crashed; restarting", "session", r.id, "provider", r.provider, "ran", now.Sub(prev.started))
	r.mu.Lock()
	if now.Sub(prev.started) >= r.m.opts.Restart.HealthyAfter {
		r.provAttempts = 0
	}
	r.mu.Unlock()
	for {
		attempt, delay, ok := r.next(rc, &r.provAttempts)
		if !ok {
			n := attempt - 1
			r.failFatal(api.Error{Code: CodeProviderFailed,
				Message: fmt.Sprintf("the provider stream failed %d restarts in a row: %s", n, rc.cause.Message),
				Params:  &map[string]any{"provider": r.provider, "attempts": n}})
			r.m.log.Error("provider stream gave up", "session", r.id, "provider", r.provider, "attempts", n)
			r.m.logEvent(api.AdminEventLogLevelError, CodeProviderFailed, r.id, map[string]any{"provider": r.provider, "attempts": n})
			return nil
		}
		if !r.wait(delay, r.ended) {
			r.setRecovering(nil)
			return nil
		}
		st, err := r.openASR()
		if err != nil {
			rc.cause = r.providerStartError(err)
			r.fail(rc.cause)
			r.m.log.Warn("provider restart failed", "session", r.id, "provider", r.provider, "attempt", attempt, "err", err)
			continue
		}
		n := r.restarted(CodeProviderRestarted, params, attempt)
		// Providers number segments from 1 again: keep their IDs unique.
		st.prefix = fmt.Sprintf("%s%d-", r.prefix, n)
		r.m.log.Info("provider stream restarted", "session", r.id, "provider", r.provider, "attempt", attempt)
		r.attach(st)
		return st
	}
}

// restartSource starts a failed source again, with backoff, and returns
// its frames; nil when the run stops meanwhile, the source can't be
// restarted, or the attempts run out (then the run fails with
// source.failed).
func (r *run) restartSource(cause error) <-chan domain.AudioFrame {
	kind := r.source.Kind()
	failed := func(attempts int) {
		params := map[string]any{"source": kind}
		if attempts > 0 {
			params["attempts"] = attempts
		}
		p := maps.Clone(params)
		r.failFatal(api.Error{Code: CodeSourceFailed, Message: cause.Error(), Params: &p})
		r.m.logEvent(api.AdminEventLogLevelError, CodeSourceFailed, r.id, params)
	}
	if s, ok := r.source.(restartable); ok && !s.Restartable() {
		failed(0)
		return nil
	}
	now := r.m.opts.Clock.Now()
	params := map[string]any{"source": kind}
	rc := &recovery{component: api.RecoveryComponentSource, code: CodeSourceRestarting, params: params, since: now,
		cause: api.Error{Code: CodeSourceFailed, Message: cause.Error(), Params: &map[string]any{"source": kind}}}
	r.fail(rc.cause)
	r.m.log.Warn("audio source failed; restarting", "session", r.id, "source", kind, "err", cause)
	r.mu.Lock()
	if now.Sub(r.srcStarted) >= r.m.opts.Restart.HealthyAfter {
		r.srcAttempts = 0
	}
	r.mu.Unlock()
	for {
		attempt, delay, ok := r.next(rc, &r.srcAttempts)
		if !ok {
			r.m.log.Error("audio source gave up", "session", r.id, "source", kind, "attempts", attempt-1)
			failed(attempt - 1)
			return nil
		}
		if !r.wait(delay, nil) {
			r.setRecovering(nil)
			return nil
		}
		frames, err := r.source.Start(r.srcCtx)
		if err != nil {
			cause = err
			rc.cause = api.Error{Code: CodeSourceUnavailable, Message: err.Error(), Params: &map[string]any{"source": kind}}
			r.fail(rc.cause)
			r.m.log.Warn("audio source restart failed", "session", r.id, "source", kind, "attempt", attempt, "err", err)
			continue
		}
		// The new frames start their own timeline: place it after the audio
		// so far plus the wall time lost, so captions keep tracking real time.
		now := r.m.opts.Clock.Now()
		r.mu.Lock()
		from := max(r.end, r.offset)
		lost := time.Duration(0)
		if !r.lastFrameAt.IsZero() {
			lost = max(0, now.Sub(r.lastFrameAt)).Round(domain.FrameDuration)
		}
		r.offset, r.hasBase, r.srcStarted = from+lost, false, now
		g, gapped := r.addGap(from, from+lost)
		r.mu.Unlock()
		if gapped {
			r.logGap(g)
		}
		r.restarted(CodeSourceRestarted, params, attempt)
		r.m.log.Info("audio source restarted", "session", r.id, "source", kind, "attempt", attempt)
		return frames
	}
}
