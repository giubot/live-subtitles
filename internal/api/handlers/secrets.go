// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

// knownSecrets are the names the admin UI manages (the SecretName enum).
var knownSecrets = []api.SecretName{api.GoogleApiKey, api.ObsWebsocketPassword}

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
		out = append(out, info)
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
	// req.Body.Validate is handled by P2-07 (key validation).
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
	info, err := s.Secrets.SecretInfo(ctx, string(req.Name))
	if err != nil {
		return nil, err
	}
	return api.PutSecret200JSONResponse(info), nil
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
	return api.DeleteSecret204Response{}, nil
}
