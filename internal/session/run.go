// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/metrics"
	"github.com/iencodev/live-subtitles/internal/translate"
)

// run is one running session: the goroutines of its pipeline and its
// live state.
//
//	source ─frames─▶ feed ─▶ (recorder) ─▶ ASR ─events─▶ consume ─▶ bus + store (source track)
//	                                                        └─▶ translate.Fanout (one goroutine per target language) ─▶ bus + store
//
// Stopping cancels only the source: its channel closes, feed closes the
// ASR input, the provider flushes its last finals, and consume drains the
// translation fan-out before the run finishes. Cancelling ctx aborts all.
type run struct {
	m    *Manager
	id   string
	sess domain.Session

	// Set by launch before start.
	provider   domain.ProviderKind
	asr        domain.ASRProvider
	translator domain.Translator
	source     domain.AudioSource
	// offset places this run on the session clock (see Manager.clockOrigin).
	offset time.Duration
	// glossary is loaded at start and used by the ASR and the translators.
	glossary *domain.Glossary

	ctx        context.Context // the whole run
	cancel     context.CancelFunc
	srcCtx     context.Context // the source only (graceful stop)
	stopSource context.CancelFunc
	done       chan struct{}
	doneOnce   sync.Once

	mu        sync.Mutex
	st        api.SessionState
	startedAt time.Time
	detected  domain.LanguageCode
	pending   langVote  // evidence for switching detected (language.go)
	err       api.Error // last error, shown in the status
	fatal     bool      // err ended the run
	// Usage reported during this run, by speech recognition and by the
	// translators: they run on different models and are priced apart.
	asrUsage         domain.Usage
	translationUsage domain.Usage
	sent             time.Duration // audio sent to the provider
	prior            totals        // usage of the session's earlier runs
	latency          map[string]*metrics.Latency
	arrived          arrivals      // when each frame arrived, for latency
	base             time.Duration // source T of the first frame
	hasBase          bool
	end              time.Duration // session clock at the end of the last frame
	sourceEnded      bool
	// recording is the run's recording while it records (status.recordingId).
	recording domain.RecordingSink
}

func newRun(m *Manager, sess domain.Session) *run {
	ctx, cancel := context.WithCancel(context.Background())
	srcCtx, stopSource := context.WithCancel(ctx)
	return &run{
		m: m, id: sess.Id, sess: sess,
		ctx: ctx, cancel: cancel, srcCtx: srcCtx, stopSource: stopSource,
		done:    make(chan struct{}),
		st:      api.SessionStateStarting,
		latency: map[string]*metrics.Latency{},
	}
}

// start opens the source and the ASR stream and launches the pipeline.
// On error the run is aborted (done is closed) and r.err says why.
func (r *run) start(ctx context.Context) error {
	frames, err := r.source.Start(r.srcCtx)
	if err != nil {
		r.fail(api.Error{Code: CodeSourceUnavailable, Message: err.Error(),
			Params: &map[string]any{"source": r.source.Kind()}})
		r.abort()
		return fmt.Errorf("%w: source: %v", ErrUnavailable, err)
	}
	r.glossary = r.m.glossary(ctx, r.sess)
	in, out, err := r.asr.Start(r.ctx, domain.ASRConfig{SessionID: r.id, SourceLanguage: r.sess.SourceLanguage, Glossary: r.glossary})
	if err != nil {
		e := api.Error{Code: CodeProviderUnavailable, Message: err.Error(),
			Params: &map[string]any{"provider": r.provider}}
		if coded := (*domain.CodedError)(nil); errors.As(err, &coded) {
			e.Code = coded.Code
			maps.Copy(*e.Params, coded.Params)
		}
		r.fail(e)
		r.abort()
		return fmt.Errorf("%w: provider %s: %v", ErrUnavailable, r.provider, err)
	}
	var sink domain.RecordingSink
	if rec := r.m.opts.Recorder; rec != nil && r.sess.RecordingEnabled {
		if sink, err = rec.Start(ctx, r.id); err != nil {
			r.m.log.Warn("recording did not start", "session", r.id, "err", err)
			sink = nil
		}
	}

	fan := r.newFanout(ctx)
	go r.feed(frames, in, sink)
	go r.consume(out, fan)
	return nil
}

// abort ends a run that never got its pipeline going.
func (r *run) abort() {
	r.cancel()
	r.doneOnce.Do(func() { close(r.done) })
}

