// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/hwcheck"
)

// HardwareService is the hardware self-check and benchmark (hwcheck.Checker).
type HardwareService interface {
	Report(ctx context.Context) api.HardwareReport
	// Benchmark returns hwcheck.ErrBusy while one runs, or a
	// *domain.CodedError when the local provider can't run it.
	Benchmark(ctx context.Context) (api.BenchmarkResult, error)
	Runtimes(ctx context.Context) hwcheck.Runtimes
}

// BuildInfo is the version the binary was built as.
type BuildInfo struct {
	Version string
	Commit  string
	Mode    api.SystemInfoMode
}

// Health check values.
const (
	checkOK          = "ok"
	checkUnreachable = "unreachable"
	checkError       = "error"
)

// GetHealth is the liveness probe. Checks list the database and ffmpeg,
// which the server needs, and the local provider's sidecars, which only
// sessions on the local provider need: those don't make it degraded.
func (s *Server) GetHealth(ctx context.Context, _ api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	h := api.Health{Status: api.HealthStatusOk}
	checks := map[string]string{}
	if s.Settings != nil {
		checks["database"] = checkOK
		if _, err := s.Settings.Settings(ctx); err != nil && !errors.Is(err, domain.ErrNotFound) {
			checks["database"] = checkError
			h.Status = api.HealthStatusDegraded
		}
	}
	if s.Hardware != nil {
		rt := s.Hardware.Runtimes(ctx)
		checks["ffmpeg"] = reachability(rt.FFmpeg)
		checks["whisper"] = reachability(rt.Whisper)
		checks["ollama"] = reachability(rt.Ollama)
		if !rt.FFmpeg.Reachable {
			h.Status = api.HealthStatusDegraded
		}
	}
	if len(checks) > 0 {
		h.Checks = &checks
	}
	return api.GetHealth200JSONResponse(h), nil
}

func reachability(st api.RuntimeStatus) string {
	if st.Reachable {
		return checkOK
	}
	return checkUnreachable
}

// GetSystemInfo reports the build and what this server can do: SRT ingest
// needs ffmpeg with libsrt, recording needs ffmpeg.
func (s *Server) GetSystemInfo(ctx context.Context, _ api.GetSystemInfoRequestObject) (api.GetSystemInfoResponseObject, error) {
	info := api.SystemInfo{Version: s.Build.Version, Mode: s.Build.Mode}
	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Mode == "" {
		info.Mode = api.Dev
	}
	if s.Build.Commit != "" {
		c := s.Build.Commit
		info.Commit = &c
	}
	if s.Hardware != nil {
		rt := s.Hardware.Runtimes(ctx)
		info.Features.Recording = rt.FFmpeg.Reachable
		info.Features.SrtIngest = rt.SRT
	}
	if s.TLS != nil {
		info.Features.Tls = s.TLS.Info().Enabled
	}
	return api.GetSystemInfo200JSONResponse(info), nil
}

// GetHardwareReport is the hardware self-check (AI-12).
func (s *Server) GetHardwareReport(ctx context.Context, _ api.GetHardwareReportRequestObject) (api.GetHardwareReportResponseObject, error) {
	if s.Hardware == nil {
		return nil, api.ErrNotImplemented
	}
	return api.GetHardwareReport200JSONResponse(s.Hardware.Report(ctx)), nil
}

// RunBenchmark runs the bundled clip through the local provider (AI-12).
func (s *Server) RunBenchmark(ctx context.Context, _ api.RunBenchmarkRequestObject) (api.RunBenchmarkResponseObject, error) {
	if s.Hardware == nil {
		return nil, api.ErrNotImplemented
	}
	res, err := s.Hardware.Benchmark(ctx)
	if errors.Is(err, hwcheck.ErrBusy) {
		return api.RunBenchmark409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse{
			Code: hwcheck.CodeBenchmarkRunning, Message: "a benchmark is already running"}}, nil
	}
	var ce *domain.CodedError
	if errors.As(err, &ce) {
		return api.RunBenchmark422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse(codedError(ce))}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.RunBenchmark200JSONResponse(res), nil
}

// codedError converts a *domain.CodedError to the API error.
func codedError(ce *domain.CodedError) api.Error {
	e := api.Error{Code: ce.Code, Message: ce.Message}
	if len(ce.Params) > 0 {
		p := ce.Params
		e.Params = &p
	}
	return e
}

// ListLanguages returns the supported language catalog (AI-5).
func (s *Server) ListLanguages(context.Context, api.ListLanguagesRequestObject) (api.ListLanguagesResponseObject, error) {
	return api.ListLanguages200JSONResponse(domain.Languages()), nil
}
