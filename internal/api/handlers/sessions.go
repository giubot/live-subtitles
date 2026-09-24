// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/session"
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

// stateConflict answers an operation the session's state doesn't allow.
func (s *Server) stateConflict(id string) api.ConflictJSONResponse {
	return api.ConflictJSONResponse{Code: "session.state_conflict", Message: "not allowed in the session's current state",
		Params: &map[string]any{"state": s.Manager.State(id)}}
}

func (s *Server) StartSession(ctx context.Context, req api.StartSessionRequestObject) (api.StartSessionResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	if req.Body != nil && req.Body.Source != nil && *req.Body.Source != api.AudioSourceKindBrowser {
		// File audio has its own endpoint (sources/file); SRT and device
		// capture come later (P3-11, P5-08).
		return api.StartSession422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse{
			Code: "source.unsupported", Message: "start this source from its own endpoint, or use browser capture",
			Params: &map[string]any{"source": *req.Body.Source},
		}}, nil
	}
	st, err := s.Manager.Start(ctx, req.SessionId, nil)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.StartSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case errors.Is(err, session.ErrState):
		return api.StartSession409JSONResponse{ConflictJSONResponse: s.stateConflict(req.SessionId)}, nil
	case errors.Is(err, session.ErrUnavailable):
		e := api.Error{Code: session.CodeProviderUnavailable, Message: err.Error()}
		if st.Error != nil {
			e = *st.Error
		}
		return api.StartSession422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse(e)}, nil
	case err != nil:
		return nil, err
	}
	return api.StartSession200JSONResponse(st), nil
}

func (s *Server) PauseSession(ctx context.Context, req api.PauseSessionRequestObject) (api.PauseSessionResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	st, err := s.Manager.Pause(ctx, req.SessionId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.PauseSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case errors.Is(err, session.ErrState):
		return api.PauseSession409JSONResponse{ConflictJSONResponse: s.stateConflict(req.SessionId)}, nil
	case err != nil:
		return nil, err
	}
	return api.PauseSession200JSONResponse(st), nil
}

func (s *Server) StopSession(ctx context.Context, req api.StopSessionRequestObject) (api.StopSessionResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	st, err := s.Manager.Stop(ctx, req.SessionId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.StopSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case err != nil:
		return nil, err
	}
	return api.StopSession200JSONResponse(st), nil
}

func (s *Server) GetSessionStatus(ctx context.Context, req api.GetSessionStatusRequestObject) (api.GetSessionStatusResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	st, err := s.Manager.Status(ctx, req.SessionId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.GetSessionStatus404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case err != nil:
		return nil, err
	}
	return api.GetSessionStatus200JSONResponse(st), nil
}

func (s *Server) StartFileSource(ctx context.Context, req api.StartFileSourceRequestObject) (api.StartFileSourceResponseObject, error) {
	if s.Manager == nil || s.Files == nil {
		return nil, api.ErrNotImplemented
	}
	badRequest := func(code, msg string) api.StartFileSource400JSONResponse {
		return api.StartFileSource400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: code, Message: msg, Fields: &map[string]string{"uri": code},
		}}
	}
	if req.Body == nil {
		return badRequest("source.file_not_allowed", "a uri is required"), nil
	}
	in := ffmpeg.FileInput{URI: req.Body.Uri}
	if req.Body.Loop != nil {
		in.Loop = *req.Body.Loop
	}
	if req.Body.StartAtSec != nil {
		if *req.Body.StartAtSec < 0 {
			return api.StartFileSource400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
				Code: "request.invalid", Message: "startAtSec must not be negative",
				Fields: &map[string]string{"startAtSec": "request.invalid"},
			}}, nil
		}
		in.StartAt = time.Duration(float64(*req.Body.StartAtSec) * float64(time.Second))
	}
	src, err := s.Files.Open(in)
	switch {
	case errors.Is(err, ffmpeg.ErrNotFound):
		return badRequest("source.file_not_found", err.Error()), nil
	case errors.Is(err, ffmpeg.ErrNotAllowed):
		return badRequest("source.file_not_allowed", err.Error()), nil
	case err != nil:
		return nil, err
	}
	st, err := s.Manager.Start(ctx, req.SessionId, src)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.StartFileSource404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case errors.Is(err, session.ErrState):
		return api.StartFileSource409JSONResponse{ConflictJSONResponse: s.stateConflict(req.SessionId)}, nil
	case errors.Is(err, session.ErrUnavailable):
		e := api.Error{Code: session.CodeSourceUnavailable, Message: err.Error()}
		if st.Error != nil {
			e = *st.Error
		}
		return api.StartFileSource422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse(e)}, nil
	case err != nil:
		return nil, err
	}
	return api.StartFileSource202JSONResponse(st), nil
}

// StopFileSource stops the session if a file source feeds it.
func (s *Server) StopFileSource(ctx context.Context, req api.StopFileSourceRequestObject) (api.StopFileSourceResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	if _, err := s.Manager.Status(ctx, req.SessionId); errors.Is(err, domain.ErrNotFound) {
		return api.StopFileSource404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	} else if err != nil {
		return nil, err
	}
	if kind, ok := s.Manager.SourceKind(req.SessionId); !ok || kind != api.AudioSourceKindFile {
		return api.StopFileSource404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse{
			Code: "source.not_running", Message: "no file source is running for this session",
		}}, nil
	}
	if _, err := s.Manager.Stop(ctx, req.SessionId); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	return api.StopFileSource204Response{}, nil
}
