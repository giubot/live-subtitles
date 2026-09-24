// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/domain"
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

	// Auth checks admin credentials and ingest tokens; nil: setup and auth
	// operations answer 501 and admin operations are not protected.
	Auth *auth.Service

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
	return api.HandlerWithOptions(strict, api.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []api.MiddlewareFunc{s.authorize(policy)},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			WriteError(w, http.StatusBadRequest, "request.invalid", err.Error())
		},
	})
}

// WriteError writes an api.Error with a translatable code (UI-4).
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Code: code, Message: message})
}
