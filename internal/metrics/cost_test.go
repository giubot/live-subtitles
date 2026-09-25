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
	prices := Prices{InputPerMTok: 0.30, OutputPerMTok: 2.50, AudioPerMin: 0.006}
	pricing := Pricing{Gemini: prices}
	for _, c := range []struct {
		name  string
		kind  domain.ProviderKind
		usage domain.Usage
		want  float64
	}{
		{"nothing used", api.ProviderKindGemini, domain.Usage{}, 0},
		{"a million input tokens", api.ProviderKindGemini, domain.Usage{InputTokens: 1_000_000}, 0.30},
		{"output tokens", api.ProviderKindGemini, domain.Usage{OutputTokens: 200_000}, 0.50},
		{"an hour of audio", api.ProviderKindGemini, domain.Usage{AudioSeconds: 3600}, 0.36},
		{"everything", api.ProviderKindGemini, domain.Usage{AudioSeconds: 600, InputTokens: 500_000, OutputTokens: 100_000}, 0.06 + 0.15 + 0.25},
		{"local is free", api.ProviderKindLocal, domain.Usage{AudioSeconds: 3600, InputTokens: 1_000_000}, 0},
		{"mock is free", api.ProviderKindMock, domain.Usage{AudioSeconds: 3600, OutputTokens: 1_000_000}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := pricing.Cost(c.kind, c.usage); math.Abs(got-c.want) > 1e-9 {
				t.Errorf("Cost = %.9f, want %.9f", got, c.want)
			}
		})
	}
}

func TestDefaultGeminiAudioHour(t *testing.T) {
	// One hour of streamed audio, the bulk of a session's cost, stays
	// around 35 cents at the default prices.
	got := DefaultPricing().Cost(api.ProviderKindGemini, domain.Usage{AudioSeconds: 3600})
	if got < 0.30 || got > 0.40 {
		t.Errorf("an hour of Gemini audio costs $%.4f", got)
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct {
		name string
		p    Prices
		err  error
	}{
		{"defaults", DefaultGeminiPrices, nil},
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
