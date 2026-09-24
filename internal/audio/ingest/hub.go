// SPDX-License-Identifier: Apache-2.0

// Package ingest receives browser audio over WebSocket (/ws/ingest, AUD-1)
// and exposes it per session as a domain.AudioSource with level, silence and
// clipping metering (AUD-6).
//
// Protocol (see api/openapi.yaml, wsIngest):
//  1. GET /ws/ingest/{sessionId}?token=… — the token is checked before the
//     upgrade; a bad one gets a 401 JSON Error.
//  2. The client sends an IngestHello text frame (s16le, 16000 Hz, 1 channel);
//     the server answers {type: ready}.
//  3. The client sends binary s16le PCM of any length; it is re-chunked into
//     20 ms frames. The server sends {type: level} every LevelInterval.
//
// One connection per session is active: a new one replaces the old, which
// gets {type: error, error.code: ingest.replaced} and is closed.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// TokenVerifier checks the ingest token of a session. It returns nil when
// the token is valid, an error wrapping domain.ErrNotFound when the session
// doesn't exist (404), and any other error for a bad token (401).
type TokenVerifier func(ctx context.Context, sessionID, token string) error

// Error codes sent to ingest clients (UI-4).
const (
	CodeUnauthorized = "auth.invalid_token"
	CodeNotFound     = "session.not_found"
	CodeBadHello     = "ingest.bad_hello"
	CodeReplaced     = "ingest.replaced"
	CodeClosed       = "ingest.closed"
)

// StatusReplaced is the WebSocket close code of a replaced connection.
const StatusReplaced websocket.StatusCode = 4000

// Options tunes a Hub. Zero fields take the defaults.
type Options struct {
	Clock  domain.Clock
	Logger *slog.Logger
	Level  audio.LevelMeterConfig
	// LevelInterval is how often level messages go to the client (default 200 ms).
	LevelInterval time.Duration
	// BufferFrames is the jitter buffer size in 20 ms frames (default 50 = 1 s).
	BufferFrames int
	// HelloTimeout bounds the wait for IngestHello (default 5 s).
	HelloTimeout time.Duration
	// StallThreshold is how far behind real time a connection's audio may
	// fall before a frame gap is counted (default 500 ms).
	StallThreshold time.Duration
	// OriginPatterns are extra allowed Origin hosts (e.g. "localhost:5173"
	// for the Vite dev server); same-origin requests are always allowed.
	OriginPatterns []string
}

func (o Options) withDefaults() Options {
	if o.Clock == nil {
		o.Clock = domain.SystemClock{}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.LevelInterval <= 0 {
		o.LevelInterval = 200 * time.Millisecond
	}
	if o.BufferFrames <= 0 {
		o.BufferFrames = 50
	}
	if o.HelloTimeout <= 0 {
		o.HelloTimeout = 5 * time.Second
	}
	if o.StallThreshold <= 0 {
		o.StallThreshold = 500 * time.Millisecond
	}
	return o
}

// maxMessageBytes caps one binary message (1 s of audio is 32000 bytes).
const maxMessageBytes = 64 << 10

// Hub owns the ingest Source of every session and serves /ws/ingest.
type Hub struct {
	verify TokenVerifier
	opts   Options

	mu      sync.Mutex
	sources map[string]*Source
}

var _ http.Handler = (*Hub)(nil)

// NewHub returns a hub that authenticates connections with verify.
func NewHub(verify TokenVerifier, opts Options) *Hub {
	return &Hub{verify: verify, opts: opts.withDefaults(), sources: map[string]*Source{}}
}

// Source returns the session's source, creating it on first use. The
// session pipeline calls Start on it; clients may connect before or after.
func (h *Hub) Source(sessionID string) *Source {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sources[sessionID]
	if !ok {
		s = newSource(sessionID, &h.opts)
		h.sources[sessionID] = s
	}
	return s
}

// Status returns the session's audio status; Connected is false when no
// source exists yet.
func (h *Hub) Status(sessionID string) api.AudioStatus {
	h.mu.Lock()
	s, ok := h.sources[sessionID]
	h.mu.Unlock()
	if !ok {
		return api.AudioStatus{}
	}
	return s.Status()
}

// Remove closes the session's source (ending its frame stream and its
// connection) and forgets it; a later Source call starts a new timeline.
func (h *Hub) Remove(sessionID string) {
	h.mu.Lock()
	s, ok := h.sources[sessionID]
	delete(h.sources, sessionID)
	h.mu.Unlock()
	if ok {
		s.close()
	}
}

// Close removes every source.
func (h *Hub) Close() {
	h.mu.Lock()
	ids := make([]string, 0, len(h.sources))
	for id := range h.sources {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.Remove(id)
	}
}

// ServeHTTP serves GET /ws/ingest/{sessionId}?token=…; mount it on a
// pattern with a {sessionId} wildcard.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionId")
	if id == "" {
		id = r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
	}
	h.ServeIngest(w, r, id)
}

