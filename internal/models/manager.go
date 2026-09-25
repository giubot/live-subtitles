// SPDX-License-Identifier: Apache-2.0

package models

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/local/gemma"
)

// Error codes of model downloads (UI-4).
const (
	CodeNotFound          = "model.not_found"
	CodeOllamaUnreachable = "model.ollama_unreachable"
	CodeDownloadFailed    = "model.download_failed"
	CodeChecksumMismatch  = "model.checksum_mismatch"
	CodePullFailed        = "model.pull_failed"
)

// Defaults of Options.
const (
	DefaultAttempts   = 3
	DefaultRetryDelay = 2 * time.Second
	// progressEvery limits modelProgress events: one per percent, or per
	// second while the percentage doesn't move.
	progressStep  = 0.01
	progressEvery = time.Second
	ollamaTimeout = 3 * time.Second
)

// Options configure a Manager.
type Options struct {
	// Dir is the models directory, shared with whisper-server (--model
	// <Dir>/ggml-<name>.bin). Created on the first download.
	Dir string
	// Settings supplies providers.local.ollamaUrl; nil or empty fields use
	// gemma.DefaultURL.
	Settings func(ctx context.Context) (api.Settings, error)
	// Recommended names the whisper model and Gemma tag the hardware check
	// recommends; nil recommends nothing.
	Recommended func(ctx context.Context) (whisper, gemma string)
	// Publish gets every status change and progress step (modelProgress
	// on /ws/admin); nil drops them.
	Publish func(api.LocalModel)
	// BaseURL is where whisper files are downloaded from (default
	// HuggingFaceBase); tests point it at an httptest server.
	BaseURL string
	// Catalog defaults to the package Catalog.
	Catalog []Model
	// HTTPClient defaults to a client whose only timeout is 30 s for the
	// response headers: a model download takes as long as it takes.
	HTTPClient *http.Client
	// Attempts and RetryDelay govern retries of a failed whisper download,
	// each resuming where the last stopped.
	Attempts   int
	RetryDelay time.Duration
	Logger     *slog.Logger
}

// Manager lists the local models and downloads them in the background
// (AI-13).
type Manager struct {
	opts   Options
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu sync.Mutex
	// jobs are running or failed downloads, by model ID. A finished one is
	// removed: the disk (or Ollama) then says it is ready.
	jobs map[string]*job
}

// job is one download's state.
type job struct {
	status   api.LocalModelStatus
	progress float32
	size     int64
	err      *api.Error
	// Throttling of progress events.
	sentProgress float32
	sentAt       time.Time
}

func (j *job) active() bool {
	return j.status == api.LocalModelStatusDownloading || j.status == api.LocalModelStatusVerifying
}

