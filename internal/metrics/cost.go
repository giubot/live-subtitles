// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Prices are what one provider charges, in US dollars (AI-9).
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

// DefaultGeminiPrices are estimates from Google's published pay-as-you-go
// list prices (2025): Gemini 2.5 Flash text at $0.30 / $2.50 per million
// input / output tokens, and Live API audio input at $3.00 per million
// tokens × 32 tokens per second ≈ $0.00576 per minute. They go stale:
// override them with the --gemini-*-usd-* flags.
var DefaultGeminiPrices = Prices{InputPerMTok: 0.30, OutputPerMTok: 2.50, AudioPerMin: 0.00576}

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

// Pricing prices usage by provider. Only Gemini is billed: the local
// provider runs on the event's own hardware and mock is free.
type Pricing struct {
	Gemini Prices
}

// DefaultPricing uses DefaultGeminiPrices.
func DefaultPricing() Pricing { return Pricing{Gemini: DefaultGeminiPrices} }

// Cost is the estimated price of u on provider kind.
func (p Pricing) Cost(kind domain.ProviderKind, u domain.Usage) float64 {
	if kind == api.ProviderKindGemini {
		return p.Gemini.Cost(u)
	}
	return 0
}
