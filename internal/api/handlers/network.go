// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"github.com/iencodev/live-subtitles/internal/api"
)

func (s *Server) GetNetworkInfo(context.Context, api.GetNetworkInfoRequestObject) (api.GetNetworkInfoResponseObject, error) {
	if s.Network == nil {
		return nil, api.ErrNotImplemented
	}
	return api.GetNetworkInfo200JSONResponse(s.Network()), nil
}
