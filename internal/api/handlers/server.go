// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/iencodev/live-subtitles/internal/api"
)

// Server implements api.StrictServerInterface. Operations without a handler
// yet fall through to api.Unimplemented (501 not_implemented).
type Server struct {
	api.Unimplemented
}

var _ api.StrictServerInterface = (*Server)(nil)

// New returns the API server.
func New() *Server { return &Server{} }

// Handler routes every operation of the spec onto mux.
func (s *Server) Handler(mux *http.ServeMux, log *slog.Logger) http.Handler {
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
		BaseRouter: mux,
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
