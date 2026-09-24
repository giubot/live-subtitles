// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/auth"
)

// access is who may call an operation, from its `security` in the spec.
type access int

const (
	// adminOnly: the global default (adminSession cookie or adminToken bearer).
	adminOnly access = iota
	// public: `security: []`.
	public
	// ingestTokenOnly: the handler checks the per-session ingest token itself.
	ingestTokenOnly
)

// securityPolicy maps each route pattern the generated router registers
// ("GET /api/sessions/{sessionId}") to its access, read from the embedded
// spec so the spec stays the only place security is declared.
func securityPolicy() (map[string]access, error) {
	doc, err := api.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load embedded spec: %w", err)
	}
	policy := map[string]access{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			policy[method+" "+path] = operationAccess(op.Security)
		}
	}
	return policy, nil
}

func operationAccess(sec *openapi3.SecurityRequirements) access {
	switch {
	case sec == nil:
		return adminOnly // inherits the top-level requirement
	case len(*sec) == 0:
		return public
	}
	for _, req := range *sec {
		if _, ok := req["ingestToken"]; !ok || len(req) != 1 {
			return adminOnly
		}
	}
	return ingestTokenOnly
}

// authorize runs before every generated route. It records the caller in
// the request context (auth.RequestFrom) and answers 401 auth.required to
// admin operations without valid credentials. Without an Auth service every
// operation is open, which only handler unit tests rely on.
func (s *Server) authorize(policy map[string]access) api.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.Auth == nil {
				next.ServeHTTP(w, r)
				return
			}
			admin, err := s.Auth.Authenticated(r.Context(), r)
			if err != nil {
				s.log.ErrorContext(r.Context(), "authenticate request", "path", r.URL.Path, "err", err)
				WriteError(w, http.StatusInternalServerError, "internal", "internal error")
				return
			}
			// r.Pattern is "METHOD /path"; a missing entry is treated as admin-only.
			if policy[strings.TrimSpace(r.Pattern)] == adminOnly && !admin {
				WriteError(w, http.StatusUnauthorized, "auth.required", "admin login required")
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithRequest(r.Context(), auth.NewRequest(r, admin))))
		})
	}
}
