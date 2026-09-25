// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func (s *Server) GetHealth(context.Context, api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	return api.GetHealth200JSONResponse{Status: api.HealthStatusOk}, nil
}

// ListLanguages returns the supported language catalog (AI-5).
func (s *Server) ListLanguages(context.Context, api.ListLanguagesRequestObject) (api.ListLanguagesResponseObject, error) {
	return api.ListLanguages200JSONResponse(domain.Languages()), nil
}