// ServeIngest serves one ingest connection for sessionID, for callers that
// extract the path parameter themselves (e.g. a generated router).
func (h *Hub) ServeIngest(w http.ResponseWriter, r *http.Request, sessionID string) {
	if err := h.verify(r.Context(), sessionID, r.URL.Query().Get("token")); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeHTTPError(w, http.StatusNotFound, CodeNotFound, "session not found")
			return
		}
		writeHTTPError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid ingest token")
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.opts.OriginPatterns})
	if err != nil {
		return // Accept already wrote the HTTP error
	}
	defer func() { _ = ws.CloseNow() }()
	ws.SetReadLimit(maxMessageBytes)
	log := h.opts.Logger.With("session", sessionID)

	hello, err := readHello(r.Context(), ws, h.opts.HelloTimeout)
	if err != nil {
		log.InfoContext(r.Context(), "ingest hello rejected", "err", err)
		sendError(r.Context(), ws, CodeBadHello, err.Error())
		_ = ws.Close(websocket.StatusPolicyViolation, CodeBadHello)
		return
	}
	kind := api.AudioSourceKindBrowser
	if hello.Source != nil {
		kind = *hello.Source
	}

	src := h.Source(sessionID)
	c := &conn{kicked: make(chan struct{})}
	old, err := src.attach(c, kind)
	if err != nil {
		sendError(r.Context(), ws, CodeClosed, "session source closed")
		_ = ws.Close(websocket.StatusGoingAway, CodeClosed)
		return
	}
	defer src.detach(c)
	if old != nil {
		close(old.kicked)
		log.InfoContext(r.Context(), "ingest connection replaced")
	}
	log.InfoContext(r.Context(), "ingest connected", "source", kind)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if err := send(ctx, ws, api.IngestServerMessage{Type: api.IngestServerMessageTypeReady}); err != nil {
		return
	}

	readDone := make(chan error, 1)
	go func() { readDone <- h.readLoop(ctx, ws, src, c) }()
	tick := time.NewTicker(h.opts.LevelInterval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if err := send(ctx, ws, levelMessage(src.Level())); err != nil {
				cancel()
				<-readDone
				return
			}
		case err := <-readDone:
			log.InfoContext(r.Context(), "ingest disconnected", "status", websocket.CloseStatus(err))
			return
		case <-c.kicked:
			sendError(ctx, ws, CodeReplaced, "replaced by a newer ingest connection")
			_ = ws.Close(StatusReplaced, CodeReplaced)
			<-readDone
			return
		case <-src.done:
			sendError(ctx, ws, CodeClosed, "session source closed")
			_ = ws.Close(websocket.StatusGoingAway, CodeClosed)
			<-readDone
			return
		}
	}
}

// conn is one ingest connection; its audio fields are guarded by Source.mu.
type conn struct {
	kicked   chan struct{} // closed when a newer connection replaces this one
	started  bool          // first audio received
	start    time.Time     // wall time the connection's first sample was captured
	received time.Duration // audio received on this connection
}

func (h *Hub) readLoop(ctx context.Context, ws *websocket.Conn, src *Source, c *conn) error {
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageBinary {
			src.write(c, data, h.opts.Clock.Now())
		}
		// Text frames after the hello are ignored (reserved for future control messages).
	}
}

func readHello(ctx context.Context, ws *websocket.Conn, timeout time.Duration) (api.IngestHello, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var hello api.IngestHello
	typ, data, err := ws.Read(ctx)
	if err != nil {
		return hello, fmt.Errorf("reading hello: %w", err)
	}
	if typ != websocket.MessageText {
		return hello, errors.New("first message must be an IngestHello text frame")
	}
	if err := json.Unmarshal(data, &hello); err != nil {
		return hello, fmt.Errorf("decoding hello: %w", err)
	}
	switch {
	case hello.Type != "hello":
		return hello, fmt.Errorf("type %q, want hello", hello.Type)
	case hello.Format != api.S16le:
		return hello, fmt.Errorf("format %q, want s16le", hello.Format)
	case hello.SampleRate != domain.SampleRate:
		return hello, fmt.Errorf("sampleRate %d, want %d", hello.SampleRate, domain.SampleRate)
	case hello.Channels != 1:
		return hello, fmt.Errorf("channels %d, want 1", hello.Channels)
	case hello.Source != nil && !hello.Source.Valid():
		return hello, fmt.Errorf("unknown source %q", *hello.Source)
	}
	return hello, nil
}

func levelMessage(l audio.Level) api.IngestServerMessage {
	rms, peak := float32(l.RMSDBFS), float32(l.PeakDBFS)
	return api.IngestServerMessage{
		Type:      api.IngestServerMessageTypeLevel,
		LevelDbfs: &rms,
		PeakDbfs:  &peak,
		Silent:    &l.Silent,
		Clipping:  &l.Clipping,
	}
}

const writeTimeout = 2 * time.Second

func send(ctx context.Context, ws *websocket.Conn, msg api.IngestServerMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return ws.Write(ctx, websocket.MessageText, data)
}

func sendError(ctx context.Context, ws *websocket.Conn, code, message string) {
	_ = send(ctx, ws, api.IngestServerMessage{
		Type:  api.IngestServerMessageTypeError,
		Error: &api.Error{Code: code, Message: message},
	})
}

// writeHTTPError mirrors handlers.WriteError; it is duplicated so the
// handlers package can import ingest without a cycle.
func writeHTTPError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Code: code, Message: message})
}
