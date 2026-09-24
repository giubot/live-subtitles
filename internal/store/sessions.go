// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// Sessions are stored as given; the caller sets timestamps, state and URLs.

// CreateSession inserts s; domain.ErrConflict if its id is taken.
func (s *Store) CreateSession(ctx context.Context, sess domain.Session) error {
	data, err := marshal(sess)
	if err != nil {
		return err
	}
	return s.insert(ctx, `INSERT INTO sessions (id, created_at, data) VALUES (?, ?, ?)
		ON CONFLICT (id) DO NOTHING`, sess.Id, sess.CreatedAt.UnixNano(), data)
}

// GetSession returns the session with id, or domain.ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (domain.Session, error) {
	return getDoc[domain.Session](ctx, s, `SELECT data FROM sessions WHERE id = ?`, id)
}

// ListSessions returns every session, oldest first.
func (s *Store) ListSessions(ctx context.Context) ([]domain.Session, error) {
	return listDocs[domain.Session](ctx, s, `SELECT data FROM sessions ORDER BY created_at, id`)
}

// UpdateSession replaces the stored session with the same id, or returns
// domain.ErrNotFound. The ingest token hash is kept.
func (s *Store) UpdateSession(ctx context.Context, sess domain.Session) error {
	data, err := marshal(sess)
	if err != nil {
		return err
	}
	return s.execOne(ctx, `UPDATE sessions SET created_at = ?, data = ? WHERE id = ?`,
		sess.CreatedAt.UnixNano(), data, sess.Id)
}

// DeleteSession removes the session and its captions, or returns
// domain.ErrNotFound. Recordings are kept.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.execOne(ctx, `DELETE FROM sessions WHERE id = ?`, id)
}

// IngestTokenHash returns the session's ingest token hash ("" if none was
// set), or domain.ErrNotFound if the session doesn't exist.
func (s *Store) IngestTokenHash(ctx context.Context, sessionID string) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT ingest_token_hash FROM sessions WHERE id = ?`, sessionID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return h, err
}

// SetIngestTokenHash replaces the session's ingest token hash, or returns
// domain.ErrNotFound if the session doesn't exist.
func (s *Store) SetIngestTokenHash(ctx context.Context, sessionID, hash string) error {
	return s.execOne(ctx, `UPDATE sessions SET ingest_token_hash = ? WHERE id = ?`, hash, sessionID)
}
