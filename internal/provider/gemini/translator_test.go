// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fakeTranslatorModels answers with fixed chunks and records the calls.
type fakeTranslatorModels struct {
	chunks []string
	usage  *genai.GenerateContentResponseUsageMetadata
	err    error

	mu     sync.Mutex
	models []string
	cfgs   []*genai.GenerateContentConfig
	users  []string
}

func (f *fakeTranslatorModels) record(model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.models = append(f.models, model)
	f.cfgs = append(f.cfgs, cfg)
	f.users = append(f.users, contents[0].Parts[0].Text)
}

func (f *fakeTranslatorModels) response(text string, last bool) *genai.GenerateContentResponse {
	r := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{
		Role:  genai.RoleModel,
		Parts: []*genai.Part{{Text: "thinking…", Thought: true}, {Text: text}},
	}}}}
	if last {
		r.UsageMetadata = f.usage
	}
	return r
}

func (f *fakeTranslatorModels) GenerateContent(_ context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	f.record(model, contents, cfg)
	if f.err != nil {
		return nil, f.err
	}
	return f.response(strings.Join(f.chunks, ""), true), nil
}

func (f *fakeTranslatorModels) GenerateContentStream(_ context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) iter.Seq2[*genai.GenerateContentResponse, error] {
	f.record(model, contents, cfg)
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		if f.err != nil {
			yield(nil, f.err)
			return
		}
		for i, c := range f.chunks {
			if !yield(f.response(c, i == len(f.chunks)-1), nil) {
				return
			}
		}
	}
}

func settingsWithModel(model string) func(context.Context) (api.Settings, error) {
	return func(context.Context) (api.Settings, error) {
		var s api.Settings
		s.Providers.Gemini.TranslationModel = model
		return s, nil
	}
}

func newFakeTranslator(f *fakeTranslatorModels, settings func(context.Context) (api.Settings, error)) *Translator {
	return &Translator{
		APIKey:    func(context.Context) (string, error) { return "k", nil },
		Settings:  settings,
		newModels: func(context.Context, string) (translatorModels, error) { return f, nil },
	}
}

var enToEs = domain.TranslateRequest{SessionID: "main", SegmentID: "s0", Text: "Hello everyone.", From: "en", To: "es", Final: true,
	Context: []string{"Welcome."}}

func TestTranslatorTranslate(t *testing.T) {
	usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 40, CandidatesTokenCount: 4, ThoughtsTokenCount: 1}
	tests := []struct {
		name      string
		fake      *fakeTranslatorModels
		settings  func(context.Context) (api.Settings, error)
		want      domain.TranslateResult
		wantModel string
		wantErr   string
	}{
		{
			name:      "default model, usage and cleaned text",
			fake:      &fakeTranslatorModels{chunks: []string{`"Hola a todos."`}, usage: usage},
			want:      domain.TranslateResult{Text: "Hola a todos.", Usage: domain.Usage{InputTokens: 40, OutputTokens: 5}},
			wantModel: DefaultTranslationModel,
		},
		{
			name:      "model from settings",
			fake:      &fakeTranslatorModels{chunks: []string{"Hola a todos."}},
			settings:  settingsWithModel("gemini-2.5-flash"),
			want:      domain.TranslateResult{Text: "Hola a todos."},
			wantModel: "gemini-2.5-flash",
		},
		{
			name:      "settings not saved yet",
			fake:      &fakeTranslatorModels{chunks: []string{"Hola."}},
			settings:  func(context.Context) (api.Settings, error) { return api.Settings{}, domain.ErrNotFound },
			want:      domain.TranslateResult{Text: "Hola."},
			wantModel: DefaultTranslationModel,
		},
		{
			name:    "API error",
			fake:    &fakeTranslatorModels{err: errors.New("quota")},
			wantErr: "quota",
		},
		{
			name:    "empty reply",
			fake:    &fakeTranslatorModels{chunks: []string{"  "}},
			wantErr: "empty reply",
		},
	}
	for _, tt := range tests {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", tt.name, stream), func(t *testing.T) {
				tr := newFakeTranslator(tt.fake, tt.settings)
				var got domain.TranslateResult
				var err error
				if stream {
					got, err = tr.TranslateStream(t.Context(), enToEs, nil)
				} else {
					got, err = tr.Translate(t.Context(), enToEs)
				}
				if tt.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("err = %v, want %q", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if got != tt.want {
					t.Errorf("result = %+v, want %+v", got, tt.want)
				}
				if m := tt.fake.models[0]; m != tt.wantModel {
					t.Errorf("model = %q, want %q", m, tt.wantModel)
				}
				cfg := tt.fake.cfgs[0]
				if sys := cfg.SystemInstruction.Parts[0].Text; !strings.Contains(sys, "from English to Spanish") {
					t.Errorf("system instruction = %q", sys)
				}
				if u := tt.fake.users[0]; !strings.Contains(u, "Welcome.") || !strings.HasSuffix(u, "Hello everyone.") {
					t.Errorf("user turn = %q", u)
				}
			})
		}
	}
}

