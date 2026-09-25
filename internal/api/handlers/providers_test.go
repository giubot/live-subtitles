// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/provider/selector"
)

// fakeRule is a ProviderRule whose Validate answers from a map of secret
// name → result (or error).
type fakeRule struct {
	providers api.ProvidersResponse
	results   map[string]api.SecretValidation
	errs      map[string]error
	last      map[string]bool
	validated []string
	forgets   int
}

func (f *fakeRule) Providers(context.Context) api.ProvidersResponse { return f.providers }

func (f *fakeRule) Validate(_ context.Context, name string) (api.SecretValidation, error) {
	f.validated = append(f.validated, name)
	if err := f.errs[name]; err != nil {
		return api.SecretValidation{}, err
	}
	r, ok := f.results[name]
	if !ok {
		return api.SecretValidation{}, selector.ErrUnsupported
	}
	if f.last == nil {
		f.last = map[string]bool{}
	}
	f.last[name] = r.Valid
	return r, nil
}

func (f *fakeRule) LastValid(_ context.Context, name string) *bool {
	v, ok := f.last[name]
	if !ok {
		return nil
	}
	return &v
}

func (f *fakeRule) Forget() { f.forgets++ }

func serve(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListProviders(t *testing.T) {
	reason := api.NoGoogleApiKey
	code := selector.CodeNoAPIKey
	tests := []struct {
		name       string
		rule       ProviderRule
		wantStatus int
		wantBody   []string
	}{
		{"not wired", nil, 501, []string{`"code":"not_implemented"`}},
		{"local by default", &fakeRule{providers: api.ProvidersResponse{
			DefaultProvider: api.ProviderKindLocal, DefaultReason: &reason,
			Providers: []api.ProviderInfo{{Kind: api.ProviderKindGemini, ReasonCode: &code}, {Kind: api.ProviderKindLocal, Available: true}},
		}}, 200, []string{`"defaultProvider":"local"`, `"defaultReason":"no_google_api_key"`, `"reasonCode":"provider.no_api_key"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if tt.rule != nil {
				s.Providers = tt.rule
			}
			rec := serve(t, s, "GET", "/api/providers", "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			for _, want := range tt.wantBody {
				if !strings.Contains(rec.Body.String(), want) {
					t.Errorf("body %s lacks %s", rec.Body, want)
				}
			}
		})
	}
}

func TestValidateSecret(t *testing.T) {
	invalid := selector.CodeKeyInvalid
	tests := []struct {
		name       string
		path       string
		rule       *fakeRule
		wantStatus int
		wantBody   string
	}{
		{"valid", "/api/secrets/google_api_key/validate",
			&fakeRule{results: map[string]api.SecretValidation{"google_api_key": {Valid: true}}}, 200, `{"valid":true}`},
		{"invalid", "/api/secrets/google_api_key/validate",
			&fakeRule{results: map[string]api.SecretValidation{"google_api_key": {Code: &invalid}}}, 200, `"code":"provider.key_invalid"`},
		{"not set", "/api/secrets/google_api_key/validate",
			&fakeRule{errs: map[string]error{"google_api_key": selector.ErrNotSet}}, 404, `"code":"secret.not_found"`},
		{"no check", "/api/secrets/obs_websocket_password/validate", &fakeRule{}, 422, `"code":"secret.validation_unsupported"`},
		{"unknown name", "/api/secrets/nope/validate", &fakeRule{}, 404, `"code":"secret.not_found"`},
		{"store failure", "/api/secrets/google_api_key/validate",
			&fakeRule{errs: map[string]error{"google_api_key": errors.New("keychain locked")}}, 500, `"code":"internal"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.Providers = tt.rule
			rec := serve(t, s, "POST", tt.path, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body %s lacks %s", rec.Body, tt.wantBody)
			}
		})
	}
}

// Saving a key validates it unless the client opts out; listing and
// saving report the last result; deleting forgets the cached key.
func TestSecretsWithRule(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		wantStatus               int
		wantValidated            int
		wantForgets              int
		wantBody                 string
	}{
		{"put validates", "PUT", "/api/secrets/google_api_key", `{"value":"AIzaNewKey1234"}`, 200, 1, 1, `"valid":true`},
		{"put without validation", "PUT", "/api/secrets/google_api_key", `{"value":"AIzaNewKey1234","validate":false}`, 200, 0, 1, `"set":true`},
		{"put a secret without a check", "PUT", "/api/secrets/obs_websocket_password", `{"value":"hunter2hunter2"}`, 200, 1, 1, `"set":true`},
		{"delete forgets", "DELETE", "/api/secrets/google_api_key", "", 204, 0, 1, ""},
		{"list reports validity", "GET", "/api/secrets", "", 200, 0, 0, `"valid":false`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &fakeRule{
				results: map[string]api.SecretValidation{"google_api_key": {Valid: true}},
				last:    map[string]bool{"google_api_key": false},
			}
			s := New()
			s.Secrets = &fakeSecrets{values: map[string]string{"google_api_key": "AIzaOldKey9876"}}
			s.Providers = rule
			rec := serve(t, s, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if len(rule.validated) != tt.wantValidated || rule.forgets != tt.wantForgets {
				t.Errorf("validated %v, forgets %d; want %d, %d", rule.validated, rule.forgets, tt.wantValidated, tt.wantForgets)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body %s lacks %s", rec.Body, tt.wantBody)
			}
			if strings.Contains(rec.Body.String(), "AIza") {
				t.Errorf("value leaked: %s", rec.Body)
			}
		})
	}
}
