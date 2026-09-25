// SPDX-License-Identifier: Apache-2.0

package selector

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/provider/local/gemma"
	"github.com/iencodev/live-subtitles/internal/provider/local/whisper"
)

// probeTimeout bounds each sidecar probe.
const probeTimeout = 1500 * time.Millisecond

// LocalProbe checks that the local provider's sidecars answer: whisper-server
// (GET /health) and Ollama (GET /api/version), at the URLs in the settings.
// It only proves they are up; model problems still show at session start.
type LocalProbe struct {
	// Settings supplies providers.local.{whisperUrl,ollamaUrl}; nil or an
	// error uses the defaults.
	Settings func(ctx context.Context) (api.Settings, error)
	// Client defaults to http.DefaultClient.
	Client *http.Client
}

// Probe reports whether both sidecars answer, with a translatable reason
// when one doesn't. It is Options.Local.
func (p LocalProbe) Probe(ctx context.Context) (bool, string) {
	whisperURL, ollamaURL := whisper.DefaultURL, gemma.DefaultURL
	if p.Settings != nil {
		// Unreadable settings fall back to the defaults, like the providers do
		// for ErrNotFound.
		if st, err := p.Settings(ctx); err == nil {
			if u := strings.TrimSpace(st.Providers.Local.WhisperUrl); u != "" {
				whisperURL = u
			}
			if u := strings.TrimSpace(st.Providers.Local.OllamaUrl); u != "" {
				ollamaURL = u
			}
		}
	}
	w, o := make(chan bool, 1), make(chan bool, 1)
	// Older whisper-servers answer 404 on /health, and it answers 503 while
	// it loads its model: both prove it is up.
	go func() { w <- p.get(ctx, strings.TrimRight(whisperURL, "/")+"/health", true) }()
	go func() { o <- p.get(ctx, strings.TrimRight(ollamaURL, "/")+"/api/version", false) }()
	wOK, oOK := <-w, <-o
	switch {
	case !wOK:
		return false, CodeWhisperUnreachable
	case !oOK:
		return false, CodeOllamaUnreachable
	}
	return true, ""
}

// get reports whether url answers 2xx (or any status when anyStatus).
func (p LocalProbe) get(ctx context.Context, url string, anyStatus bool) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	c := p.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	return anyStatus || resp.StatusCode < 300
}
