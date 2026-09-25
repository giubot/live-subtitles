// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

type fakeModels struct {
	list []api.LocalModel
	err  error
}

func (f fakeModels) List(context.Context) ([]api.LocalModel, error) { return f.list, f.err }

func (f fakeModels) Download(_ context.Context, id string) (api.LocalModel, error) {
	if f.err != nil {
		return api.LocalModel{}, f.err
	}
	for _, m := range f.list {
		if m.Id == id {
			m.Status = api.LocalModelStatusDownloading
			return m, nil
		}
	}
	return api.LocalModel{}, domain.ErrNotFound
}

func TestModelHandlers(t *testing.T) {
	turbo := api.LocalModel{Id: "whisper-large-v3-turbo", Kind: api.Whisper, Name: "large-v3-turbo", Status: api.LocalModelStatusMissing}
	ollamaDown := &domain.CodedError{Code: "model.ollama_unreachable", Message: "Ollama isn't reachable"}
	for _, c := range []struct {
		name     string
		models   ModelService
		method   string
		path     string
		status   int
		wantBody []string
	}{
		{"list 501", nil, "GET", "/api/models", 501, []string{"not_implemented"}},
		{"list", fakeModels{list: []api.LocalModel{turbo}}, "GET", "/api/models", 200,
			[]string{`"id":"whisper-large-v3-turbo"`, `"status":"missing"`}},
		{"list error", fakeModels{err: errors.New("boom")}, "GET", "/api/models", 500, []string{`"code":"internal"`}},
		{"download", fakeModels{list: []api.LocalModel{turbo}}, "POST", "/api/models/whisper-large-v3-turbo/download", 202,
			[]string{`"status":"downloading"`}},
		{"download unknown", fakeModels{}, "POST", "/api/models/nope/download", 404,
			[]string{`"code":"model.not_found"`, `"model":"nope"`}},
		{"download without ollama", fakeModels{err: ollamaDown}, "POST", "/api/models/gemma3-4b/download", 409,
			[]string{`"code":"model.ollama_unreachable"`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := New()
			s.Models = c.models
			h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
			res := call{c.method, c.path, "", "", "", c.status, ""}.do(t, h)
			body, _ := io.ReadAll(res.Body)
			for _, w := range c.wantBody {
				if !strings.Contains(string(body), w) {
					t.Errorf("body %s lacks %s", body, w)
				}
			}
		})
	}
}
