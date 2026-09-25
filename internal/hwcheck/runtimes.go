// SPDX-License-Identifier: Apache-2.0

package hwcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
)

// runtimeTimeout bounds each reachability check.
const runtimeTimeout = 2 * time.Second

// Runtimes is the reachability of the local provider's sidecars and ffmpeg.
type Runtimes struct {
	Whisper, Ollama, FFmpeg api.RuntimeStatus
	// SRT means ffmpeg can read srt:// inputs (SRT ingest).
	SRT bool
}

// checkWhisper asks whisper-server for GET /health. Servers older than
// /health answer 404, which still proves they are up; 503 means the model
// is still loading.
func checkWhisper(ctx context.Context, hc *http.Client, base string) api.RuntimeStatus {
	st := api.RuntimeStatus{Url: &base}
	resp, err := get(ctx, hc, base+"/health")
	if err != nil {
		st.Detail = detail(err.Error())
		return st
	}
	defer drain(resp)
	switch {
	case resp.StatusCode < 300, resp.StatusCode == http.StatusNotFound:
		st.Reachable = true
	case resp.StatusCode == http.StatusServiceUnavailable:
		st.Reachable = true
		st.Detail = detail("loading its model")
	default:
		st.Detail = detail("health: " + resp.Status)
	}
	return st
}

// checkOllama asks Ollama for GET /api/version.
func checkOllama(ctx context.Context, hc *http.Client, base string) api.RuntimeStatus {
	st := api.RuntimeStatus{Url: &base}
	resp, err := get(ctx, hc, base+"/api/version")
	if err != nil {
		st.Detail = detail(err.Error())
		return st
	}
	defer drain(resp)
	if resp.StatusCode >= 300 {
		st.Detail = detail("version: " + resp.Status)
		return st
	}
	st.Reachable = true
	var v struct {
		Version string `json:"version"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<12)).Decode(&v) == nil && v.Version != "" {
		st.Version = &v.Version
	}
	return st
}

// checkFFmpeg reports whether ffmpeg runs and whether it has libsrt.
func checkFFmpeg(ctx context.Context, p *ffmpeg.Prober) (api.RuntimeStatus, bool) {
	caps, err := p.Capabilities(ctx)
	if err != nil {
		return api.RuntimeStatus{Detail: detail(err.Error())}, false
	}
	st := api.RuntimeStatus{Reachable: true}
	if caps.Version != "" {
		st.Version = &caps.Version
	}
	if caps.SRT {
		st.Detail = detail("libsrt: SRT ingest available")
	} else {
		st.Detail = detail("no libsrt: SRT ingest unavailable")
	}
	return st, caps.SRT
}

func get(ctx context.Context, hc *http.Client, url string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, runtimeTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("not reachable: %w", err)
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

func detail(s string) *string {
	s = strings.TrimSpace(s)
	return &s
}
