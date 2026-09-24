// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"math"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/auth"
)

func (s *Server) Login(ctx context.Context, req api.LoginRequestObject) (api.LoginResponseObject, error) {
	if s.Auth == nil {
		return nil, api.ErrNotImplemented
	}
	pin := ""
	if req.Body != nil {
		pin = req.Body.Pin
	}
	r := auth.RequestFrom(ctx)
	token, err := s.Auth.Login(ctx, r.Client, pin)
	var limited *auth.RateLimitedError
	switch {
	case errors.As(err, &limited):
		return api.Login429JSONResponse{TooManyRequestsJSONResponse: api.TooManyRequestsJSONResponse{
			Code: "auth.rate_limited", Message: "too many failed logins",
			Params: &map[string]any{"retryAfterSec": int(math.Ceil(limited.RetryAfter.Seconds()))},
		}}, nil
	case errors.Is(err, auth.ErrSetupRequired):
		return api.Login401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{
			Code: "auth.setup_required", Message: "set the admin PIN first",
		}}, nil
	case errors.Is(err, auth.ErrWrongPIN):
		s.log.WarnContext(ctx, "admin login failed", "client", r.Client)
		return api.Login401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{
			Code: "auth.invalid_pin", Message: "wrong PIN",
			Fields: &map[string]string{"pin": "auth.invalid_pin"},
		}}, nil
	case err != nil:
		return nil, err
	}
	s.log.InfoContext(ctx, "admin logged in", "client", r.Client)
	cookie := s.Auth.Cookie(token, r.Secure).String()
	return api.Login204Response{Headers: api.Login204ResponseHeaders{SetCookie: &cookie}}, nil
}

// Logout ends the cookie's login on the server. The spec's 204 carries no
// Set-Cookie, so the browser keeps a cookie that no longer authenticates.
func (s *Server) Logout(ctx context.Context, _ api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	if s.Auth == nil {
		return nil, api.ErrNotImplemented
	}
	if err := s.Auth.Logout(ctx, auth.RequestFrom(ctx).Cookie); err != nil {
		return nil, err
	}
	return api.Logout204Response{}, nil
}

func (s *Server) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	if s.Auth == nil {
		return nil, api.ErrNotImplemented
	}
	return api.GetMe200JSONResponse{Authenticated: auth.RequestFrom(ctx).Admin}, nil
}
