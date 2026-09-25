// SPDX-License-Identifier: Apache-2.0

package hwcheck

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/local/gemma"
	"github.com/iencodev/live-subtitles/internal/provider/local/whisper"
)

// runtimesTTL is how long a runtime check is reused, so health probes
// don't hit the sidecars on every call.
const runtimesTTL = 5 * time.Second

// BenchmarkFile is where the last benchmark is kept in the data directory.
const BenchmarkFile = "benchmark.json"

// Options configure a Checker.
type Options struct {
	// Settings supplies providers.local (sidecar URLs and models); nil or
	// ErrNotFound uses the defaults.
	Settings func(ctx context.Context) (api.Settings, error)
	// FFmpeg probes ffmpeg; nil probes "ffmpeg" in PATH.
	FFmpeg *ffmpeg.Prober
	// ASR and Translator are the local provider the benchmark runs; nil
	// makes the benchmark answer runtime_unavailable.
	ASR        domain.ASRProvider
	Translator domain.Translator
	// DataDir keeps the last benchmark (BenchmarkFile); empty keeps it in
	// memory only.
	DataDir string
	// ModelsDir is where whisper models are downloaded (--models-dir); the
	// report gives it as an absolute path so the setup guide can show the
	// whisper-server command. Empty leaves it out.
	ModelsDir string
	// HTTPClient checks the sidecars (default http.DefaultClient; every
	// call has its own timeout).
	HTTPClient *http.Client
	Logger     *slog.Logger
	// Now defaults to time.Now.
	Now func() time.Time
}

// Checker is the hardware self-check and benchmark (AI-12).
type Checker struct {
	opts Options
	sys  system

	hwOnce sync.Once
	hw     Hardware

	mu        sync.Mutex
	runtimes  Runtimes
	checkedAt time.Time
	last      *api.BenchmarkResult

	bench sync.Mutex
}