// New returns a Manager. Close stops its downloads.
func New(opts Options) *Manager {
	if opts.BaseURL == "" {
		opts.BaseURL = HuggingFaceBase
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.Catalog == nil {
		opts.Catalog = Catalog
	}
	if opts.HTTPClient == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.ResponseHeaderTimeout = 30 * time.Second
		opts.HTTPClient = &http.Client{Transport: t}
	}
	if opts.Attempts <= 0 {
		opts.Attempts = DefaultAttempts
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = DefaultRetryDelay
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{opts: opts, ctx: ctx, cancel: cancel, jobs: map[string]*job{}}
}

// Close cancels running downloads (a whisper file resumes next time) and
// waits for them to stop.
func (m *Manager) Close() {
	m.cancel()
	m.wg.Wait()
}

// List reports every catalog model (GET /api/models).
func (m *Manager) List(ctx context.Context) ([]api.LocalModel, error) {
	rw, rg := m.recommended(ctx)
	var pulled map[string]bool
	var ollamaErr error
	out := make([]api.LocalModel, 0, len(m.opts.Catalog))
	for _, mod := range m.opts.Catalog {
		lm := m.base(mod, rw, rg)
		m.mu.Lock()
		j := m.jobs[mod.ID]
		if j != nil && j.active() {
			m.fill(&lm, j)
			m.mu.Unlock()
			out = append(out, lm)
			continue
		}
		m.mu.Unlock()

		switch mod.Kind {
		case api.Whisper:
			m.whisperOnDisk(&lm, mod)
		case api.Gemma:
			if pulled == nil && ollamaErr == nil {
				pulled, ollamaErr = m.ollamaModels(ctx)
			}
			switch {
			case ollamaErr != nil:
				lm.Status = api.LocalModelStatusError
				lm.Error = ollamaUnreachable(ollamaErr)
			case hasTag(pulled, mod.Name):
				lm.Status = api.LocalModelStatusReady
			}
		}
		if lm.Status != api.LocalModelStatusReady && j != nil && j.err != nil {
			lm.Status, lm.Error = api.LocalModelStatusError, j.err
		}
		out = append(out, lm)
	}
	return out, nil
}

// Download starts or resumes a model download (POST
// /api/models/{id}/download) and returns the model's state. A model that
// is ready or already downloading is returned as it is. The errors are
// domain.ErrNotFound for an unknown ID and a *domain.CodedError with
// CodeOllamaUnreachable for Gemma without Ollama.
func (m *Manager) Download(ctx context.Context, id string) (api.LocalModel, error) {
	mod, ok := m.find(id)
	if !ok {
		return api.LocalModel{}, domain.ErrNotFound
	}
	rw, rg := m.recommended(ctx)
	lm := m.base(mod, rw, rg)

	m.mu.Lock()
	if j := m.jobs[id]; j != nil && j.active() {
		m.fill(&lm, j)
		m.mu.Unlock()
		return lm, nil
	}
	m.mu.Unlock()

	var ollamaURL string
	switch mod.Kind {
	case api.Whisper:
		if m.whisperOnDisk(&lm, mod); lm.Status == api.LocalModelStatusReady {
			return lm, nil
		}
	case api.Gemma:
		pulled, err := m.ollamaModels(ctx)
		if err != nil {
			e := ollamaUnreachable(err)
			return api.LocalModel{}, &domain.CodedError{Code: e.Code, Message: e.Message}
		}
		if hasTag(pulled, mod.Name) {
			lm.Status = api.LocalModelStatusReady
			return lm, nil
		}
		ollamaURL = m.ollamaURL(ctx)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if j := m.jobs[id]; j != nil && j.active() { // lost a race with another request
		m.fill(&lm, j)
		return lm, nil
	}
	var progress float32
	if lm.Progress != nil {
		progress = *lm.Progress
	}
	j := &job{status: api.LocalModelStatusDownloading, progress: progress, size: mod.Size, sentAt: time.Now()}
	m.jobs[id] = j
	m.fill(&lm, j)
	m.wg.Go(func() {
		var err error
		if mod.Kind == api.Whisper {
			err = m.fetchWhisper(m.ctx, mod)
		} else {
			err = m.pullGemma(m.ctx, ollamaURL, mod)
		}
		m.finish(mod, err)
	})
	m.opts.Logger.Info("model download started", "model", id)
	return lm, nil
}

// finish records the outcome of a download and publishes it.
func (m *Manager) finish(mod Model, err error) {
	if m.ctx.Err() != nil {
		m.mu.Lock()
		delete(m.jobs, mod.ID) // shutting down: a later start resumes
		m.mu.Unlock()
		return
	}
	rw, rg := m.recommended(m.ctx)
	lm := m.base(mod, rw, rg)
	m.mu.Lock()
	if err == nil {
		delete(m.jobs, mod.ID)
		lm.Status = api.LocalModelStatusReady
		m.opts.Logger.Info("model ready", "model", mod.ID)
	} else {
		e := asAPIError(err)
		j := m.jobs[mod.ID]
		j.status, j.err = api.LocalModelStatusError, &e
		m.fill(&lm, j)
		m.opts.Logger.Warn("model download failed", "model", mod.ID, "err", err)
	}
	m.mu.Unlock()
	m.publish(lm)
}

// update changes a running job and publishes it, throttled unless the
// status changes.
func (m *Manager) update(mod Model, status api.LocalModelStatus, progress float32, size int64) {
	m.mu.Lock()
	j := m.jobs[mod.ID]
	if j == nil {
		m.mu.Unlock()
		return
	}
	changed := j.status != status
	j.status, j.progress = status, progress
	if size > 0 {
		j.size = size
	}
	now := time.Now()
	if !changed && progress-j.sentProgress < progressStep && now.Sub(j.sentAt) < progressEvery {
		m.mu.Unlock()
		return
	}
	j.sentProgress, j.sentAt = progress, now
	m.mu.Unlock()

	rw, rg := m.recommended(m.ctx)
	lm := m.base(mod, rw, rg)
	m.mu.Lock()
	m.fill(&lm, j)
	m.mu.Unlock()
	m.publish(lm)
}

func (m *Manager) publish(lm api.LocalModel) {
	if m.opts.Publish != nil {
		m.opts.Publish(lm)
	}
}

func (m *Manager) find(id string) (Model, bool) {
	for _, mod := range m.opts.Catalog {
		if mod.ID == id {
			return mod, true
		}
	}
	return Model{}, false
}

func (m *Manager) recommended(ctx context.Context) (string, string) {
	if m.opts.Recommended == nil {
		return "", ""
	}
	return m.opts.Recommended(ctx)
}

func (m *Manager) base(mod Model, recWhisper, recGemma string) api.LocalModel {
	lm := api.LocalModel{Id: mod.ID, Kind: mod.Kind, Name: mod.Name, Status: api.LocalModelStatusMissing}
	if mod.Size > 0 {
		size := mod.Size
		lm.SizeBytes = &size
	}
	rec := (mod.Kind == api.Whisper && mod.Name == recWhisper) || (mod.Kind == api.Gemma && mod.Name == recGemma)
	lm.Recommended = &rec
	return lm
}

// fill copies a job's state into lm. Called with mu held.
func (m *Manager) fill(lm *api.LocalModel, j *job) {
	lm.Status = j.status
	p := j.progress
	lm.Progress = &p
	if j.size > 0 {
		s := j.size
		lm.SizeBytes = &s
	}
	lm.Error = j.err
}

// whisperOnDisk sets lm from the models directory: ready when the file has
// the catalog size (it was verified before it got its name), otherwise the
// share of a partial download that a new start resumes.
func (m *Manager) whisperOnDisk(lm *api.LocalModel, mod Model) {
	if st, err := os.Stat(filepath.Join(m.opts.Dir, mod.File())); err == nil && st.Size() == mod.Size {
		lm.Status = api.LocalModelStatusReady
		return
	}
	if st, err := os.Stat(m.partPath(mod)); err == nil && mod.Size > 0 && st.Size() <= mod.Size {
		p := float32(float64(st.Size()) / float64(mod.Size))
		lm.Progress = &p
	}
}

func (m *Manager) partPath(mod Model) string {
	return filepath.Join(m.opts.Dir, mod.File()+".part")
}

func (m *Manager) ollamaURL(ctx context.Context) string {
	u := gemma.DefaultURL
	if m.opts.Settings != nil {
		if s, err := m.opts.Settings(ctx); err == nil {
			if v := strings.TrimSpace(s.Providers.Local.OllamaUrl); v != "" {
				u = v
			}
		}
	}
	return strings.TrimRight(u, "/")
}

func ollamaUnreachable(err error) *api.Error {
	return &api.Error{Code: CodeOllamaUnreachable, Message: "Ollama isn't reachable: " + err.Error()}
}

// asAPIError maps a download error to an api.Error with a code.
func asAPIError(err error) api.Error {
	var ce *domain.CodedError
	if errors.As(err, &ce) {
		e := api.Error{Code: ce.Code, Message: ce.Message}
		if len(ce.Params) > 0 {
			p := ce.Params
			e.Params = &p
		}
		return e
	}
	return api.Error{Code: CodeDownloadFailed, Message: err.Error()}
}
