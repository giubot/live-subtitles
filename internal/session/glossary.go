// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// glossary loads the session's glossary (AI-7) for a new run: the ASR
// stream and every translation of the run use this copy, so an edit
// applies from the next start. The settings default was copied into the
// session when it was created, so a session without one runs without a
// glossary. A glossary that can't be read is logged and skipped rather
// than failing the start.
func (m *Manager) glossary(ctx context.Context, sess domain.Session) *domain.Glossary {
	if m.opts.Glossaries == nil || sess.GlossaryId == nil || *sess.GlossaryId == "" {
		return nil
	}
	g, err := m.opts.Glossaries.GetGlossary(ctx, *sess.GlossaryId)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			m.log.Warn("session glossary not found; running without it", "session", sess.Id, "glossary", *sess.GlossaryId)
		} else {
			m.log.Warn("read session glossary; running without it", "session", sess.Id, "glossary", *sess.GlossaryId, "err", err)
		}
		return nil
	}
	m.log.Info("session glossary loaded", "session", sess.Id, "glossary", g.Id,
		"terms", len(g.Terms), "do_not_translate", len(g.DoNotTranslate))
	return &g
}
