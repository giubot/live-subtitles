// SPDX-License-Identifier: Apache-2.0

// Package gemma translates captions with Gemma on a local Ollama server
// (AI-3, AI-5), through Ollama's /api/chat streaming API.
package gemma

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/translate"
)

// Defaults, matching Settings.providers.local in the API spec.
const (
	DefaultURL   = "http://127.0.0.1:11434"
	DefaultModel = "gemma3:4b"
	// DefaultKeepAlive keeps the model loaded between captions and through
	// pauses, so a talk never waits for a reload.
	DefaultKeepAlive = 30 * time.Minute
	// DefaultConcurrency is how many requests run at once per model. Ollama
	// queues the rest anyway; limiting here keeps finals of one track from
	// waiting behind a pile of interims of another.
	DefaultConcurrency = 2
	// WarmTimeout bounds the model load at session start.
	WarmTimeout = 3 * time.Minute
)

// configTTL is how long resolved settings are reused.
const configTTL = 30 * time.Second

// Translator is a domain.StreamingTranslator on Gemma via Ollama. The
// server URL and model come from Settings.providers.local.{ollamaUrl,
// gemmaModel}, resolved lazily so edits apply to the next session.
type Translator struct {
	// Settings, if set, supplies the Ollama URL and the Gemma model.
	Settings func(ctx context.Context) (api.Settings, error)
	// URL and Model are used when the settings leave them empty (default
	// DefaultURL, DefaultModel).
	URL, Model string
	// KeepAlive is sent with every request (default DefaultKeepAlive).
	KeepAlive time.Duration
	// Concurrency limits requests per server and model (default
	// DefaultConcurrency).
	Concurrency int
	// HTTPClient defaults to a client without a timeout; request contexts
	// bound each call.
	HTTPClient *http.Client
	// Now defaults to time.Now.
	Now func() time.Time

	mu      sync.Mutex
	sems    map[string]chan struct{} // per URL + model
	url     string
	model   string
	expires time.Time
}

var (
	_ domain.StreamingTranslator = (*Translator)(nil)
	_ translate.Warmer           = (*Translator)(nil)
)

func (t *Translator) Kind() domain.ProviderKind { return api.ProviderKindLocal }

// Translate translates req; it streams internally so cancellation stops
// generation early.
func (t *Translator) Translate(ctx context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	return t.TranslateStream(ctx, req, nil)
}

// TranslateStream translates req, passing the text so far to partial.
func (t *Translator) TranslateStream(ctx context.Context, req domain.TranslateRequest, partial func(text string)) (domain.TranslateResult, error) {
	url, model, err := t.resolve(ctx)
	if err != nil {
		return domain.TranslateResult{}, err
	}
	release, err := t.acquire(ctx, url, model)
	if err != nil {
		return domain.TranslateResult{}, err
	}
	defer release()

	p := translate.BuildPrompt(req)
	body := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: p.System},
			{Role: "user", Content: p.User},
		},
		Stream:    true,
		KeepAlive: t.keepAlive(),
		Think:     new(bool), // false: Gemma 4 and other thinking models answer directly
		Options:   &chatOptions{Temperature: 0.2, NumPredict: 256},
	}
	resp, err := t.post(ctx, url, body)
	if err != nil {
		return domain.TranslateResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var text strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk chatChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			return domain.TranslateResult{}, fmt.Errorf("ollama: bad stream line: %w", err)
		}
		if chunk.Error != "" {
			return domain.TranslateResult{}, fmt.Errorf("ollama: %s", chunk.Error)
		}
		if c := chunk.Message.Content; c != "" {
			text.WriteString(c)
			if partial != nil {
				partial(text.String())
			}
		}
		if chunk.Done {
			res := domain.TranslateResult{Text: translate.CleanOutput(text.String())}
			if res.Text == "" && strings.TrimSpace(req.Text) != "" {
				return res, errors.New("ollama: empty reply")
			}
			// Local tokens cost nothing, so they stay out of the usage
			// that feeds the cost estimate.
			return res, nil
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return domain.TranslateResult{}, ctx.Err()
		}
		return domain.TranslateResult{}, fmt.Errorf("ollama: read stream: %w", err)
	}
	return domain.TranslateResult{}, errors.New("ollama: stream ended before done")
}

// Warm loads the model into memory with keep_alive, so the first caption
// of a session doesn't wait for it (an empty chat only loads the model).
func (t *Translator) Warm(ctx context.Context) error {
	url, model, err := t.resolve(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, WarmTimeout)
	defer cancel()
	resp, err := t.post(ctx, url, chatRequest{Model: model, Messages: []chatMessage{}, KeepAlive: t.keepAlive()})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// post sends a chat request and checks the status.
func (t *Translator) post(ctx context.Context, url string, body chatRequest) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/")+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	client := t.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ollama unreachable at %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		var e struct {
			Error string `json:"error"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		if json.Unmarshal(raw, &e) != nil || e.Error == "" {
			e.Error = strings.TrimSpace(string(raw))
		}
		return nil, fmt.Errorf("ollama: %s (HTTP %d, model %s)", e.Error, resp.StatusCode, body.Model)
	}
	return resp, nil
}

// resolve returns the Ollama URL and model, reusing them for configTTL.
func (t *Translator) resolve(ctx context.Context) (url, model string, err error) {
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.url != "" && now().Before(t.expires) {
		return t.url, t.model, nil
	}
	url, model = t.URL, t.Model
	if t.Settings != nil {
		st, err := t.Settings(ctx)
		switch {
		case err == nil:
			if l := st.Providers.Local; l.OllamaUrl != "" {
				url = l.OllamaUrl
			}
			if l := st.Providers.Local; l.GemmaModel != "" {
				model = l.GemmaModel
			}
		case !errors.Is(err, domain.ErrNotFound):
			return "", "", fmt.Errorf("gemma: read settings: %w", err)
		}
	}
	if url == "" {
		url = DefaultURL
	}
	if model == "" {
		model = DefaultModel
	}
	t.url, t.model, t.expires = url, model, now().Add(configTTL)
	return url, model, nil
}

// acquire takes a slot of the model's concurrency limit.
func (t *Translator) acquire(ctx context.Context, url, model string) (release func(), err error) {
	n := t.Concurrency
	if n <= 0 {
		n = DefaultConcurrency
	}
	key := url + "\x00" + model
	t.mu.Lock()
	if t.sems == nil {
		t.sems = map[string]chan struct{}{}
	}
	sem := t.sems[key]
	if sem == nil {
		sem = make(chan struct{}, n)
		t.sems[key] = sem
	}
	t.mu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *Translator) keepAlive() string {
	d := t.KeepAlive
	if d <= 0 {
		d = DefaultKeepAlive
	}
	return d.String()
}

// Ollama /api/chat wire types.
type (
	chatRequest struct {
		Model     string        `json:"model"`
		Messages  []chatMessage `json:"messages"`
		Stream    bool          `json:"stream"`
		KeepAlive string        `json:"keep_alive,omitempty"`
		Think     *bool         `json:"think,omitempty"`
		Options   *chatOptions  `json:"options,omitempty"`
	}
	chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	chatOptions struct {
		Temperature float32 `json:"temperature"`
		NumPredict  int     `json:"num_predict,omitempty"`
	}
	chatChunk struct {
		Message chatMessage `json:"message"`
		Done    bool        `json:"done"`
		Error   string      `json:"error"`
	}
)
