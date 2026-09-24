// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"github.com/iencodev/live-subtitles/internal/api"
)

func (s *Server) GetHealth(context.Context, api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	return api.GetHealth200JSONResponse{Status: api.HealthStatusOk}, nil
}