// New returns a Checker; it reads the last benchmark from DataDir.
func New(opts Options) *Checker {
	if opts.FFmpeg == nil {
		opts.FFmpeg = &ffmpeg.Prober{}
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	c := &Checker{opts: opts, sys: hostSystem()}
	c.last = c.loadBenchmark()
	return c
}

// Hardware detects the machine once; it doesn't change while running.
func (c *Checker) Hardware(ctx context.Context) Hardware {
	c.hwOnce.Do(func() { c.hw = c.sys.detect(context.WithoutCancel(ctx)) })
	return c.hw
}

// Recommendation is the table's pick for this machine; after a benchmark,
// RealtimeLikely is its measurement.
func (c *Checker) Recommendation(ctx context.Context) Recommendation {
	r := Recommend(c.Hardware(ctx))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last != nil {
		r.RealtimeLikely = c.last.Ok
	}
	return r
}

// Runtimes checks whisper-server, Ollama and ffmpeg, reusing a result
// younger than a few seconds.
func (c *Checker) Runtimes(ctx context.Context) Runtimes {
	c.mu.Lock()
	if !c.checkedAt.IsZero() && c.opts.Now().Sub(c.checkedAt) < runtimesTTL {
		r := c.runtimes
		c.mu.Unlock()
		return r
	}
	c.mu.Unlock()

	local := c.local(ctx)
	var r Runtimes
	var wg sync.WaitGroup
	wg.Go(func() { r.Whisper = checkWhisper(ctx, c.opts.HTTPClient, local.whisperURL) })
	wg.Go(func() { r.Ollama = checkOllama(ctx, c.opts.HTTPClient, local.ollamaURL) })
	wg.Go(func() { r.FFmpeg, r.SRT = checkFFmpeg(ctx, c.opts.FFmpeg) })
	wg.Wait()

	c.mu.Lock()
	c.runtimes, c.checkedAt = r, c.opts.Now()
	c.mu.Unlock()
	return r
}

// SupportsSRT reports whether ffmpeg can read srt:// inputs.
func (c *Checker) SupportsSRT(ctx context.Context) bool { return c.opts.FFmpeg.SupportsSRT(ctx) }

// reportGPU is the generated element type of HardwareReport.Gpus.
type reportGPU = struct {
	Backend     api.HardwareReportGpusBackend `json:"backend"`
	MemoryBytes *int64                        `json:"memoryBytes,omitempty"`
	Name        string                        `json:"name"`
}

// Report is GET /api/system/hardware.
func (c *Checker) Report(ctx context.Context) api.HardwareReport {
	h := c.Hardware(ctx)
	rt := c.Runtimes(ctx)
	rec := c.Recommendation(ctx)
	rep := api.HardwareReport{
		Os: h.OS, Arch: h.Arch, MemoryBytes: h.MemoryBytes,
		Gpus: []reportGPU{},
	}
	rep.Cpu.Model, rep.Cpu.Cores = h.CPUModel, h.Cores
	for _, g := range h.GPUs {
		gpu := reportGPU{Backend: g.Backend, Name: g.Name}
		if g.MemoryBytes > 0 {
			gpu.MemoryBytes = &g.MemoryBytes
		}
		rep.Gpus = append(rep.Gpus, gpu)
	}
	rep.Runtimes.Whisper, rep.Runtimes.Ollama, rep.Runtimes.Ffmpeg = rt.Whisper, rt.Ollama, rt.FFmpeg
	rep.Recommendation.WhisperModel = rec.WhisperModel
	rep.Recommendation.GemmaModel = rec.GemmaModel
	rep.Recommendation.LocalRealtimeLikely = rec.RealtimeLikely
	if c.opts.ModelsDir != "" {
		dir, err := filepath.Abs(c.opts.ModelsDir)
		if err != nil {
			dir = c.opts.ModelsDir
		}
		rep.ModelsDir = &dir
	}
	c.mu.Lock()
	rep.LastBenchmark = c.last
	c.mu.Unlock()
	return rep
}

// Benchmark runs the bundled clip through the local provider (POST
// /api/system/benchmark). It returns ErrBusy while another run is going,
// and a *domain.CodedError when a sidecar is missing or fails.
func (c *Checker) Benchmark(ctx context.Context) (api.BenchmarkResult, error) {
	if !c.bench.TryLock() {
		return api.BenchmarkResult{}, ErrBusy
	}
	defer c.bench.Unlock()
	if c.opts.ASR == nil || c.opts.Translator == nil {
		return api.BenchmarkResult{}, unavailable("whisper-server", errors.New("the local provider is not configured"))
	}
	clip, err := Clip()
	if err != nil {
		return api.BenchmarkResult{}, err
	}
	local := c.local(ctx)
	res, err := benchmark(ctx, c.opts.ASR, c.opts.Translator, clip, c.opts.Now)
	if err != nil {
		c.opts.Logger.Warn("benchmark failed", "err", err)
		return api.BenchmarkResult{}, err
	}
	res.WhisperModel, res.GemmaModel = &local.whisperModel, &local.gemmaModel
	c.opts.Logger.Info("benchmark finished", "rtf", res.RealTimeFactor, "asr_ms", res.AsrMs,
		"translation_ms", res.TranslationMs, "whisper", local.whisperModel, "gemma", local.gemmaModel)
	c.mu.Lock()
	c.last = &res
	c.mu.Unlock()
	c.saveBenchmark(res)
	return res, nil
}

// localSettings are providers.local with the defaults filled in.
type localSettings struct {
	whisperURL, whisperModel, ollamaURL, gemmaModel string
}

func (c *Checker) local(ctx context.Context) localSettings {
	l := localSettings{whisper.DefaultURL, whisper.DefaultModel, gemma.DefaultURL, gemma.DefaultModel}
	if c.opts.Settings == nil {
		return l
	}
	s, err := c.opts.Settings(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			c.opts.Logger.Warn("hardware check: read settings", "err", err)
		}
		return l
	}
	p := s.Providers.Local
	for _, f := range []struct {
		dst *string
		v   string
	}{
		{&l.whisperURL, p.WhisperUrl}, {&l.whisperModel, p.WhisperModel},
		{&l.ollamaURL, p.OllamaUrl}, {&l.gemmaModel, p.GemmaModel},
	} {
		if v := strings.TrimSpace(f.v); v != "" {
			*f.dst = v
		}
	}
	l.whisperURL, l.ollamaURL = strings.TrimRight(l.whisperURL, "/"), strings.TrimRight(l.ollamaURL, "/")
	return l
}

func (c *Checker) benchmarkPath() string {
	if c.opts.DataDir == "" {
		return ""
	}
	return filepath.Join(c.opts.DataDir, BenchmarkFile)
}

func (c *Checker) loadBenchmark() *api.BenchmarkResult {
	p := c.benchmarkPath()
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.opts.Logger.Warn("read last benchmark", "err", err)
		}
		return nil
	}
	var r api.BenchmarkResult
	if err := json.Unmarshal(b, &r); err != nil {
		c.opts.Logger.Warn("read last benchmark", "err", err)
		return nil
	}
	return &r
}

func (c *Checker) saveBenchmark(r api.BenchmarkResult) {
	p := c.benchmarkPath()
	if p == "" {
		return
	}
	b, err := json.Marshal(r)
	if err == nil {
		err = os.WriteFile(p, b, 0o600)
	}
	if err != nil {
		c.opts.Logger.Warn("save benchmark", "err", err)
	}
}
