// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// Admin login sessions, keyed by a hash of the cookie value (see
// internal/auth).

// CreateAdminSession records a login that is valid until expires.
func (s *Store) CreateAdminSession(ctx context.Context, tokenHash string, created, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_sessions (token_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		tokenHash, created.UnixNano(), expires.UnixNano())
	return err
}

// AdminSessionValid reports whether the login exists and hasn't expired at now.
func (s *Store) AdminSessionValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_sessions WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, now.UnixNano()).Scan(&n)
	return n > 0, err
}

// DeleteAdminSession removes one login (logout). A missing one is not an error.
func (s *Store) DeleteAdminSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteAdminSessions removes every login, e.g. after the PIN changes.
func (s *Store) DeleteAdminSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions`)
	return err
}

// PurgeAdminSessions removes the logins that expired before now.
func (s *Store) PurgeAdminSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at <= ?`, now.UnixNano())
	return err
}
