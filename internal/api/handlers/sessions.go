// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

var sessionNotFound = api.NotFoundJSONResponse{Code: "session.not_found", Message: "session not found"}

func (s *Server) RotateIngestToken(ctx context.Context, req api.RotateIngestTokenRequestObject) (api.RotateIngestTokenResponseObject, error) {
	if s.Auth == nil {
		return nil, api.ErrNotImplemented
	}
	token, err := s.Auth.RotateIngestToken(ctx, req.SessionId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.RotateIngestToken404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.RotateIngestToken200JSONResponse{IngestToken: token}, nil
}
