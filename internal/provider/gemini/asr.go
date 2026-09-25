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
// providers.gemini.liveModel empty: Gemini's streaming speech-to-text model.
const DefaultLiveModel = "gemini-3.5-transcribe-live"

// maxVocabulary caps the custom vocabulary sent to the model. The API takes
// up to 1,000 terms, but Google reports the best results with up to 100.
const maxVocabulary = 100

// ErrNoAPIKey means no Google API key is configured. The session manager
// reports it as provider.unavailable.
var ErrNoAPIKey = errors.New("gemini: no Google API key is set (add the google_api_key secret in Settings or set GEMINI_API_KEY)")

// ASR is a domain.ASRProvider on the Gemini Live API (AI-2) with a
// transcription model (gemini-3.5-transcribe-live). It streams the session
// audio and maps the server's interim transcription to interim events and
// its input transcription to the final of the same segment (AI-6), tagged
// with the source language, English or Spanish (AI-10). A Live
// transcription session streams for at most 10 minutes, so the provider
// opens the next connection before the limit and switches over at a pause;
// dropped connections are reopened with capped backoff (5.5).
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

	// SendEvery batches frames into one message per this much audio
	// (100 ms, the chunk size Google recommends).
	SendEvery time.Duration
	// IdleFinal is a safety net: an interim the server hasn't updated or
	// finalized for this much wall time becomes final (5 s).
	IdleFinal time.Duration
	// TurnGrace is how long after the end of a turn the server's final may
	// take before the open interim becomes final (1 s).
	TurnGrace time.Duration
	// RotateAfter is how long a connection is used before the next one is
	// opened, ahead of the 10-minute session limit (9 min).
	RotateAfter time.Duration
	// RotateGrace is how long the switch waits for a pause after
	// RotateAfter before it cuts mid-utterance (45 s).
	RotateGrace time.Duration
	// MaxBuffer is how much audio is kept while reconnecting (15 s); older
	// audio is dropped and logged as a gap.
	MaxBuffer time.Duration
	// MaxRetries is how many reconnects in a row may fail before the stream
	// ends with an error (8).
	MaxRetries int
	// Backoff is the first reconnect delay (500 ms), doubled up to MaxBackoff (10 s).
	Backoff, MaxBackoff time.Duration
	// DrainTimeout bounds how long the end of the audio, or a replaced
	// connection, waits for the last transcription (4 s).
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
	dc := dialConfig{APIKey: key, Model: model, Language: lang, Vocabulary: glossaryTerms(cfg.Glossary)}

	conn, err := dialCtx(ctx, opts.dial, dc)
	if err != nil {
		return nil, nil, coded(fmt.Errorf("gemini: connect to the Live API (model %s): %w", model, err))
	}
	s := newStream(ctx, opts, cfg, dc)
	s.log.Info("gemini live connected", "model", model, "source_language", cfg.SourceLanguage, "vocabulary", len(dc.Vocabulary))
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
		Logger: a.logger(), SendEvery: a.SendEvery, IdleFinal: a.IdleFinal, TurnGrace: a.TurnGrace,
		RotateAfter: a.RotateAfter, RotateGrace: a.RotateGrace, MaxBuffer: a.MaxBuffer, MaxRetries: a.MaxRetries,
		Backoff: a.Backoff, MaxBackoff: a.MaxBackoff, DrainTimeout: a.DrainTimeout,
		dial: a.dial, now: a.now, tick: a.tick,
	}
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&o.SendEvery, 100*time.Millisecond)
	def(&o.IdleFinal, 5*time.Second)
	def(&o.TurnGrace, time.Second)
	def(&o.RotateAfter, 9*time.Minute)
	def(&o.RotateGrace, 45*time.Second)
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

// glossaryTerms lists the terms and do-not-translate entries of a glossary,
// at most maxVocabulary, as the model's custom vocabulary (AI-7).
func glossaryTerms(g *domain.Glossary) []string {
	if g == nil {
		return nil
	}
	var terms []string
	seen := map[string]bool{}
	add := func(t string) {
		if t = strings.TrimSpace(t); t != "" && !seen[t] && len(terms) < maxVocabulary {
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
