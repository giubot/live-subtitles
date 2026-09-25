// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// DefaultLiveModel is the Live model used when the settings leave
// providers.gemini.liveModel empty.
const DefaultLiveModel = "gemini-2.5-flash-native-audio-preview-09-2025"

// ErrNoAPIKey means no Google API key is configured. The session manager
// reports it as provider.unavailable.
var ErrNoAPIKey = errors.New("gemini: no Google API key is set (add the google_api_key secret in Settings or set GEMINI_API_KEY)")

// ASR is a domain.ASRProvider on the Gemini Live API (AI-2). It streams the
// session audio and turns the input audio transcription into interim and
// final events (AI-6), tagged with the detected source language, English or
// Spanish (AI-10). Long talks survive the Live session limits through
// session resumption and context window compression, and dropped
// connections are reopened with capped backoff (5.5).
//
// The key and the model are read at every Start, so the admin can change
// them between sessions.
type ASR struct {
	// APIKey returns the Google API key. An empty key or ErrNoAPIKey make
	// Start fail with ErrNoAPIKey.
	APIKey func(ctx context.Context) (string, error)
	// Settings returns the admin settings; providers.gemini.liveModel picks
	// the model. Nil, domain.ErrNotFound or an empty model mean
	// DefaultLiveModel.
	Settings func(ctx context.Context) (api.Settings, error)
	Logger   *slog.Logger

	// Tuning; zero values use the defaults below.

	// SendEvery batches frames into one message per this much audio (40 ms).
	SendEvery time.Duration
	// IdleFinal finalizes a segment when no transcription arrived for this
	// much wall time (1.5 s).
	IdleFinal time.Duration
	// MaxBuffer is how much audio is kept while reconnecting (15 s); older
	// audio is dropped and logged as a gap.
	MaxBuffer time.Duration
	// MaxRetries is how many reconnects in a row may fail before the stream
	// ends with an error (8).
	MaxRetries int
	// Backoff is the first reconnect delay (500 ms), doubled up to MaxBackoff (10 s).
	Backoff, MaxBackoff time.Duration
	// DrainTimeout bounds how long the end of the audio waits for the last
	// transcription (4 s).
	DrainTimeout time.Duration

	// dial opens a Live connection; nil uses the genai SDK. Tests replace it.
	dial dialFunc
	// now is the wall clock; nil means time.Now.
	now func() time.Time
	// tick is the housekeeping interval; zero means defaultTick.
	tick time.Duration
}

var _ domain.ASRProvider = (*ASR)(nil)

func (a *ASR) Kind() domain.ProviderKind { return api.ProviderKindGemini }

// Start resolves the key and the model, opens the first Live connection and
// starts streaming. A missing key or a failed first connection is an error
// here, so the session doesn't go live on a provider that can't work.
func (a *ASR) Start(ctx context.Context, cfg domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	key, err := a.apiKey(ctx)
	if err != nil {
		return nil, nil, err
	}
	model := a.model(ctx)
	opts := a.withDefaults()
	lang := pinnedLanguage(cfg.SourceLanguage)
	dc := dialConfig{APIKey: key, Model: model, Language: lang, Glossary: glossaryTerms(cfg.Glossary)}

	conn, err := dialCtx(ctx, opts.dial, dc)
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: connect to the Live API (model %s): %w", model, err)
	}
	s := newStream(ctx, opts, cfg, dc)
	s.log.Info("gemini live connected", "model", model, "source_language", cfg.SourceLanguage)
	go s.run(conn)
	return s.in, s.out, nil
}

func (a *ASR) apiKey(ctx context.Context) (string, error) {
	if a.APIKey == nil {
		return "", ErrNoAPIKey
	}
	key, err := a.APIKey(ctx)
	switch {
	case errors.Is(err, ErrNoAPIKey):
		return "", ErrNoAPIKey
	case err != nil:
		return "", fmt.Errorf("gemini: read the Google API key: %w", err)
	case strings.TrimSpace(key) == "":
		return "", ErrNoAPIKey
	}
	return strings.TrimSpace(key), nil
}

func (a *ASR) model(ctx context.Context) string {
	if a.Settings == nil {
		return DefaultLiveModel
	}
	st, err := a.Settings(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			a.logger().Warn("read settings; using the default Gemini Live model", "err", err)
		}
		return DefaultLiveModel
	}
	if m := strings.TrimSpace(st.Providers.Gemini.LiveModel); m != "" {
		return m
	}
	return DefaultLiveModel
}

func (a *ASR) logger() *slog.Logger {
	if a.Logger != nil {
		return a.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// withDefaults returns a copy with every tuning field set.
func (a *ASR) withDefaults() ASR {
	o := ASR{
		Logger: a.logger(), SendEvery: a.SendEvery, IdleFinal: a.IdleFinal, MaxBuffer: a.MaxBuffer,
		MaxRetries: a.MaxRetries, Backoff: a.Backoff, MaxBackoff: a.MaxBackoff, DrainTimeout: a.DrainTimeout,
		dial: a.dial, now: a.now, tick: a.tick,
	}
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&o.SendEvery, 40*time.Millisecond)
	def(&o.IdleFinal, 1500*time.Millisecond)
	def(&o.MaxBuffer, 15*time.Second)
	def(&o.Backoff, 500*time.Millisecond)
	def(&o.MaxBackoff, 10*time.Second)
	def(&o.DrainTimeout, 4*time.Second)
	def(&o.tick, defaultTick)
	if o.MaxRetries <= 0 {
		o.MaxRetries = 8
	}
	if o.dial == nil {
		o.dial = dialGenAI
	}
	if o.now == nil {
		o.now = time.Now
	}
	return o
}

// pinnedLanguage is the pinned source language, or "" for auto.
func pinnedLanguage(s domain.SourceLanguage) domain.LanguageCode {
	switch s {
	case api.En, api.Es:
		return string(s)
	}
	return ""
}

// glossaryTerms lists the source terms of a glossary for the prompt (AI-7).
func glossaryTerms(g *domain.Glossary) []string {
	if g == nil {
		return nil
	}
	var terms []string
	seen := map[string]bool{}
	add := func(t string) {
		if t = strings.TrimSpace(t); t != "" && !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	for _, t := range g.Terms {
		add(t.Term)
	}
	for _, t := range g.DoNotTranslate {
		add(t)
	}
	return terms
}

// dialCtx dials, giving up when ctx ends (the SDK's dial ignores ctx).
func dialCtx(ctx context.Context, dial dialFunc, dc dialConfig) (liveConn, error) {
	type result struct {
		c   liveConn
		err error
	}
	res := make(chan result, 1)
	go func() {
		c, err := dial(ctx, dc)
		res <- result{c, err}
	}()
	select {
	case r := <-res:
		return r.c, r.err
	case <-ctx.Done():
		go func() {
			if r := <-res; r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, ctx.Err()
	}
}
