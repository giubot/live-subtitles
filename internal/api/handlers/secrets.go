// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/provider/selector"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

// knownSecrets are the names the admin UI manages (the SecretName enum).
var knownSecrets = []api.SecretName{api.GoogleApiKey, api.ObsWebsocketPassword, api.SrtPassphrase}

func (s *Server) ListSecrets(ctx context.Context, _ api.ListSecretsRequestObject) (api.ListSecretsResponseObject, error) {
	if s.Secrets == nil {
		return nil, api.ErrNotImplemented
	}
	out := make(api.ListSecrets200JSONResponse, 0, len(knownSecrets))
	for _, name := range knownSecrets {
		info, err := s.Secrets.SecretInfo(ctx, string(name))
		if err != nil {
			return nil, err
		}
		out = append(out, s.withValidity(ctx, info))
	}
	return out, nil
}

func (s *Server) PutSecret(ctx context.Context, req api.PutSecretRequestObject) (api.PutSecretResponseObject, error) {
	if s.Secrets == nil {
		return nil, api.ErrNotImplemented
	}
	if !req.Name.Valid() {
		return api.PutSecret400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: "secret.unknown_name", Message: "unknown secret name",
		}}, nil
	}
	if req.Body == nil || req.Body.Value == nil || *req.Body.Value == "" {
		return api.PutSecret400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: "secret.value_required", Message: "a value is required",
			Fields: &map[string]string{"value": "secret.value_required"},
		}}, nil
	}
	err := s.Secrets.SetSecret(ctx, string(req.Name), *req.Body.Value)
	switch {
	case errors.Is(err, secrets.ErrReadOnly):
		return api.PutSecret422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse{
			Code: "secret.read_only_env", Message: "this secret is set by an environment variable",
		}}, nil
	case errors.Is(err, secrets.ErrNoBackend):
		return api.PutSecret422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse{
			Code: "secret.no_backend", Message: "no OS keychain available and " + secrets.MasterKeyEnv + " is not set",
		}}, nil
	case err != nil:
		return nil, err
	}
	// A key is validated on save (AI-11) unless the client opts out. An
	// invalid key is still stored: the default stays local and the
	// response says it's invalid.
	if s.Providers != nil {
		s.Providers.Forget()
		if req.Body.Validate == nil || *req.Body.Validate {
			if _, err := s.Providers.Validate(ctx, string(req.Name)); err != nil && !errors.Is(err, selector.ErrUnsupported) {
				return nil, err
			}
		}
	}
	info, err := s.Secrets.SecretInfo(ctx, string(req.Name))
	if err != nil {
		return nil, err
	}
	return api.PutSecret200JSONResponse(s.withValidity(ctx, info)), nil
}

func (s *Server) DeleteSecret(ctx context.Context, req api.DeleteSecretRequestObject) (api.DeleteSecretResponseObject, error) {
	if s.Secrets == nil {
		return nil, api.ErrNotImplemented
	}
	notFound := api.DeleteSecret404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse{
		Code: "secret.not_found", Message: "secret not found",
	}}
	if !req.Name.Valid() {
		return notFound, nil
	}
	err := s.Secrets.DeleteSecret(ctx, string(req.Name))
	switch {
	case errors.Is(err, secrets.ErrNotFound):
		return notFound, nil
	case errors.Is(err, secrets.ErrReadOnly):
		return api.DeleteSecret409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse{
			Code: "secret.read_only_env", Message: "this secret is set by an environment variable",
		}}, nil
	case err != nil:
		return nil, err
	}
	if s.Providers != nil {
		s.Providers.Forget() // the default falls back to local now (AI-11)
	}
	return api.DeleteSecret204Response{}, nil
}

// ValidateSecret checks the stored secret against its service (SEC-5).
func (s *Server) ValidateSecret(ctx context.Context, req api.ValidateSecretRequestObject) (api.ValidateSecretResponseObject, error) {
	if s.Providers == nil {
		return nil, api.ErrNotImplemented
	}
	notFound := api.ValidateSecret404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse{
		Code: "secret.not_found", Message: "secret not found",
	}}
	if !req.Name.Valid() {
		return notFound, nil
	}
	v, err := s.Providers.Validate(ctx, string(req.Name))
	switch {
	case errors.Is(err, selector.ErrNotSet):
		return notFound, nil
	case errors.Is(err, selector.ErrUnsupported):
		return api.ValidateSecret422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse{
			Code: selector.CodeValidationUnsupported, Message: "this secret can't be checked",
		}}, nil
	case err != nil:
		return nil, err
	}
	return api.ValidateSecret200JSONResponse(v), nil
}

// withValidity adds the last validation result to info (SecretInfo.valid).
func (s *Server) withValidity(ctx context.Context, info api.SecretInfo) api.SecretInfo {
	if s.Providers != nil && info.Set {
		info.Valid = s.Providers.LastValid(ctx, string(info.Name))
	}
	return info
}
