// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/translate"
)

// Codes of the provider fallback (AI-8): the admin log event of a switch,
// and the reasons in SessionStatus.fallback.reasonCode besides the
// provider's own codes (domain.CodeProviderQuotaExhausted,
// domain.CodeProviderAuthFailed) and provider.unavailable.
const (
	CodeProviderFallback = "provider.fallback"
	// CodeErrorsRepeated: FallbackPolicy.Errors errors within its Window.
	CodeErrorsRepeated = "provider.errors_repeated"
	// CodeRestartsFailed: the crashed stream failed
	// FallbackPolicy.AfterRestarts restarts in a row.
	CodeRestartsFailed = "provider.restarts_failed"
)

// FallbackPolicy configures when a running session switches to the other
// provider, Gemini ⇄ local (AI-8). It applies only while
// settings.providers.fallback is on. Zero fields take the defaults.
type FallbackPolicy struct {
	// Errors reported by the provider (stream errors and failed
	// translations) within Window make a switch (default 5 in 1 min). A
	// quota or key failure switches at once.
	Errors int
	Window time.Duration
	// AfterRestarts is how many restarts of a crashed stream may fail in a
	// row before the run switches instead (default 2; at most
	// RestartPolicy.MaxAttempts, so a give-up always tries the other
	// provider first).
	AfterRestarts int
	// Cooldown is the least time between two switches of a run (default
	// 10 min), so a run never flaps between providers. Stopping and
	// starting the session starts over on its own provider.
	Cooldown time.Duration
	// ProbeEvery rate-limits the availability checks of the other
	// provider after it was found unavailable (default 10 s).
	ProbeEvery time.Duration
	// ProbeTimeout bounds one availability check (default 5 s).
	ProbeTimeout time.Duration
}

func (p FallbackPolicy) withDefaults(restart RestartPolicy) FallbackPolicy {
	if p.Errors <= 0 {
		p.Errors = 5
	}
	if p.Window <= 0 {
		p.Window = time.Minute
	}
	if p.AfterRestarts <= 0 {
		p.AfterRestarts = 2
	}
	p.AfterRestarts = min(p.AfterRestarts, restart.MaxAttempts)
	if p.Cooldown <= 0 {
		p.Cooldown = 10 * time.Minute
	}
	if p.ProbeEvery <= 0 {
		p.ProbeEvery = 10 * time.Second
	}
	if p.ProbeTimeout <= 0 {
		p.ProbeTimeout = 5 * time.Second
	}
	return p
}

// alternate is the provider a run on kind falls back to; "" for none
// (mock never falls back and is never fallen back to).
func alternate(kind domain.ProviderKind) domain.ProviderKind {
	switch kind {
	case api.ProviderKindGemini:
		return api.ProviderKindLocal
	case api.ProviderKindLocal:
		return api.ProviderKindGemini
	}
	return ""
}

// hardFailure reports whether code is a failure retrying won't fix.
func hardFailure(code string) bool {
	return code == domain.CodeProviderQuotaExhausted || code == domain.CodeProviderAuthFailed
}

// fallbackReason is the reason code of a switch caused by cause: its own
// code for a quota or key failure, def otherwise.
func fallbackReason(cause api.Error, def string) string {
	if hardFailure(cause.Code) {
		return cause.Code
	}
	return def
}

// fallbackEnabled reads settings.providers.fallback (off by default).
func (m *Manager) fallbackEnabled(ctx context.Context) bool {
	if m.opts.Settings == nil {
		return false
	}
	st, err := m.opts.Settings.Settings(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			m.log.Warn("read settings; no provider fallback", "err", err)
		}
		return false
	}
	return st.Providers.Fallback != nil && *st.Providers.Fallback
}

// countFailure counts a provider error e toward FallbackPolicy.Errors and
// falls back when it is a quota or key failure or the count is reached.
// It reports whether the run switched.
func (r *run) countFailure(e api.Error) bool {
	pol := r.m.opts.Fallback
	now := r.m.opts.Clock.Now()
	r.mu.Lock()
	r.fbErrs = slices.DeleteFunc(r.fbErrs, func(t time.Time) bool { return now.Sub(t) >= pol.Window })
	r.fbErrs = append(r.fbErrs, now)
	n := len(r.fbErrs)
	r.mu.Unlock()
	if !hardFailure(e.Code) && n < pol.Errors {
		return false
	}
	return r.fallback(fallbackReason(e, CodeErrorsRepeated), e)
}

// wantFallback reports whether a restart loop failing with cause should
// try the other provider.
func (r *run) wantFallback(cause api.Error) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return hardFailure(cause.Code) || r.provAttempts >= r.m.opts.Fallback.AfterRestarts
}

