// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"errors"
	"math"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestCost(t *testing.T) {
	pricing := Pricing{
		GeminiASR:         Prices{InputPerMTok: 1, OutputPerMTok: 20, AudioPerMin: 0.006},
		GeminiTranslation: Prices{InputPerMTok: 0.30, OutputPerMTok: 2.50},
	}
	for _, c := range []struct {
		name             string
		kind             domain.ProviderKind
		asr, translation domain.Usage
		want             float64
	}{
		{"nothing used", api.ProviderKindGemini, domain.Usage{}, domain.Usage{}, 0},
		{"an hour of audio", api.ProviderKindGemini, domain.Usage{AudioSeconds: 3600}, domain.Usage{}, 0.36},
		{"transcript tokens", api.ProviderKindGemini, domain.Usage{InputTokens: 1_000_000, OutputTokens: 100_000}, domain.Usage{}, 1 + 2},
		{"translation tokens", api.ProviderKindGemini, domain.Usage{}, domain.Usage{InputTokens: 1_000_000, OutputTokens: 200_000}, 0.30 + 0.50},
		{"both", api.ProviderKindGemini, domain.Usage{AudioSeconds: 600, OutputTokens: 50_000}, domain.Usage{InputTokens: 500_000, OutputTokens: 100_000}, 0.06 + 1 + 0.15 + 0.25},
		{"local is free", api.ProviderKindLocal, domain.Usage{AudioSeconds: 3600}, domain.Usage{InputTokens: 1_000_000}, 0},
		{"mock is free", api.ProviderKindMock, domain.Usage{AudioSeconds: 3600}, domain.Usage{OutputTokens: 1_000_000}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := pricing.Cost(c.kind, c.asr, c.translation); math.Abs(got-c.want) > 1e-9 {
				t.Errorf("Cost = %.9f, want %.9f", got, c.want)
			}
		})
	}
}

func TestDefaultGeminiAudioHour(t *testing.T) {
	// One hour of streamed audio, the bulk of a session's cost, stays
	// around 30 cents at the default prices.
	got := DefaultPricing().Cost(api.ProviderKindGemini, domain.Usage{AudioSeconds: 3600}, domain.Usage{})
	if got < 0.25 || got > 0.35 {
		t.Errorf("an hour of Gemini audio costs $%.4f", got)
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct {
		name string
		p    Prices
		err  error
	}{
		{"ASR defaults", DefaultGeminiASRPrices, nil},
		{"translation defaults", DefaultGeminiTranslationPrices, nil},
		{"free", Prices{}, nil},
		{"negative input", Prices{InputPerMTok: -1}, ErrNegativePrice},
		{"negative output", Prices{OutputPerMTok: -0.1}, ErrNegativePrice},
		{"negative audio", Prices{AudioPerMin: -0.01}, ErrNegativePrice},
	} {
		if err := c.p.Validate(); !errors.Is(err, c.err) {
			t.Errorf("%s: %v, want %v", c.name, err, c.err)
		}
	}
}
