// SPDX-License-Identifier: Apache-2.0

package session

import (
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// srtSource is a source that reports SRT connection stats.
type srtSource struct {
	fake.Source
	stats api.SrtStats
}

func (s *srtSource) SRTStats() api.SrtStats { return s.stats }

func TestStatusReportsSRTStats(t *testing.T) {
	connected, kbps := true, float32(128)
	tests := []struct {
		name    string
		src     domain.AudioSource
		wantSRT bool
	}{
		{"srt source", &srtSource{Source: fake.Source{Realtime: true},
			stats: api.SrtStats{Connected: &connected, BitrateKbps: &kbps}}, true},
		{"other source", &fake.Source{Realtime: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, Options{}, sess("main", "es"))
			st, err := e.m.Start(t.Context(), "main", tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if (st.Srt != nil) != tt.wantSRT {
				t.Fatalf("srt stats %+v, want present=%v", st.Srt, tt.wantSRT)
			}
			if tt.wantSRT && (st.Srt.Connected == nil || !*st.Srt.Connected || *st.Srt.BitrateKbps != kbps) {
				t.Errorf("srt stats %+v", st.Srt)
			}
			if _, err := e.m.Stop(t.Context(), "main"); err != nil {
				t.Fatal(err)
			}
			if st := e.m.StatusOf("main"); st.Srt != nil {
				t.Errorf("stopped session reports srt %+v", st.Srt)
			}
		})
	}
}
