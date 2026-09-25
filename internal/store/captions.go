// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// Caption page sizes, matching listCaptions' `limit` in the API.
const (
	DefaultCaptionLimit = 200
	MaxCaptionLimit     = 1000
)

// SaveCaption upserts c by (session, track, segment). The session must exist
// (domain.ErrNotFound otherwise).
func (s *Store) SaveCaption(ctx context.Context, c domain.CaptionEvent) error {
	var latency any
	if c.LatencyMs != nil {
		latency = *c.LatencyMs
	}
	// The EXISTS guard turns a missing session into zero rows (ErrNotFound)
	// rather than a foreign key error.
	return s.execOne(ctx, `INSERT INTO captions
		(session_id, track, segment_id, start_sec, end_sec, text, source_lang, final, edited, hidden, latency_ms)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM sessions WHERE id = ?)
		ON CONFLICT (session_id, track, segment_id) DO UPDATE SET
			start_sec = excluded.start_sec, end_sec = excluded.end_sec, text = excluded.text,
			source_lang = excluded.source_lang, final = excluded.final, edited = excluded.edited,
			hidden = excluded.hidden, latency_ms = excluded.latency_ms`,
		c.SessionId, c.Lang, c.SegmentId, float64(c.Start), float64(c.End), c.Text, c.SourceLang,
		c.Final, isTrue(c.Edited), isTrue(c.Hidden), latency, c.SessionId)
}

// captionCursor is the position after the last caption of a page, in
// (start, track, segment) order.
type captionCursor struct {
	Start   float64 `json:"s"`
	Track   string  `json:"t"`
	Segment string  `json:"g"`
}

func (c captionCursor) encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCaptionCursor(s string) (captionCursor, error) {
	var c captionCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err != nil {
		return c, fmt.Errorf("%w: caption cursor", domain.ErrInvalid)
	}
	return c, nil
}

// ListCaptions returns a page of the session's captions ordered by start
// time. An empty q.Track lists every track. next is empty on the last page.
// A malformed cursor returns domain.ErrInvalid.
func (s *Store) ListCaptions(ctx context.Context, q domain.CaptionQuery) ([]domain.CaptionEvent, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultCaptionLimit
	}
	limit = min(limit, MaxCaptionLimit)

	var where strings.Builder
	where.WriteString(`session_id = ?`)
	args := []any{q.SessionID}
	if q.Track != "" {
		where.WriteString(` AND track = ?`)
		args = append(args, q.Track)
	}
	if q.To > q.From {
		where.WriteString(` AND start_sec >= ? AND start_sec < ?`)
		args = append(args, q.From.Seconds(), q.To.Seconds())
	}
	if q.Cursor != "" {
		cur, err := decodeCaptionCursor(q.Cursor)
		if err != nil {
			return nil, "", err
		}
		where.WriteString(` AND (start_sec, track, segment_id) > (?, ?, ?)`)
		args = append(args, cur.Start, cur.Track, cur.Segment)
	}
	// One extra row tells whether there is a next page.
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, `SELECT session_id, track, segment_id, start_sec, end_sec, text,
		source_lang, final, edited, hidden, latency_ms FROM captions WHERE `+where.String()+`
		ORDER BY start_sec, track, segment_id LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()

	items := []domain.CaptionEvent{}
	var last captionCursor
	for rows.Next() {
		if len(items) == limit {
			return items, last.encode(), nil
		}
		var (
			c              domain.CaptionEvent
			start, end     float64
			edited, hidden bool
			latency        *int
		)
		if err := rows.Scan(&c.SessionId, &c.Lang, &c.SegmentId, &start, &end, &c.Text,
			&c.SourceLang, &c.Final, &edited, &hidden, &latency); err != nil {
			return nil, "", err
		}
		c.Start, c.End, c.LatencyMs = float32(start), float32(end), latency
		c.Edited, c.Hidden = truePtr(edited), truePtr(hidden)
		items = append(items, c)
		last = captionCursor{Start: start, Track: c.Lang, Segment: c.SegmentId}
	}
	return items, "", rows.Err()
}

func isTrue(b *bool) bool { return b != nil && *b }

// truePtr maps false to nil so unset flags are omitted from JSON.
func truePtr(b bool) *bool {
	if !b {
		return nil
	}
	return &b
}
