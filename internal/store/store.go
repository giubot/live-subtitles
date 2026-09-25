// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, keeps CGO_ENABLED=0 builds working

	"github.com/iencodev/live-subtitles/internal/domain"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var (
	_ domain.SessionStore  = (*Store)(nil)
	_ domain.CaptionStore  = (*Store)(nil)
	_ domain.SettingsStore = (*Store)(nil)
)

// Store is the SQLite database. It is safe for concurrent use: WAL lets
// readers run alongside the single writer, and busy_timeout makes writers
// wait for each other instead of failing.
type Store struct {
	db *sql.DB
}

// Every pooled connection gets these pragmas. _txlock=immediate takes the
// write lock at BEGIN, so two transactions never deadlock upgrading a read
// lock.
const dsnParams = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
	"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"

// Open opens (creating if needed) the database file at path and its parent
// directory, then applies pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("store: create dir: %w", err)
	}
	db, err := sql.Open("sqlite", path+dsnParams)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// migration is one embedded file named NNNN_description.sql.
type migration struct {
	version int
	name    string
}

func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("store: bad migration name %q", e.Name())
		}
		ms = append(ms, migration{version: v, name: e.Name()})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	// Two branches adding the same number would silently skip one of them.
	for i := 1; i < len(ms); i++ {
		if ms[i].version == ms[i-1].version {
			return nil, fmt.Errorf("store: migrations %q and %q share version %d", ms[i-1].name, ms[i].name, ms[i].version)
		}
	}
	return ms, nil
}

// migrate applies, in order, each migration newer than the recorded version.
// Each one runs in its own transaction together with its schema_migrations
// row, so a failure leaves the database at the previous version.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}
	ms, err := migrations()
	if err != nil {
		return err
	}
	for _, m := range ms {
		if err := s.apply(ctx, m); err != nil {
			return fmt.Errorf("store: migration %s: %w", m.name, err)
		}
	}
	return nil
}

func (s *Store) apply(ctx context.Context, m migration) error {
	body, err := migrationFS.ReadFile("migrations/" + m.name)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Checked inside the (immediate) transaction so concurrent Opens of the
	// same file don't both apply it.
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		m.version, time.Now().UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

// Helpers shared by the document tables (API object as JSON in `data`).

// insert runs an INSERT … ON CONFLICT DO NOTHING and maps "no row inserted"
// to domain.ErrConflict.
func (s *Store) insert(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	return requireRow(res, domain.ErrConflict)
}

// execOne runs an UPDATE or DELETE and maps "no row touched" to
// domain.ErrNotFound.
func (s *Store) execOne(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	return requireRow(res, domain.ErrNotFound)
}

func requireRow(res sql.Result, none error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return none
	}
	return nil
}

// getDoc decodes the single `data` column the query selects.
func getDoc[T any](ctx context.Context, s *Store, query string, args ...any) (T, error) {
	var v T
	var data string
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return v, domain.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal([]byte(data), &v)
}

// listDocs decodes the `data` column of every row the query selects. It
// returns an empty, non-nil slice when there are none.
func listDocs[T any](ctx context.Context, s *Store, query string, args ...any) ([]T, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []T{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
