// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/session"
)

// Server implements api.StrictServerInterface. Operations without a handler
// yet fall through to api.Unimplemented (501 not_implemented).
type Server struct {
	api.Unimplemented

	// Network describes the LAN addresses and base URL for QR codes.
	Network func() api.NetworkInfo

	// Secrets stores API keys and passwords; nil: secrets operations answer 501.
	Secrets domain.SecretStore

	// Sessions, Captions and Settings are the stores; nil: the operations
	// that need them answer 501.
	Sessions domain.SessionStore
	Captions domain.CaptionStore
	Settings domain.SettingsStore
	// OverlayPresets stores saved overlay presets; nil: only the built-ins
	// are listed and the write operations answer 501.
	OverlayPresets OverlayPresets

	// Manager runs sessions (start, pause, stop, status); nil: those
	// operations answer 501.
	Manager *session.Manager
	// Files opens file and URL test sources (sources/file); nil: 501.
	Files *ffmpeg.Files
	// Recordings lists, serves and deletes recordings; nil: the recordings
	// operations (and captions by recordingId) answer 501.
	Recordings Recordings

	// WebSocket endpoints need the raw connection, so they are served
	// outside the strict handler; nil: 501.
	CaptionsWS func(w http.ResponseWriter, r *http.Request, sessionID string, params api.WsCaptionsParams)
	IngestWS   func(w http.ResponseWriter, r *http.Request, sessionID string)
	AdminWS    http.HandlerFunc

	// Auth checks admin credentials and ingest tokens; nil: setup and auth
	// operations answer 501 and admin operations are not protected.
	Auth *auth.Service

	// TLS describes HTTPS and serves the local CA (tls.go); nil: 501.
	TLS TLSService

	log *slog.Logger
}

var _ api.StrictServerInterface = (*Server)(nil)

// New returns the API server.
func New() *Server { return &Server{} }

// Handler routes every operation of the spec onto mux, behind the admin
// authorization the spec declares.
func (s *Server) Handler(mux *http.ServeMux, log *slog.Logger) http.Handler {
	s.log = log
	policy, err := securityPolicy()
	if err != nil {
		panic(err) // the embedded spec is validated by tests; this can't fail at runtime
	}
	strict := api.NewStrictHandlerWithOptions(s, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			WriteError(w, http.StatusBadRequest, "request.invalid", err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, api.ErrNotImplemented) {
				WriteError(w, http.StatusNotImplemented, "not_implemented", "not implemented yet")
				return
			}
			log.ErrorContext(r.Context(), "handler failed", "method", r.Method, "path", r.URL.Path, "err", err)
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
		},
	})
	return api.HandlerWithOptions(websockets{strict, s}, api.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []api.MiddlewareFunc{s.authorize(policy)},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			WriteError(w, http.StatusBadRequest, "request.invalid", err.Error())
		},
	})
}

// websockets routes the WebSocket operations (and the recording audio,
// which needs the request for Range) to the Server's handlers and
// everything else to the strict handler.
type websockets struct {
	api.ServerInterface
	s *Server
}

func (ws websockets) WsCaptions(w http.ResponseWriter, r *http.Request, sessionID api.SessionId, params api.WsCaptionsParams) {
	if ws.s.CaptionsWS == nil {
		ws.ServerInterface.WsCaptions(w, r, sessionID, params)
		return
	}
	ws.s.CaptionsWS(w, r, sessionID, params)
}

func (ws websockets) WsIngest(w http.ResponseWriter, r *http.Request, sessionID api.SessionId) {
	if ws.s.IngestWS == nil {
		ws.ServerInterface.WsIngest(w, r, sessionID)
		return
	}
	ws.s.IngestWS(w, r, sessionID)
}

func (ws websockets) GetRecordingAudio(w http.ResponseWriter, r *http.Request, id api.RecordingId, params api.GetRecordingAudioParams) {
	if ws.s.Recordings == nil {
		ws.ServerInterface.GetRecordingAudio(w, r, id, params)
		return
	}
	ws.s.serveRecordingAudio(w, r, id)
}

func (ws websockets) WsAdmin(w http.ResponseWriter, r *http.Request) {
	if ws.s.AdminWS == nil {
		ws.ServerInterface.WsAdmin(w, r)
		return
	}
	ws.s.AdminWS(w, r)
}

// WriteError writes an api.Error with a translatable code (UI-4).
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Code: code, Message: message})
}
