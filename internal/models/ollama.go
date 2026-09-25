// SPDX-License-Identifier: Apache-2.0

package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// ollamaModels lists the models Ollama has (GET /api/tags).
func (m *Manager) ollamaModels(ctx context.Context) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, ollamaTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.ollamaURL(ctx)+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tags: %s", resp.Status)
	}
	var tags struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&tags); err != nil {
		return nil, fmt.Errorf("tags: %w", err)
	}
	have := map[string]bool{}
	for _, t := range tags.Models {
		have[t.Name], have[t.Model] = true, true
	}
	return have, nil
}

// hasTag matches Ollama's names, where a missing tag means ":latest".
func hasTag(have map[string]bool, name string) bool {
	if have[name] {
		return true
	}
	if !strings.Contains(name, ":") {
		return have[name+":latest"]
	}
	return false
}

// pullLine is one NDJSON line of POST /api/pull.
type pullLine struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error"`
}

// pullGemma has Ollama pull mod (POST /api/pull, streamed). Ollama
// resumes partial layers and checks their SHA-256 digests itself; the
// progress is the sum over the layers.
func (m *Manager) pullGemma(ctx context.Context, base string, mod Model) error {
	body, err := json.Marshal(map[string]any{"model": mod.Name, "stream": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.opts.HTTPClient.Do(req)
	if err != nil {
		return &domain.CodedError{Code: CodeOllamaUnreachable, Message: "Ollama isn't reachable: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return pullFailed(mod, fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(msg))))
	}

	totals, completed := map[string]int64{}, map[string]int64{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var l pullLine
		if err := json.Unmarshal(line, &l); err != nil {
			return pullFailed(mod, "bad progress line: "+err.Error())
		}
		switch {
		case l.Error != "":
			return pullFailed(mod, l.Error)
		case l.Status == "success":
			return nil
		case strings.HasPrefix(l.Status, "verifying"):
			m.update(mod, api.LocalModelStatusVerifying, 1, sum(totals))
		case l.Digest != "" && l.Total > 0:
			totals[l.Digest], completed[l.Digest] = l.Total, l.Completed
			total := sum(totals)
			m.update(mod, api.LocalModelStatusDownloading, float32(float64(sum(completed))/float64(total)), total)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return pullFailed(mod, "the pull ended without success")
}

func sum(m map[string]int64) int64 {
	var n int64
	for _, v := range m {
		n += v
	}
	return n
}

func pullFailed(mod Model, why string) error {
	return &domain.CodedError{Code: CodePullFailed, Params: map[string]any{"model": mod.Name},
		Message: fmt.Sprintf("ollama pull %s: %s", mod.Name, why)}
}
