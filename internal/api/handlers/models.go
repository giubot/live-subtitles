// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// ModelService lists and downloads local models (models.Manager).
type ModelService interface {
	List(ctx context.Context) ([]api.LocalModel, error)
	// Download returns domain.ErrNotFound for an unknown model and a
	// *domain.CodedError when the download can't start.
	Download(ctx context.Context, id string) (api.LocalModel, error)
}

// ListModels lists the whisper and Gemma models and their state (AI-13).
func (s *Server) ListModels(ctx context.Context, _ api.ListModelsRequestObject) (api.ListModelsResponseObject, error) {
	if s.Models == nil {
		return nil, api.ErrNotImplemented
	}
	list, err := s.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	return api.ListModels200JSONResponse(list), nil
}

// DownloadModel starts or resumes a download; progress goes out as
// modelProgress events on /ws/admin.
func (s *Server) DownloadModel(ctx context.Context, req api.DownloadModelRequestObject) (api.DownloadModelResponseObject, error) {
	if s.Models == nil {
		return nil, api.ErrNotImplemented
	}
	m, err := s.Models.Download(ctx, req.ModelId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.DownloadModel404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse{
			Code: "model.not_found", Message: "no such model", Params: &map[string]any{"model": req.ModelId}}}, nil
	}
	var ce *domain.CodedError
	if errors.As(err, &ce) {
		return api.DownloadModel409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(codedError(ce))}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.DownloadModel202JSONResponse(m), nil
}
