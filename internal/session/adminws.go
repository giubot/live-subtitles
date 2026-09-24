// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
)

const (
	adminPingInterval = 20 * time.Second
	adminWriteTimeout = 10 * time.Second
)

// AdminHandler serves GET /ws/admin (ADM-1): a JSON text frame per
// AdminEvent. A new connection first gets the status of every session.
// Authorization happens before it, in the API middleware.
//
// When the subscriber falls behind, the connection is closed with status
// 1013 so the client reconnects and gets a fresh snapshot.
func (m *Manager) AdminHandler(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return // Accept wrote the HTTP error
		}
		defer func() { _ = c.CloseNow() }()
		ctx := c.CloseRead(r.Context())

		// Subscribe before the snapshot so no change falls in between.
		events := m.events.Subscribe(ctx)
		snapshot, err := m.Snapshot(ctx)
		if err != nil {
			log.ErrorContext(ctx, "admin events: snapshot", "err", err)
			_ = c.Close(websocket.StatusInternalError, "snapshot failed")
			return
		}
		now := m.opts.Clock.Now()
		for i := range snapshot {
			st := snapshot[i]
			if err := writeEvent(ctx, c, api.AdminEvent{Type: api.AdminEventTypeSessionStatus, At: now, Status: &st, SessionId: &st.SessionId}); err != nil {
				return
			}
		}

		ping := time.NewTicker(adminPingInterval)
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = c.Close(websocket.StatusGoingAway, "")
				return
			case ev, ok := <-events:
				if !ok {
					if ctx.Err() == nil {
						_ = c.Close(websocket.StatusTryAgainLater, "resubscribe")
					}
					return
				}
				if err := writeEvent(ctx, c, ev); err != nil {
					log.DebugContext(ctx, "admin events: write failed", "err", err)
					return
				}
			case <-ping.C:
				pctx, cancel := context.WithTimeout(ctx, adminWriteTimeout)
				err := c.Ping(pctx)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}
}

func writeEvent(ctx context.Context, c *websocket.Conn, ev api.AdminEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, adminWriteTimeout)
	defer cancel()
	return c.Write(ctx, websocket.MessageText, b)
}
