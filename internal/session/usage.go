// SPDX-License-Identifier: Apache-2.0

package session

import (
	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// totals is what a session has used across its runs since the server
// started (AI-9). Each run is priced for its own provider, so a session
// that moves from Gemini to local keeps the cost it already ran up.
type totals struct {
	usage   domain.Usage
	costUSD float64
}

func (t totals) add(u domain.Usage, costUSD float64) totals {
	return totals{usage: t.usage.Add(u), costUSD: t.costUSD + costUSD}
}

// stats is the SessionStatus.usage view. Tokens are left out until a
// provider reports some.
func (t totals) stats() *api.UsageStats {
	secs, cost := float32(t.usage.AudioSeconds), float32(t.costUSD)
	s := &api.UsageStats{AudioSeconds: &secs, EstimatedCostUsd: &cost}
	if t.usage.InputTokens > 0 || t.usage.OutputTokens > 0 {
		in, out := t.usage.InputTokens, t.usage.OutputTokens
		s.InputTokens, s.OutputTokens = &in, &out
	}
	return s
}
