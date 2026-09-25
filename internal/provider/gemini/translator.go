// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/translate"
)

// DefaultTranslationModel is used when Settings.providers.gemini.translationModel
// is empty: a fast, non-thinking text model suits one-line captions.
const DefaultTranslationModel = "gemini-2.5-flash-lite"

// translatorConfigTTL is how long a resolved API key and model are reused,
// so a live session doesn't hit the keychain on every caption.
const translatorConfigTTL = 30 * time.Second

// Translator is a domain.StreamingTranslator on a Gemini text model (AI-2,
// AI-5). The API key and the model are resolved lazily, so a key saved in
// Settings works without a restart.
type Translator struct {
	// APIKey returns the Google API key (the google_api_key secret). Required.
	APIKey func(ctx context.Context) (string, error)
	// Settings, if set, supplies providers.gemini.translationModel.
	Settings func(ctx context.Context) (api.Settings, error)
	// BaseURL overrides the Gemini API endpoint (tests).
	BaseURL string
	// HTTPClient is used for API calls; nil uses the genai default.
	HTTPClient *http.Client
	// Now defaults to time.Now.
	Now func() time.Time

	// newModels opens the API for a key; tests replace it.
	newModels func(ctx context.Context, apiKey string) (translatorModels, error)

	mu       sync.Mutex
	models   translatorModels
	modelsOf string // the API key models was opened with
	model    string
	expires  time.Time
}

// translatorModels is the part of genai.Models the translator uses.
type translatorModels interface {
	GenerateContent(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
	GenerateContentStream(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) iter.Seq2[*genai.GenerateContentResponse, error]
}

var _ domain.StreamingTranslator = (*Translator)(nil)

func (t *Translator) Kind() domain.ProviderKind { return api.ProviderKindGemini }

// Translate translates req in one call.
func (t *Translator) Translate(ctx context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	models, model, err := t.resolve(ctx)
	if err != nil {
		return domain.TranslateResult{}, err
	}
	contents, cfg := translatorRequest(req, model)
	resp, err := models.GenerateContent(ctx, model, contents, cfg)
	if err != nil {
		return domain.TranslateResult{}, fmt.Errorf("gemini translate: %w", err)
	}
	return translatorResult(req, translatorText(resp), resp.UsageMetadata)
}

// TranslateStream translates req, passing the text so far to partial as
// it streams.
func (t *Translator) TranslateStream(ctx context.Context, req domain.TranslateRequest, partial func(text string)) (domain.TranslateResult, error) {
	models, model, err := t.resolve(ctx)
	if err != nil {
		return domain.TranslateResult{}, err
	}
	contents, cfg := translatorRequest(req, model)
	var text strings.Builder
	var usage *genai.GenerateContentResponseUsageMetadata
	for resp, err := range models.GenerateContentStream(ctx, model, contents, cfg) {
		if err != nil {
			return domain.TranslateResult{}, fmt.Errorf("gemini translate: %w", err)
		}
		if resp == nil {
			continue
		}
		if resp.UsageMetadata != nil {
			usage = resp.UsageMetadata
		}
		if chunk := translatorText(resp); chunk != "" {
			text.WriteString(chunk)
			if partial != nil {
				partial(text.String())
			}
		}
	}
	return translatorResult(req, text.String(), usage)
}

// resolve returns the API client and model, reusing them for
// translatorConfigTTL.
func (t *Translator) resolve(ctx context.Context) (translatorModels, string, error) {
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.models != nil && now().Before(t.expires) {
		return t.models, t.model, nil
	}
	if t.APIKey == nil {
		return nil, "", errors.New("gemini translate: no API key source")
	}
	key, err := t.APIKey(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("gemini translate: read the Google API key: %w", err)
	}
	if key == "" {
		return nil, "", fmt.Errorf("gemini translate: the Google API key is not set: %w", domain.ErrNotFound)
	}
	model := DefaultTranslationModel
	if t.Settings != nil {
		st, err := t.Settings(ctx)
		switch {
		case err == nil && st.Providers.Gemini.TranslationModel != "":
			model = st.Providers.Gemini.TranslationModel
		case err != nil && !errors.Is(err, domain.ErrNotFound):
			return nil, "", fmt.Errorf("gemini translate: read settings: %w", err)
		}
	}
	if t.models == nil || t.modelsOf != key {
		open := t.newModels
		if open == nil {
			open = t.openModels
		}
		m, err := open(ctx, key)
		if err != nil {
			return nil, "", fmt.Errorf("gemini translate: %w", err)
		}
		t.models, t.modelsOf = m, key
	}
	t.model, t.expires = model, now().Add(translatorConfigTTL)
	return t.models, t.model, nil
}

func (t *Translator) openModels(ctx context.Context, apiKey string) (translatorModels, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      apiKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPClient:  t.HTTPClient,
		HTTPOptions: genai.HTTPOptions{BaseURL: t.BaseURL},
	})
	if err != nil {
		return nil, err
	}
	return c.Models, nil
}

// translatorRequest renders req with the shared prompt template.
func translatorRequest(req domain.TranslateRequest, model string) ([]*genai.Content, *genai.GenerateContentConfig) {
	p := translate.BuildPrompt(req)
	temp := float32(0.2)
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: p.System}}},
		Temperature:       &temp,
		// A caption line is short; this only stops a runaway reply.
		MaxOutputTokens: 512,
		ThinkingConfig:  translatorThinking(model),
	}
	return []*genai.Content{genai.NewContentFromText(p.User, genai.RoleUser)}, cfg
}

// translatorThinking turns thinking down where the model allows it:
// translation of a caption gains little from it and latency matters more.
func translatorThinking(model string) *genai.ThinkingConfig {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "flash-lite"):
		return nil // doesn't think by default
	case strings.Contains(m, "gemini-2.5-flash"):
		zero := int32(0)
		return &genai.ThinkingConfig{ThinkingBudget: &zero}
	case strings.HasPrefix(m, "gemini-3") && strings.Contains(m, "flash"):
		return &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal}
	case strings.HasPrefix(m, "gemini-3"):
		return &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}
	}
	return nil
}

// translatorText is the non-thought text of the first candidate.
func translatorText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range resp.Candidates[0].Content.Parts {
		if p != nil && !p.Thought {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func translatorResult(req domain.TranslateRequest, text string, u *genai.GenerateContentResponseUsageMetadata) (domain.TranslateResult, error) {
	res := domain.TranslateResult{Text: translate.CleanOutput(text)}
	if u != nil {
		res.Usage = domain.Usage{
			InputTokens:  int64(u.PromptTokenCount),
			OutputTokens: int64(u.CandidatesTokenCount) + int64(u.ThoughtsTokenCount),
		}
	}
	if res.Text == "" && strings.TrimSpace(req.Text) != "" {
		return res, errors.New("gemini translate: empty reply")
	}
	return res, nil
}
