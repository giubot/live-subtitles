// SPDX-License-Identifier: Apache-2.0

//go:build ollama

package gemma

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// TestLiveLatency translates es↔en captions on a real Ollama and logs the
// latency:
//
//	GEMMA_MODEL=gemma3:4b go test -tags ollama -run TestLiveLatency -v ./internal/provider/local/gemma/
//
// OLLAMA_URL overrides the server (default DefaultURL).
func TestLiveLatency(t *testing.T) {
	tr := &Translator{URL: os.Getenv("OLLAMA_URL"), Model: os.Getenv("GEMMA_MODEL")}
	start := time.Now()
	if err := tr.Warm(t.Context()); err != nil {
		t.Skipf("Ollama not available: %v", err)
	}
	t.Logf("warm-up (model load) took %v", time.Since(start))

	g := &domain.Glossary{DoNotTranslate: []string{"Kubernetes", "React"}}
	tests := []struct {
		req  domain.TranslateRequest
		want string
	}{
		{domain.TranslateRequest{Text: "Kubernetes schedules pods across the nodes of the cluster.", From: "en", To: "es", Final: true, Glossary: g}, "Kubernetes"},
		{domain.TranslateRequest{Text: "Today we're going to talk about how we migrated our frontend to React.", From: "en", To: "es", Final: true, Glossary: g,
			Context: []string{"Hi everyone, thanks for coming."}}, "React"},
		{domain.TranslateRequest{Text: "Gracias a todos por venir a Nerdearla.", From: "es", To: "en", Final: true}, "Nerdearla"},
		{domain.TranslateRequest{Text: "Vamos a ver cómo desplegar esto en producción sin", From: "es", To: "en", Final: false}, ""},
		{domain.TranslateRequest{Text: "La latencia importa mucho cuando los subtítulos van en vivo.", From: "es", To: "en", Final: true,
			Context: []string{"Vamos a ver cómo desplegar esto en producción."}}, ""},
	}
	var lat []time.Duration
	for _, tt := range tests {
		start := time.Now()
		var first time.Duration
		res, err := tr.TranslateStream(t.Context(), tt.req, func(string) {
			if first == 0 {
				first = time.Since(start)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		d := time.Since(start)
		lat = append(lat, d)
		t.Logf("%s→%s final=%v: first token %v, total %v: %q", tt.req.From, tt.req.To, tt.req.Final, first.Round(time.Millisecond), d.Round(time.Millisecond), res.Text)
		if tt.want != "" && !strings.Contains(res.Text, tt.want) {
			t.Errorf("translation %q lacks %q", res.Text, tt.want)
		}
	}
	slices.Sort(lat)
	t.Logf("median %v, max %v", lat[len(lat)/2].Round(time.Millisecond), lat[len(lat)-1].Round(time.Millisecond))
}