// feed forwards source frames to the recorder and the ASR, on the session
// clock, dropping them while paused. It closes the ASR input when the
// source ends.
func (r *run) feed(frames <-chan domain.AudioFrame, in chan<- domain.AudioFrame, sink domain.RecordingSink) {
	defer close(in)
	if sink != nil {
		r.mu.Lock()
		r.recording = sink
		r.mu.Unlock()
		defer func() {
			if sink == nil { // it failed and was closed already
				return
			}
			if err := sink.Close(); err != nil {
				r.m.log.Warn("recording did not close cleanly", "session", r.id, "err", err)
			}
		}()
	}
	for f := range frames {
		now := r.m.opts.Clock.Now()
		r.mu.Lock()
		if !r.hasBase {
			r.base, r.hasBase = f.T, true
		}
		f = domain.AudioFrame{PCM: f.PCM, T: r.offset + f.T - r.base}
		r.end = f.End()
		r.arrived.add(f.End(), now)
		paused := r.st == api.SessionStatePaused
		if !paused {
			r.sent += f.End() - f.T
		}
		r.mu.Unlock()
		if paused {
			continue
		}
		if sink != nil {
			if err := sink.Write(f); err != nil {
				r.m.log.Warn("recording stopped", "session", r.id, "err", err)
				_ = sink.Close()
				sink = nil
				r.mu.Lock()
				r.recording = nil
				r.mu.Unlock()
			}
		}
		select {
		case in <- f:
		case <-r.ctx.Done():
			return
		}
	}
	r.mu.Lock()
	r.sourceEnded = true
	r.mu.Unlock()
	if err := r.source.Err(); err != nil && r.srcCtx.Err() == nil {
		r.failFatal(api.Error{Code: CodeSourceFailed, Message: err.Error(), Params: &map[string]any{"source": r.source.Kind()}})
		r.m.logEvent(api.AdminEventLogLevelError, CodeSourceFailed, r.id, map[string]any{"source": r.source.Kind()})
	}
}

// consume turns ASR events into source captions and hands them to the
// translation fan-out. When the ASR stream ends it drains the fan-out and
// finishes the run.
func (r *run) consume(out <-chan domain.ASREvent, fan *translate.Fanout) {
	defer r.finish()
	prefix := fmt.Sprintf("r%d-", r.offset/time.Second)
	for ev := range out {
		if ev.Err != nil {
			r.providerError(ev.Err)
			continue
		}
		r.addUsage(ev.Usage)
		text := ev.Text
		if text == "" {
			continue
		}
		lang := r.sourceLang(ev.Lang, ev.Final, text)
		c := api.Caption{
			SessionId:  r.id,
			Lang:       domain.SourceTrack,
			SegmentId:  prefix + ev.SegmentID,
			Final:      ev.Final,
			Text:       text,
			Start:      domain.Seconds(ev.Start),
			End:        domain.Seconds(ev.End),
			SourceLang: lang,
		}
		c.LatencyMs = r.latencyMs(ev.End)
		r.publish(c)
		fan.Push(c)
	}
	fan.Close()
	r.mu.Lock()
	crashed := !r.sourceEnded && r.srcCtx.Err() == nil && r.ctx.Err() == nil
	r.mu.Unlock()
	if crashed {
		// The provider closed its stream while audio was still coming
		// (P3-12 will restart it).
		r.failFatal(api.Error{Code: CodeProviderError, Message: "the provider stream ended unexpectedly",
			Params: &map[string]any{"provider": r.provider}})
		r.m.logEvent(api.AdminEventLogLevelError, CodeProviderError, r.id, map[string]any{"provider": r.provider})
	}
}

func (r *run) finish() {
	r.cancel()
	r.m.finished(r)
	r.doneOnce.Do(func() { close(r.done) })
}

// publish sends a caption to the bus and stores it when final.
func (r *run) publish(c api.Caption) {
	r.m.opts.Bus.Publish(r.id, domain.BusMessage{Type: api.CaptionsServerMessageTypeCaption, Caption: &c})
	if !c.Final {
		return
	}
	if c.LatencyMs != nil {
		r.mu.Lock()
		l := r.latency[c.Lang]
		if l == nil {
			l = &metrics.Latency{}
			r.latency[c.Lang] = l
		}
		l.Add(*c.LatencyMs)
		r.mu.Unlock()
		if o := r.m.opts.Observer; o != nil {
			o.CaptionLatency(r.provider, c.Lang, *c.LatencyMs)
		}
	}
	if st := r.m.opts.Captions; st != nil {
		// A stopped run still stores its last finals, so don't use r.ctx.
		if err := st.SaveCaption(context.WithoutCancel(r.ctx), c); err != nil {
			r.m.log.Error("store caption", "session", r.id, "track", c.Lang, "segment", c.SegmentId, "err", err)
		}
	}
}

// latencyMs is how long after the end of its audio (end, on the session
// clock) reached the server a caption is emitted now: recognition time for
// the source track, plus translation time for the others. Nil before any
// audio arrived.
func (r *run) latencyMs(end time.Duration) *int {
	now := r.m.opts.Clock.Now()
	r.mu.Lock()
	arrived, ok := r.arrived.at(end)
	r.mu.Unlock()
	if !ok {
		return nil
	}
	ms := int(max(0, now.Sub(arrived)).Milliseconds())
	return &ms
}

