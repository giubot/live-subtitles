// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"github.com/iencodev/live-subtitles/internal/api"
)

// ProviderRule is the default-provider rule and the key checks behind it
// (AI-11, SEC-5); *selector.Selector in production.
type ProviderRule interface {
	// Providers reports provider availability and the resolved default.
	Providers(ctx context.Context) api.ProvidersResponse
	// Validate checks the named secret now; selector.ErrNotSet when it
	// isn't stored, selector.ErrUnsupported when it has no check.
	Validate(ctx context.Context, name string) (api.SecretValidation, error)
	// LastValid is the cached result for the secret's current value, or nil.
	LastValid(ctx context.Context, name string) *bool
	// Forget drops cached secret values after a secret changed.
	Forget()
}

// ListProviders reports provider availability and the default (AI-4, AI-11).
func (s *Server) ListProviders(ctx context.Context, _ api.ListProvidersRequestObject) (api.ListProvidersResponseObject, error) {
	if s.Providers == nil {
		return nil, api.ErrNotImplemented
	}
	return api.ListProviders200JSONResponse(s.Providers.Providers(ctx)), nil
}