func TestTranslatorStreamPartials(t *testing.T) {
	f := &fakeTranslatorModels{chunks: []string{"Hola", " a", " todos."}}
	tr := newFakeTranslator(f, nil)
	var partials []string
	res, err := tr.TranslateStream(t.Context(), enToEs, func(s string) { partials = append(partials, s) })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Hola", "Hola a", "Hola a todos."}; !slices.Equal(partials, want) {
		t.Errorf("partials = %q, want %q", partials, want)
	}
	if res.Text != "Hola a todos." {
		t.Errorf("text = %q", res.Text)
	}
}

func TestTranslatorResolve(t *testing.T) {
	now := time.Unix(0, 0)
	var keys, opens int
	key := "k1"
	var mu sync.Mutex
	tr := &Translator{
		APIKey: func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			keys++
			return key, nil
		},
		Now: func() time.Time { return now },
		newModels: func(context.Context, string) (translatorModels, error) {
			opens++
			return &fakeTranslatorModels{chunks: []string{"ok"}}, nil
		},
	}
	call := func() {
		t.Helper()
		if _, err := tr.Translate(t.Context(), enToEs); err != nil {
			t.Fatal(err)
		}
	}
	call()
	call()
	if keys != 1 || opens != 1 {
		t.Errorf("within the TTL: key reads = %d, opens = %d; want 1, 1", keys, opens)
	}
	now = now.Add(translatorConfigTTL + time.Second)
	call()
	if keys != 2 || opens != 1 {
		t.Errorf("after the TTL, same key: key reads = %d, opens = %d; want 2, 1", keys, opens)
	}
	now = now.Add(translatorConfigTTL + time.Second)
	key = "k2"
	call()
	if keys != 3 || opens != 2 {
		t.Errorf("after the TTL, new key: key reads = %d, opens = %d; want 3, 2", keys, opens)
	}
}

func TestTranslatorKeyErrors(t *testing.T) {
	tests := []struct {
		name   string
		apiKey func(context.Context) (string, error)
		is     error
	}{
		{name: "no source"},
		{name: "not set", apiKey: func(context.Context) (string, error) { return "", domain.ErrNotFound }, is: domain.ErrNotFound},
		{name: "empty", apiKey: func(context.Context) (string, error) { return "", nil }, is: domain.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Translator{APIKey: tt.apiKey}
			_, err := tr.Translate(t.Context(), enToEs)
			if err == nil || (tt.is != nil && !errors.Is(err, tt.is)) {
				t.Errorf("err = %v, want %v", err, tt.is)
			}
		})
	}
}

func TestTranslatorThinking(t *testing.T) {
	zero := int32(0)
	tests := []struct {
		model string
		want  *genai.ThinkingConfig
	}{
		{"gemini-2.5-flash-lite", nil},
		{"gemini-2.5-flash", &genai.ThinkingConfig{ThinkingBudget: &zero}},
		{"gemini-3-flash-preview", &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal}},
		{"gemini-3-pro-preview", &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}},
		{"gemini-2.5-pro", nil},
	}
	for _, tt := range tests {
		got := translatorThinking(tt.model)
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(tt.want)
		if string(gj) != string(wj) {
			t.Errorf("translatorThinking(%q) = %s, want %s", tt.model, gj, wj)
		}
	}
}

// The real genai client against a fake Gemini API: checks the wire
// format (endpoint, key header, system instruction, SSE streaming).
func TestTranslatorHTTP(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.Header.Get("X-Goog-Api-Key") != "secret-key" {
			http.Error(w, `{"error":{"code":401,"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		var req struct {
			SystemInstruction struct {
				Parts []struct{ Text string } `json:"parts"`
			} `json:"systemInstruction"`
		}
		if err := json.Unmarshal(body, &req); err != nil || len(req.SystemInstruction.Parts) == 0 ||
			!strings.Contains(req.SystemInstruction.Parts[0].Text, "from English to Spanish") {
			http.Error(w, `{"error":{"code":400,"message":"no system instruction"}}`, http.StatusBadRequest)
			return
		}
		chunk := func(text string, usage bool) string {
			s := `{"candidates":[{"content":{"role":"model","parts":[{"text":` + fmt.Sprintf("%q", text) + `}]}}]`
			if usage {
				s += `,"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3}`
			}
			return s + "}"
		}
		if strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", chunk("Hola", false))
			fmt.Fprintf(w, "data: %s\n\n", chunk(" a todos.", true))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chunk("Hola a todos.", true))
	}))
	defer srv.Close()

	tr := &Translator{
		APIKey:   func(context.Context) (string, error) { return "secret-key", nil },
		Settings: settingsWithModel("gemini-test"),
		BaseURL:  srv.URL,
	}
	want := domain.TranslateResult{Text: "Hola a todos.", Usage: domain.Usage{InputTokens: 12, OutputTokens: 3}}
	res, err := tr.Translate(t.Context(), enToEs)
	if err != nil || res != want {
		t.Fatalf("Translate = %+v, %v; want %+v", res, err, want)
	}
	var partials []string
	res, err = tr.TranslateStream(t.Context(), enToEs, func(s string) { partials = append(partials, s) })
	if err != nil || res != want {
		t.Fatalf("TranslateStream = %+v, %v; want %+v", res, err, want)
	}
	if !slices.Equal(partials, []string{"Hola", "Hola a todos."}) {
		t.Errorf("partials = %q", partials)
	}
	for _, p := range paths {
		if !strings.Contains(p, "/models/gemini-test:") {
			t.Errorf("path = %q, want the settings model", p)
		}
	}
}