func (r *run) providerError(err error) {
	r.mu.Lock()
	r.err = api.Error{Code: CodeProviderError, Message: err.Error(), Params: &map[string]any{"provider": r.provider}}
	r.mu.Unlock()
	r.observeError(CodeProviderError)
	r.m.log.Warn("provider error", "session", r.id, "provider", r.provider, "err", err)
	r.m.logEvent(api.AdminEventLogLevelWarn, CodeProviderError, r.id, map[string]any{"provider": r.provider})
}

func (r *run) addUsage(u domain.Usage) {
	r.mu.Lock()
	r.asrUsage = r.asrUsage.Add(u)
	r.mu.Unlock()
}

func (r *run) addTranslationUsage(u domain.Usage) {
	r.mu.Lock()
	r.translationUsage = r.translationUsage.Add(u)
	r.mu.Unlock()
}

func (r *run) fail(e api.Error) {
	r.mu.Lock()
	r.err = e
	r.mu.Unlock()
	r.observeError(e.Code)
}

func (r *run) failFatal(e api.Error) {
	r.mu.Lock()
	r.err, r.fatal = e, true
	r.mu.Unlock()
	r.observeError(e.Code)
}

func (r *run) observeError(code string) {
	if o := r.m.opts.Observer; o != nil {
		o.SessionError(r.provider, code)
	}
}

func (r *run) lastError() api.Error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// failedRun reports whether the run ended because of an error.
func (r *run) failedRun() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fatal
}

func (r *run) state() api.SessionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st
}

func (r *run) setState(s api.SessionState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.st = s
	if s == api.SessionStateLive && r.startedAt.IsZero() {
		r.startedAt = r.m.opts.Clock.Now()
	}
}

// goLive moves a starting run to live; a Stop that came in meanwhile wins.
func (r *run) goLive() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.st == api.SessionStateStarting {
		r.st, r.startedAt = api.SessionStateLive, r.m.opts.Clock.Now()
	}
}

// clock is the session clock at the end of the audio seen so far.
func (r *run) clock() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return max(r.end, r.offset)
}

// audioStatuser is implemented by sources that meter their audio (ingest).
type audioStatuser interface {
	Status() api.AudioStatus
}

// srtStatuser is implemented by the SRT listener source (SRT-4).
type srtStatuser interface {
	SRTStats() api.SrtStats
}

func (r *run) status() api.SessionStatus {
	r.mu.Lock()
	src := r.source
	r.mu.Unlock()
	var audio api.AudioStatus
	if s, ok := src.(audioStatuser); ok {
		audio = s.Status()
	} else if src != nil {
		kind := src.Kind()
		audio = api.AudioStatus{Connected: true, Source: &kind}
	}
	var srt *api.SrtStats
	if s, ok := src.(srtStatuser); ok {
		v := s.SRTStats()
		srt = &v
	}
	viewers := r.m.opts.Bus.Viewers(r.id)

	r.mu.Lock()
	defer r.mu.Unlock()
	st := api.SessionStatus{SessionId: r.id, State: r.st, Viewers: viewers, Srt: srt}
	if r.provider != "" {
		p := r.provider
		st.Provider = &p
	}
	if !r.startedAt.IsZero() {
		t := r.startedAt
		st.StartedAt = &t
	}
	if r.detected != "" {
		d := r.detected
		st.DetectedLanguage = &d
	}
	if r.source != nil {
		st.Audio = &audio
	}
	if r.recording != nil {
		if id := r.recording.ID(); id != "" {
			st.RecordingId = &id
		}
	}
	st.Latency = r.latencyStats()
	st.Usage = r.totals().stats()
	if r.err.Code != "" {
		e := r.err
		st.Error = &e
	}
	return st
}

// latencyStats are the per-track latencies of the finals so far, nil
// before the first. Called with mu held.
func (r *run) latencyStats() *map[string]api.LatencyStats {
	if len(r.latency) == 0 {
		return nil
	}
	lat := make(map[string]api.LatencyStats, len(r.latency))
	for track, l := range r.latency {
		lat[track] = l.Stats()
	}
	return &lat
}

// totals is the session's usage including this run, with this run priced
// for its provider. Called with mu held.
func (r *run) totals() totals {
	asr := r.asrUsage
	// Audio is billed for what was sent, whether or not the provider reports it.
	asr.AudioSeconds = max(asr.AudioSeconds, r.sent.Seconds())
	return r.prior.add(asr.Add(r.translationUsage), r.m.pricing.Cost(r.provider, asr, r.translationUsage))
}

// finalStats are what the session shows once this run has ended.
func (r *run) finalStats() (totals, *map[string]api.LatencyStats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.totals(), r.latencyStats()
}

// errCanceled reports a context cancellation of the run itself (not a
// provider timeout).
func (r *run) errCanceled(err error) bool {
	return errors.Is(err, context.Canceled) && r.ctx.Err() != nil
}
