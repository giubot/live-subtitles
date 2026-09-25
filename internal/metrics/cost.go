// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Prices are what one model charges, in US dollars (AI-9).
//
// Tokens are text tokens (translation prompts and results, transcripts).
// Audio input is priced per minute from Usage.AudioSeconds, so a provider
// reports the audio it streams as seconds, not as input tokens, or it would
// be counted twice.
type Prices struct {
	InputPerMTok  float64 // per million input tokens
	OutputPerMTok float64 // per million output tokens
	AudioPerMin   float64 // per minute of audio sent
}

// Default Gemini prices are estimates from Google's published pay-as-you-go
// list prices (September 2026). They go stale: override them with the
// --gemini-*-usd-* flags.
var (
	// DefaultGeminiASRPrices: gemini-3.5-transcribe-live, $0.005 per minute
	// of audio in and $21 per million transcript tokens out.
	DefaultGeminiASRPrices = Prices{OutputPerMTok: 21.00, AudioPerMin: 0.005}
	// DefaultGeminiTranslationPrices: gemini-3.5-flash-lite, $0.30 / $2.50
	// per million input / output tokens.
	DefaultGeminiTranslationPrices = Prices{InputPerMTok: 0.30, OutputPerMTok: 2.50}
)

// ErrNegativePrice rejects a price below zero.
var ErrNegativePrice = errors.New("metrics: a price can't be negative")

// Validate reports a negative price.
func (p Prices) Validate() error {
	if p.InputPerMTok < 0 || p.OutputPerMTok < 0 || p.AudioPerMin < 0 {
		return ErrNegativePrice
	}
	return nil
}

// Cost is the estimated price of u.
func (p Prices) Cost(u domain.Usage) float64 {
	return float64(u.InputTokens)/1e6*p.InputPerMTok +
		float64(u.OutputTokens)/1e6*p.OutputPerMTok +
		u.AudioSeconds/60*p.AudioPerMin
}

// Pricing prices usage by provider. Speech recognition and translation run
// on different models, so each has its own prices. Only Gemini is billed:
// the local provider runs on the event's own hardware and mock is free.
type Pricing struct {
	GeminiASR         Prices
	GeminiTranslation Prices
}

// DefaultPricing uses the default Gemini prices.
func DefaultPricing() Pricing {
	return Pricing{GeminiASR: DefaultGeminiASRPrices, GeminiTranslation: DefaultGeminiTranslationPrices}
}

// Cost is the estimated price, on provider kind, of asr (speech
// recognition) and translation usage.
func (p Pricing) Cost(kind domain.ProviderKind, asr, translation domain.Usage) float64 {
	if kind == api.ProviderKindGemini {
		return p.GeminiASR.Cost(asr) + p.GeminiTranslation.Cost(translation)
	}
	return 0
}
