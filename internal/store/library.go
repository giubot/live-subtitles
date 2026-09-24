// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Glossaries, overlay presets and recordings: plain CRUD over JSON documents.
// Create returns domain.ErrConflict for a taken id; Get, Update and Delete
// return domain.ErrNotFound for a missing one. Ids and timestamps are set by
// the caller.

// CreateGlossary inserts g.
func (s *Store) CreateGlossary(ctx context.Context, g domain.Glossary) error {
	data, err := marshal(g)
	if err != nil {
		return err
	}
	return s.insert(ctx, `INSERT INTO glossaries (id, name, data) VALUES (?, ?, ?)
		ON CONFLICT (id) DO NOTHING`, g.Id, g.Name, data)
}

// GetGlossary returns the glossary with id.
func (s *Store) GetGlossary(ctx context.Context, id string) (domain.Glossary, error) {
	return getDoc[domain.Glossary](ctx, s, `SELECT data FROM glossaries WHERE id = ?`, id)
}

// ListGlossaries returns every glossary ordered by name.
func (s *Store) ListGlossaries(ctx context.Context) ([]domain.Glossary, error) {
	return listDocs[domain.Glossary](ctx, s, `SELECT data FROM glossaries ORDER BY name, id`)
}

// UpdateGlossary replaces the glossary with the same id.
func (s *Store) UpdateGlossary(ctx context.Context, g domain.Glossary) error {
	data, err := marshal(g)
	if err != nil {
		return err
	}
	return s.execOne(ctx, `UPDATE glossaries SET name = ?, data = ? WHERE id = ?`, g.Name, data, g.Id)
}

// DeleteGlossary removes the glossary with id.
func (s *Store) DeleteGlossary(ctx context.Context, id string) error {
	return s.execOne(ctx, `DELETE FROM glossaries WHERE id = ?`, id)
}

// CreateOverlayPreset inserts p.
func (s *Store) CreateOverlayPreset(ctx context.Context, p api.OverlayPreset) error {
	data, err := marshal(p)
	if err != nil {
		return err
	}
	return s.insert(ctx, `INSERT INTO overlay_presets (id, name, data) VALUES (?, ?, ?)
		ON CONFLICT (id) DO NOTHING`, p.Id, p.Name, data)
}

// GetOverlayPreset returns the preset with id.
func (s *Store) GetOverlayPreset(ctx context.Context, id string) (api.OverlayPreset, error) {
	return getDoc[api.OverlayPreset](ctx, s, `SELECT data FROM overlay_presets WHERE id = ?`, id)
}

// ListOverlayPresets returns every preset ordered by name.
func (s *Store) ListOverlayPresets(ctx context.Context) ([]api.OverlayPreset, error) {
	return listDocs[api.OverlayPreset](ctx, s, `SELECT data FROM overlay_presets ORDER BY name, id`)
}

// UpdateOverlayPreset replaces the preset with the same id.
func (s *Store) UpdateOverlayPreset(ctx context.Context, p api.OverlayPreset) error {
	data, err := marshal(p)
	if err != nil {
		return err
	}
	return s.execOne(ctx, `UPDATE overlay_presets SET name = ?, data = ? WHERE id = ?`, p.Name, data, p.Id)
}

// DeleteOverlayPreset removes the preset with id.
func (s *Store) DeleteOverlayPreset(ctx context.Context, id string) error {
	return s.execOne(ctx, `DELETE FROM overlay_presets WHERE id = ?`, id)
}

// CreateRecording inserts r.
func (s *Store) CreateRecording(ctx context.Context, r domain.Recording) error {
	data, err := marshal(r)
	if err != nil {
		return err
	}
	return s.insert(ctx, `INSERT INTO recordings (id, session_id, started_at, data) VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`, r.Id, r.SessionId, r.StartedAt.UnixNano(), data)
}

// GetRecording returns the recording with id.
func (s *Store) GetRecording(ctx context.Context, id string) (domain.Recording, error) {
	return getDoc[domain.Recording](ctx, s, `SELECT data FROM recordings WHERE id = ?`, id)
}

// ListRecordings returns the session's recordings, newest first; an empty
// sessionID lists all of them.
func (s *Store) ListRecordings(ctx context.Context, sessionID string) ([]domain.Recording, error) {
	return listDocs[domain.Recording](ctx, s, `SELECT data FROM recordings
		WHERE ? = '' OR session_id = ? ORDER BY started_at DESC, id`, sessionID, sessionID)
}

// UpdateRecording replaces the recording with the same id.
func (s *Store) UpdateRecording(ctx context.Context, r domain.Recording) error {
	data, err := marshal(r)
	if err != nil {
		return err
	}
	return s.execOne(ctx, `UPDATE recordings SET session_id = ?, started_at = ?, data = ? WHERE id = ?`,
		r.SessionId, r.StartedAt.UnixNano(), data, r.Id)
}

// DeleteRecording removes the recording with id (not its audio file).
func (s *Store) DeleteRecording(ctx context.Context, id string) error {
	return s.execOne(ctx, `DELETE FROM recordings WHERE id = ?`, id)
}
