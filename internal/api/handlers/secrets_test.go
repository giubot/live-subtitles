// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

// fakeSecrets is an in-memory domain.SecretStore; names in env are read-only.
type fakeSecrets struct {
	values    map[string]string
	env       map[string]string
	noBackend bool
}

func (f *fakeSecrets) GetSecret(_ context.Context, name string) (string, domain.SecretSource, error) {
	if v, ok := f.env[name]; ok {
		return v, domain.SecretFromEnv, nil
	}
	if v, ok := f.values[name]; ok {
		return v, domain.SecretFromKeychain, nil
	}
	return "", "", secrets.ErrNotFound
}

func (f *fakeSecrets) SetSecret(_ context.Context, name, value string) error {
	if _, ok := f.env[name]; ok {
		return fmt.Errorf("%w: %s", secrets.ErrReadOnly, name)
	}
	if f.noBackend {
		return secrets.ErrNoBackend
	}
	f.values[name] = value
	return nil
}

func (f *fakeSecrets) DeleteSecret(_ context.Context, name string) error {
	if _, ok := f.env[name]; ok {
		return fmt.Errorf("%w: %s", secrets.ErrReadOnly, name)
	}
	if _, ok := f.values[name]; !ok {
		return fmt.Errorf("%w: %s", secrets.ErrNotFound, name)
	}
	delete(f.values, name)
	return nil
}

func (f *fakeSecrets) SecretInfo(ctx context.Context, name string) (domain.SecretInfo, error) {
	info := domain.SecretInfo{Name: api.SecretName(name)}
	v, src, err := f.GetSecret(ctx, name)
	if err != nil {
		return info, nil
	}
	hint, s := secrets.Hint(v), secrets.APISource(src)
	info.Set, info.Hint, info.Source = true, &hint, &s
	return info, nil
}

func TestSecretsHandlers(t *testing.T) {
	const stored = "AIzaSyStoredValue3f9a"
	tests := []struct {
		name, method, path, body string
		store                    *fakeSecrets
		wantStatus               int
		wantBody                 []string
	}{
		{"list", "GET", "/api/secrets", "", &fakeSecrets{values: map[string]string{"google_api_key": stored}}, 200,
			[]string{`{"hint":"••••3f9a","name":"google_api_key","set":true,"source":"keychain"}`, `{"name":"obs_websocket_password","set":false}`}},
		{"put", "PUT", "/api/secrets/google_api_key", `{"value":"new-value-abcd"}`, &fakeSecrets{values: map[string]string{}}, 200,
			[]string{`"set":true`, `"hint":"••••abcd"`}},
		{"put empty", "PUT", "/api/secrets/google_api_key", `{"value":""}`, &fakeSecrets{values: map[string]string{}}, 400,
			[]string{`"code":"secret.value_required"`}},
		{"put missing value", "PUT", "/api/secrets/google_api_key", `{}`, &fakeSecrets{values: map[string]string{}}, 400,
			[]string{`"code":"secret.value_required"`}},
		{"put unknown name", "PUT", "/api/secrets/nope", `{"value":"x"}`, &fakeSecrets{values: map[string]string{}}, 400,
			[]string{`"code":"secret.unknown_name"`}},
		{"put env", "PUT", "/api/secrets/google_api_key", `{"value":"x-value"}`, &fakeSecrets{values: map[string]string{}, env: map[string]string{"google_api_key": "e"}}, 422,
			[]string{`"code":"secret.read_only_env"`}},
		{"put no backend", "PUT", "/api/secrets/google_api_key", `{"value":"x-value"}`, &fakeSecrets{values: map[string]string{}, noBackend: true}, 422,
			[]string{`"code":"secret.no_backend"`}},
		{"delete", "DELETE", "/api/secrets/google_api_key", "", &fakeSecrets{values: map[string]string{"google_api_key": stored}}, 204, nil},
		{"delete missing", "DELETE", "/api/secrets/google_api_key", "", &fakeSecrets{values: map[string]string{}}, 404,
			[]string{`"code":"secret.not_found"`}},
		{"delete unknown name", "DELETE", "/api/secrets/nope", "", &fakeSecrets{values: map[string]string{}}, 404,
			[]string{`"code":"secret.not_found"`}},
		{"delete env", "DELETE", "/api/secrets/google_api_key", "", &fakeSecrets{values: map[string]string{}, env: map[string]string{"google_api_key": "e"}}, 409,
			[]string{`"code":"secret.read_only_env"`}},
		{"validate is P2-07", "POST", "/api/secrets/google_api_key/validate", "", &fakeSecrets{values: map[string]string{}}, 501,
			[]string{`"code":"not_implemented"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.Secrets = tt.store
			h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			body := rec.Body.String()
			for _, want := range tt.wantBody {
				if !strings.Contains(body, want) {
					t.Errorf("body %s lacks %s", body, want)
				}
			}
			if strings.Contains(body, stored) || strings.Contains(body, "new-value") {
				t.Errorf("value leaked: %s", body)
			}
		})
	}
}

func TestSecretsHandlersWithoutStore(t *testing.T) {
	h := New().Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/secrets", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status %d, want 501", rec.Code)
	}
}
