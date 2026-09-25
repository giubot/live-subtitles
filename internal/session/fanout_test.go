// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

type fakeSettings struct {
	s   api.Settings
	err error
}

func (f fakeSettings) Settings(context.Context) (api.Settings, error)  { return f.s, f.err }
func (f fakeSettings) PutSettings(context.Context, api.Settings) error { return nil }

func TestContextSentencesFromSettings(t *testing.T) {
	withK := func(k int) api.Settings {
		var s api.Settings
		s.Translation = &struct {
			ContextSentences *int `json:"contextSentences,omitempty"`
		}{ContextSentences: &k}
		return s
	}
	tests := []struct {
		name     string
		settings domain.SettingsStore
		want     int
	}{
		{name: "no settings store", want: 3},
		{name: "not saved yet", settings: fakeSettings{err: domain.ErrNotFound}, want: 3},
		{name: "read error", settings: fakeSettings{err: errors.New("disk")}, want: 3},
		{name: "unset", settings: fakeSettings{}, want: 3},
		{name: "zero", settings: fakeSettings{s: withK(0)}, want: 0},
		{name: "five", settings: fakeSettings{s: withK(5)}, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manager{opts: Options{Settings: tt.settings, ContextSentences: 3}, log: slog.New(slog.DiscardHandler)}
			if got := m.contextSentences(t.Context()); got != tt.want {
				t.Errorf("contextSentences = %d, want %d", got, tt.want)
			}
		})
	}
}
