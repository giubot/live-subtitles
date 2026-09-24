// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

func (s *Server) GetSetupStatus(ctx context.Context, _ api.GetSetupStatusRequestObject) (api.GetSetupStatusResponseObject, error) {
	if s.Auth == nil {
		return nil, api.ErrNotImplemented
	}
	pinSet, err := s.Auth.SetupDone(ctx)
	if err != nil {
		return nil, err
	}
	keySet := false
	if s.Secrets != nil {
		_, _, err := s.Secrets.GetSecret(ctx, string(api.GoogleApiKey))
		switch {
		case err == nil:
			keySet = true
		case !errors.Is(err, secrets.ErrNotFound):
			return nil, err
		}
	}
	// The PIN is the only required step until the full wizard (P3-06)
	// adds hardware, models and the first session.
	return api.GetSetupStatus200JSONResponse{
		Completed:       pinSet,
		AdminPinSet:     pinSet,
		GoogleApiKeySet: keySet,
	}, nil
}

func (s *Server) CompleteSetup(ctx context.Context, req api.CompleteSetupRequestObject) (api.CompleteSetupResponseObject, error) {
	if s.Auth == nil {
		return nil, api.ErrNotImplemented
	}
	pin := ""
	if req.Body != nil {
		pin = req.Body.Pin
	}
	token, err := s.Auth.Setup(ctx, pin)
	switch {
	case errors.Is(err, auth.ErrInvalidPIN):
		return api.CompleteSetup400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: "setup.pin_invalid", Message: err.Error(),
			Params: &map[string]any{"min": auth.MinPINLength, "max": auth.MaxPINLength},
			Fields: &map[string]string{"pin": "setup.pin_invalid"},
		}}, nil
	case errors.Is(err, auth.ErrSetupDone):
		return api.CompleteSetup409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse{
			Code: "setup.already_done", Message: "the admin PIN is already set",
		}}, nil
	case err != nil:
		return nil, err
	}
	cookie := s.Auth.Cookie(token, auth.RequestFrom(ctx).Secure).String()
	return api.CompleteSetup204Response{Headers: api.CompleteSetup204ResponseHeaders{SetCookie: &cookie}}, nil
}
