// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Settings returns the stored settings, or domain.ErrNotFound before the
// first PutSettings (the caller then applies defaults).
func (s *Store) Settings(ctx context.Context) (api.Settings, error) {
	return getDoc[api.Settings](ctx, s, `SELECT data FROM settings WHERE id = 1`)
}

// PutSettings replaces the stored settings.
func (s *Store) PutSettings(ctx context.Context, v api.Settings) error {
	data, err := marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (id, data) VALUES (1, ?)
		ON CONFLICT (id) DO UPDATE SET data = excluded.data`, data)
	return err
}

// AdminPINHash returns the admin PIN hash, or domain.ErrNotFound before
// setup.
func (s *Store) AdminPINHash(ctx context.Context) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT pin_hash FROM admin WHERE id = 1`).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return h, err
}

// InitAdminPINHash stores the first admin PIN hash, or returns
// domain.ErrConflict if one is already set (setup ran). It's atomic, so two
// concurrent setups can't both win.
func (s *Store) InitAdminPINHash(ctx context.Context, hash string) error {
	return s.insert(ctx, `INSERT INTO admin (id, pin_hash) VALUES (1, ?) ON CONFLICT (id) DO NOTHING`, hash)
}

// SetAdminPINHash stores (or replaces) the admin PIN hash.
func (s *Store) SetAdminPINHash(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin (id, pin_hash) VALUES (1, ?)
		ON CONFLICT (id) DO UPDATE SET pin_hash = excluded.pin_hash`, hash)
	return err
}
