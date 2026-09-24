// SPDX-License-Identifier: Apache-2.0

package bus

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Defaults for the CaptionsHandler options.
const (
	DefaultPingInterval = 20 * time.Second
	DefaultWriteTimeout = 10 * time.Second
	// MaxHistory is the largest `history` query value the spec allows.
	MaxHistory = 500
)

// HandlerOption configures a CaptionsHandler.
type HandlerOption func(*CaptionsHandler)

// WithSessionLookup makes the handler answer 404 session.not_found before
// upgrading when lookup returns domain.ErrNotFound (any other error is a
// 500). Without it every session id is accepted.
func WithSessionLookup(lookup func(ctx context.Context, sessionID string) error) HandlerOption {
	return func(h *CaptionsHandler) { h.lookup = lookup }
}

// WithOriginPatterns allows cross-origin WebSocket handshakes from hosts
// matching these patterns (see websocket.AcceptOptions.OriginPatterns).
// Same-origin requests are always allowed.
func WithOriginPatterns(patterns ...string) HandlerOption {
	return func(h *CaptionsHandler) { h.origins = patterns }
}

// WithPingInterval sets how often the server pings an idle client.
func WithPingInterval(d time.Duration) HandlerOption {
	return func(h *CaptionsHandler) {
		if d > 0 {
			h.pingInterval = d
		}
	}
}

// WithWriteTimeout bounds each frame write and ping round trip.
func WithWriteTimeout(d time.Duration) HandlerOption {
	return func(h *CaptionsHandler) {
		if d > 0 {
			h.writeTimeout = d
		}
	}
}

// CaptionsHandler serves GET /ws/captions/{sessionId}: it subscribes to the
// Bus and writes each message as a JSON text frame (CaptionsServerMessage).
// Query: `lang` (required, repeatable) picks the tracks, `history` (0–500,
// default 50) the number of recent finals per track sent on connect.
//
// Client frames are ignored. When the subscriber is dropped for falling
// behind, or its session is closed, the connection is closed with status
// 1013 (try again later) so the client reconnects and gets fresh history.
type CaptionsHandler struct {
	bus          *Bus
	log          *slog.Logger
	lookup       func(ctx context.Context, sessionID string) error
	origins      []string
	pingInterval time.Duration
	writeTimeout time.Duration
}

// NewCaptionsHandler returns the /ws/captions handler. Mount it with a
// `{sessionId}` wildcard, e.g.
//
//	mux.Handle("GET /ws/captions/{sessionId}", bus.NewCaptionsHandler(b, log))
//
// or call Serve from a generated api.ServerInterface.WsCaptions.
func NewCaptionsHandler(b *Bus, log *slog.Logger, opts ...HandlerOption) *CaptionsHandler {
	h := &CaptionsHandler{
		bus:          b,
		log:          log,
		pingInterval: DefaultPingInterval,
		writeTimeout: DefaultWriteTimeout,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// ServeHTTP parses the path and query and calls Serve.
func (h *CaptionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionId")
	if id == "" { // not mounted with a wildcard: take the last path segment
		id = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	}
	params := api.WsCaptionsParams{Lang: r.URL.Query()["lang"]}
	if v := r.URL.Query().Get("history"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "request.invalid", "history must be an integer")
			return
		}
		params.History = &n
	}
	h.Serve(w, r, id, params)
}

// Serve upgrades the request and streams the session's captions until the
// client goes away, the request context ends or the subscriber is dropped.
// Its signature matches api.ServerInterface.WsCaptions.
func (h *CaptionsHandler) Serve(w http.ResponseWriter, r *http.Request, sessionID string, params api.WsCaptionsParams) {
	tracks := make([]string, 0, len(params.Lang))
	for _, l := range params.Lang {
		if l != "" {
			tracks = append(tracks, l)
		}
	}
	if sessionID == "" || len(tracks) == 0 {
		writeError(w, http.StatusBadRequest, "request.invalid", "sessionId and at least one lang are required")
		return
	}
	history := DefaultHistory
	if params.History != nil {
		history = *params.History
		if history < 0 || history > MaxHistory {
			writeError(w, http.StatusBadRequest, "request.invalid", "history must be between 0 and 500")
			return
		}
	}
	if h.lookup != nil {
		if err := h.lookup(r.Context(), sessionID); errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session.not_found", "session not found")
			return
		} else if err != nil {
			h.log.ErrorContext(r.Context(), "captions: session lookup failed", "session", sessionID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.origins})
	if err != nil {
		// Accept has already written the HTTP error response.
		h.log.DebugContext(r.Context(), "captions: upgrade failed", "session", sessionID, "err", err)
		return
	}
	defer func() { _ = c.CloseNow() }()

	// CloseRead reads (and discards) client frames so control frames such
	// as pongs and close are handled; ctx ends when the client goes away.
	ctx := c.CloseRead(r.Context())
	msgs := h.bus.SubscribeHistory(ctx, sessionID, tracks, history)
	ping := time.NewTicker(h.pingInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = c.Close(websocket.StatusGoingAway, "")
			return
		case msg, ok := <-msgs:
			if !ok {
				if ctx.Err() == nil {
					_ = c.Close(websocket.StatusTryAgainLater, "resubscribe")
				}
				return
			}
			if err := h.write(ctx, c, msg); err != nil {
				h.log.DebugContext(ctx, "captions: write failed", "session", sessionID, "err", err)
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, h.writeTimeout)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				h.log.DebugContext(ctx, "captions: ping failed", "session", sessionID, "err", err)
				return
			}
		}
	}
}

func (h *CaptionsHandler) write(ctx context.Context, c *websocket.Conn, msg domain.BusMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, h.writeTimeout)
	defer cancel()
	return c.Write(ctx, websocket.MessageText, b)
}

// writeError writes an api.Error with a translatable code (UI-4), like
// handlers.WriteError (not imported to keep bus free of handler deps).
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Code: code, Message: message})
}
