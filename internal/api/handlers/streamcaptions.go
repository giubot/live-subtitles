// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/secrets"
	"github.com/iencodev/live-subtitles/internal/streamcc"
)

// StreamCaptions is the stream closed-captions service (internal/streamcc).
type StreamCaptions interface {
	// SetYouTubeURL stores the session's caption ingestion URL; errors:
	// streamcc.ErrInvalidURL, secrets.ErrReadOnly, secrets.ErrNoBackend.
	SetYouTubeURL(ctx context.Context, sessionID, url string) (api.SecretInfo, error)
	// DeleteYouTubeURL errors: secrets.ErrNotFound, secrets.ErrReadOnly.
	DeleteYouTubeURL(ctx context.Context, sessionID string) error
	YouTubeURLSet(ctx context.Context, sessionID string) bool
	// SendTest errors: streamcc.ErrNoURL or a *domain.CodedError when the
	// target can't be reached; a failed delivery is in the status.
	SendTest(ctx context.Context, sess domain.Session, text string) (api.StreamCaptionStatus, error)
	// Forget drops a deleted session's URL and state.
	Forget(ctx context.Context, sessionID string)
}

// sessionExists answers false for a missing session and an error for a
// store failure.
func (s *Server) sessionExists(ctx context.Context, id string) (domain.Session, bool, error) {
	sess, err := s.Sessions.GetSession(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Session{}, false, nil
	}
	return sess, err == nil, err
}

func (s *Server) PutYoutubeCaptionUrl(ctx context.Context, req api.PutYoutubeCaptionUrlRequestObject) (api.PutYoutubeCaptionUrlResponseObject, error) {
	if s.StreamCaptions == nil || s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	if _, ok, err := s.sessionExists(ctx, req.SessionId); err != nil {
		return nil, err
	} else if !ok {
		return api.PutYoutubeCaptionUrl404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	if req.Body == nil || req.Body.Value == nil || *req.Body.Value == "" {
		return api.PutYoutubeCaptionUrl400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: "secret.value_required", Message: "a value is required",
			Fields: &map[string]string{"value": "secret.value_required"},
		}}, nil
	}
	info, err := s.StreamCaptions.SetYouTubeURL(ctx, req.SessionId, *req.Body.Value)
	switch {
	case errors.Is(err, streamcc.ErrInvalidURL):
		return api.PutYoutubeCaptionUrl400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: streamcc.CodeInvalidURL, Message: "the caption ingestion URL must be an http or https URL",
			Fields: &map[string]string{"value": streamcc.CodeInvalidURL},
		}}, nil
	case errors.Is(err, secrets.ErrReadOnly):
		return api.PutYoutubeCaptionUrl422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse{
			Code: "secret.read_only_env", Message: "this secret is set by an environment variable",
		}}, nil
	case errors.Is(err, secrets.ErrNoBackend):
		return api.PutYoutubeCaptionUrl422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse{
			Code: "secret.no_backend", Message: "no OS keychain available and " + secrets.MasterKeyEnv + " is not set",
		}}, nil
	case err != nil:
		return nil, err
	}
	return api.PutYoutubeCaptionUrl200JSONResponse(info), nil
}

func (s *Server) DeleteYoutubeCaptionUrl(ctx context.Context, req api.DeleteYoutubeCaptionUrlRequestObject) (api.DeleteYoutubeCaptionUrlResponseObject, error) {
	if s.StreamCaptions == nil || s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	if _, ok, err := s.sessionExists(ctx, req.SessionId); err != nil {
		return nil, err
	} else if !ok {
		return api.DeleteYoutubeCaptionUrl404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	err := s.StreamCaptions.DeleteYouTubeURL(ctx, req.SessionId)
	switch {
	case errors.Is(err, secrets.ErrNotFound):
		return api.DeleteYoutubeCaptionUrl404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse{
			Code: "secret.not_found", Message: "secret not found",
		}}, nil
	case errors.Is(err, secrets.ErrReadOnly):
		return api.DeleteYoutubeCaptionUrl409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse{
			Code: "secret.read_only_env", Message: "this secret is set by an environment variable",
		}}, nil
	case err != nil:
		return nil, err
	}
	return api.DeleteYoutubeCaptionUrl204Response{}, nil
}

func (s *Server) SendTestStreamCaption(ctx context.Context, req api.SendTestStreamCaptionRequestObject) (api.SendTestStreamCaptionResponseObject, error) {
	if s.StreamCaptions == nil || s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	sess, ok, err := s.sessionExists(ctx, req.SessionId)
	if err != nil {
		return nil, err
	} else if !ok {
		return api.SendTestStreamCaption404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	text := ""
	if req.Body != nil && req.Body.Text != nil {
		text = *req.Body.Text
	}
	st, err := s.StreamCaptions.SendTest(ctx, sess, text)
	var coded *domain.CodedError
	switch {
	case errors.Is(err, streamcc.ErrNoURL):
		return api.SendTestStreamCaption422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse{
			Code: streamcc.CodeNoURL, Message: "set the caption ingestion URL first",
		}}, nil
	case errors.As(err, &coded):
		e := api.UnprocessableJSONResponse{Code: coded.Code, Message: coded.Message}
		if coded.Params != nil {
			e.Params = &coded.Params
		}
		return api.SendTestStreamCaption422JSONResponse{UnprocessableJSONResponse: e}, nil
	case err != nil:
		return nil, err
	}
	return api.SendTestStreamCaption200JSONResponse(st), nil
}

// withStreamCaptions reports whether the session's ingestion URL is
// stored (StreamCaptionsConfig.youtubeUrlSet, read-only).
func (s *Server) withStreamCaptions(ctx context.Context, sess *domain.Session) {
	if s.StreamCaptions == nil {
		return
	}
	cfg := api.StreamCaptionsConfig{}
	if sess.StreamCaptions != nil {
		cfg = *sess.StreamCaptions
	}
	set := s.StreamCaptions.YouTubeURLSet(ctx, sess.Id)
	cfg.YoutubeUrlSet = &set
	sess.StreamCaptions = &cfg
}
