// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/translate"
)

// newFanout starts the translation of the run's source track into its
// target languages (internal/translate). Translated captions go through
// the same publish path as the source track.
func (r *run) newFanout(ctx context.Context) *translate.Fanout {
	return translate.New(r.ctx, translate.Options{
		SessionID:        r.id,
		Translator:       r.translator,
		Targets:          r.sess.TargetLanguages,
		ContextSentences: r.m.contextSentences(ctx),
		Glossary:         r.glossary,
		Timeout:          r.m.opts.TranslateTimeout,
		Logger:           r.m.log,
		Publish: func(c api.Caption) {
			c.LatencyMs = r.latencyMs(secondsToDuration(c.End))
			r.publish(c)
		},
		Usage: r.addTranslationUsage,
		Failed: func(lang domain.LanguageCode, _ api.Caption, err error) {
			r.fail(api.Error{Code: CodeTranslationFailed, Message: err.Error(), Params: &map[string]any{"lang": lang}})
			r.m.logEvent(api.AdminEventLogLevelWarn, CodeTranslationFailed, r.id, map[string]any{"lang": lang})
		},
	})
}

// contextSentences is Settings.translation.contextSentences, else
// Options.ContextSentences.
func (m *Manager) contextSentences(ctx context.Context) int {
	if m.opts.Settings == nil {
		return m.opts.ContextSentences
	}
	st, err := m.opts.Settings.Settings(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			m.log.Warn("read settings; using the default translation context", "err", err)
		}
		return m.opts.ContextSentences
	}
	if st.Translation == nil || st.Translation.ContextSentences == nil {
		return m.opts.ContextSentences
	}
	return translate.ContextSentences(st)
}