// fallback switches the run to the other provider (AI-8) when
// settings.providers.fallback is on, the other provider is available and
// the run hasn't switched within FallbackPolicy.Cooldown. reason is the
// translatable reason and cause the error behind it. The current ASR
// stream is cancelled, and consume (or the restart loop, which it wakes)
// opens one on the new provider; the audio in between and what the old
// stream hadn't finalized is a gap in the captions. The translation
// fan-out uses the new translator from its next call. It reports whether
// the run switched; it is safe from any goroutine.
func (r *run) fallback(reason string, cause api.Error) bool {
	pol := r.m.opts.Fallback
	now := r.m.opts.Clock.Now()
	r.mu.Lock()
	from := r.provider
	to := alternate(from)
	if to == "" || r.fbBusy || r.sourceEnded || r.st == api.SessionStateStopping ||
		(r.fb != nil && now.Sub(r.fb.At) < pol.Cooldown) ||
		(!r.fbProbed.IsZero() && now.Sub(r.fbProbed) < pol.ProbeEvery) {
		r.mu.Unlock()
		return false
	}
	r.fbBusy = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.fbBusy = false
		r.mu.Unlock()
	}()

	if !r.m.fallbackEnabled(r.ctx) {
		return false
	}
	p, ok := r.m.opts.Providers[to]
	if !ok || p.ASR == nil || p.Translator == nil {
		return false
	}
	if avail := r.m.opts.FallbackAvailable; avail != nil {
		ctx, cancel := context.WithTimeout(r.ctx, pol.ProbeTimeout)
		ok, why := avail(ctx, to)
		cancel()
		if !ok {
			r.mu.Lock()
			r.fbProbed = r.m.opts.Clock.Now()
			r.mu.Unlock()
			r.m.log.Info("provider fallback not possible: the other provider is unavailable",
				"session", r.id, "provider", from, "fallback", to, "reason", reason, "unavailable", why)
			return false
		}
	}

	r.mu.Lock()
	if r.provider != from || r.sourceEnded {
		r.mu.Unlock()
		return false
	}
	// What the old provider used is priced for it; the new one counts afresh.
	r.prior = r.totals()
	r.asrUsage, r.translationUsage, r.sent = domain.Usage{}, domain.Usage{}, 0
	r.provider, r.asr, r.translator = to, p.ASR, p.Translator
	switches := 1
	if r.fb != nil {
		switches = r.fb.Switches + 1
	}
	c := cause
	r.fb = &api.ProviderFallback{From: from, To: to, At: now, ReasonCode: reason, Switches: switches, Error: &c}
	r.fbErrs, r.fbProbed = nil, time.Time{}
	r.err, r.provAttempts, r.recovering = api.Error{}, 0, nil
	// The old stream's audio that has no final yet is lost with it.
	var g gap
	var gapped bool
	if old := r.link; old != nil {
		r.link = nil
		g, gapped = r.addGap(max(r.lastFinal, old.from), r.end)
		old.cancel()
	}
	r.mu.Unlock()
	select {
	case r.kick <- struct{}{}:
	default:
	}

	r.m.log.Warn("provider fallback", "session", r.id, "from", from, "to", to, "reason", reason, "switches", switches, "cause", cause.Code)
	r.m.logEvent(api.AdminEventLogLevelWarn, CodeProviderFallback, r.id, map[string]any{
		"from": from, "to": to, "reason": reason,
	})
	if gapped {
		r.logGap(g)
	}
	if w, ok := p.Translator.(translate.Warmer); ok && len(r.sess.TargetLanguages) > 0 {
		go func() {
			if err := w.Warm(r.ctx); err != nil && r.ctx.Err() == nil {
				r.m.log.Warn("translator warm-up failed", "session", r.id, "provider", to, "err", err)
			}
		}()
	}
	r.m.changed(r)
	return true
}

// runTranslator is the translator the fan-out calls: the one of the
// provider the run is on at each call, so a fallback takes effect from
// the next caption.
type runTranslator struct{ r *run }

var (
	_ domain.StreamingTranslator = runTranslator{}
	_ translate.Warmer           = runTranslator{}
)

func (t runTranslator) current() domain.Translator {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	return t.r.translator
}

func (t runTranslator) Kind() domain.ProviderKind { return t.r.kind() }

func (t runTranslator) Translate(ctx context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	return t.current().Translate(ctx, req)
}

func (t runTranslator) TranslateStream(ctx context.Context, req domain.TranslateRequest, partial func(string)) (domain.TranslateResult, error) {
	cur := t.current()
	if st, ok := cur.(domain.StreamingTranslator); ok {
		return st.TranslateStream(ctx, req, partial)
	}
	return cur.Translate(ctx, req)
}

func (t runTranslator) Warm(ctx context.Context) error {
	if w, ok := t.current().(translate.Warmer); ok {
		return w.Warm(ctx)
	}
	return nil
}
