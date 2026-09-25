// SPDX-License-Identifier: Apache-2.0

// Package srt runs SRT ingest (AUD-5, SRT-1): it gives each session that
// uses SRT its own UDP listener port, reads the latency and passphrase from
// the settings and the secrets store, and opens an ffmpeg SRT listener
// source for the session.
//
// # One port per session
//
// ffmpeg's listener accepts a single caller and can't route callers by
// streamid, and two ffmpeg processes can't share a UDP port. So each
// session gets its own port, counting up from settings.srt.port (default
// 9000): the first session that asks gets 9000, the next 9001, and so on,
// up to MaxPorts. A session keeps its port for the life of the process, so
// the URL in SessionUrls.srtIngest doesn't change between runs. The URL
// still carries `streamid=<session>`, which the listener ignores, so
// encoders are already configured if routing on one port comes later.
package srt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

// Defaults of settings.srt.
const (
	DefaultPort    = 9000
	DefaultLatency = 200 * time.Millisecond
)

// MaxPorts bounds how many ports from settings.srt.port up are handed out.
const MaxPorts = 100

// Error codes of Open (UI-4).
const (
	// CodeUnavailable: ffmpeg wasn't built with libsrt (or can't run).
	CodeUnavailable = "source.srt_unavailable"
	// CodeDisabled: settings.srt.enabled is false.
	CodeDisabled = "source.srt_disabled"
	// CodePortBusy: the session's UDP port is taken, or no port is left.
	CodePortBusy = "source.srt_port_busy"
	// CodePassphraseInvalid: the srt_passphrase secret isn't 10–79 characters.
	CodePassphraseInvalid = "source.srt_passphrase_invalid"
)

// Options configure a Service.
type Options struct {
	// Binary is the ffmpeg executable (default ffmpeg.DefaultBinary).
	Binary string
	// Settings holds settings.srt; nil or unset uses the defaults.
	Settings domain.SettingsStore
	// Secrets holds the optional srt_passphrase; nil means none.
	Secrets domain.SecretStore
	// Redact, if set, learns the passphrase so logs mask it
	// (secrets.Redactor.Add).
	Redact func(string)
	Logger *slog.Logger

	// supports replaces the libsrt probe (tests).
	supports func(ctx context.Context) (bool, error)
}

// Service hands out SRT sources and ports.
type Service struct {
	opts Options
	log  *slog.Logger

	mu      sync.Mutex
	offsets map[string]int // session → port offset from settings.srt.port
	probed  time.Time
	libsrt  bool
}

// New returns a Service.
func New(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.supports == nil {
		opts.supports = func(ctx context.Context) (bool, error) { return ffmpeg.SupportsSRT(ctx, opts.Binary) }
	}
	return &Service{opts: opts, log: opts.Logger, offsets: map[string]int{}}
}

// reprobe is how long a failed libsrt probe is trusted, so installing an
// ffmpeg with libsrt works without a restart.
const reprobe = 30 * time.Second

// Available reports whether ffmpeg can listen for SRT. A positive probe
// is kept; a negative one is retried after reprobe.
func (s *Service) Available(ctx context.Context) bool {
	s.mu.Lock()
	if s.libsrt || (!s.probed.IsZero() && time.Since(s.probed) < reprobe) {
		defer s.mu.Unlock()
		return s.libsrt
	}
	s.mu.Unlock()
	ok, err := s.opts.supports(ctx)
	if err != nil {
		s.log.Warn("srt: probe ffmpeg", "err", err)
	} else if !ok {
		s.log.Warn("srt: ffmpeg was built without libsrt; SRT ingest is off")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probed, s.libsrt = time.Now(), ok
	return ok
}

// config is settings.srt with defaults.
type config struct {
	enabled bool
	port    int
	latency time.Duration
}

func (s *Service) config(ctx context.Context) config {
	c := config{enabled: true, port: DefaultPort, latency: DefaultLatency}
	if s.opts.Settings == nil {
		return c
	}
	st, err := s.opts.Settings.Settings(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			s.log.Warn("srt: read settings; using defaults", "err", err)
		}
		return c
	}
	if st.Srt == nil {
		return c
	}
	if st.Srt.Enabled != nil {
		c.enabled = *st.Srt.Enabled
	}
	if p := st.Srt.Port; p != nil && *p > 0 && *p <= 65535 {
		c.port = *p
	}
	if l := st.Srt.LatencyMs; l != nil && *l > 0 {
		c.latency = time.Duration(*l) * time.Millisecond
	}
	return c
}

// offset returns the session's port offset, assigning the lowest free
// one; -1 when all MaxPorts are taken.
func (s *Service) offset(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.offsets[sessionID]; ok {
		return o
	}
	used := make(map[int]bool, len(s.offsets))
	for _, o := range s.offsets {
		used[o] = true
	}
	for o := range MaxPorts {
		if !used[o] {
			s.offsets[sessionID] = o
			return o
		}
	}
	return -1
}

// port is the session's UDP port under c, or 0 when none is left.
func (s *Service) port(c config, sessionID string) int {
	o := s.offset(sessionID)
	if o < 0 || c.port+o > 65535 {
		return 0
	}
	return c.port + o
}

// Release forgets a deleted session's port.
func (s *Service) Release(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.offsets, sessionID)
}

// IngestURL is where an encoder pushes the session's audio,
// srt://<host>:<port>?streamid=<session>; false when SRT is off or ffmpeg
// lacks libsrt.
func (s *Service) IngestURL(ctx context.Context, host, sessionID string) (string, bool) {
	c := s.config(ctx)
	if !c.enabled || host == "" || !s.Available(ctx) {
		return "", false
	}
	port := s.port(c, sessionID)
	if port == 0 {
		return "", false
	}
	return "srt://" + net.JoinHostPort(host, strconv.Itoa(port)) + "?streamid=" + url.QueryEscape(sessionID), true
}

// Open returns the session's SRT listener source. Errors are
// *domain.CodedError with one of the Code… constants.
func (s *Service) Open(ctx context.Context, sessionID string) (*ffmpeg.SRTSource, error) {
	c := s.config(ctx)
	if !c.enabled {
		return nil, &domain.CodedError{Code: CodeDisabled, Message: "SRT ingest is disabled in the settings"}
	}
	if !s.Available(ctx) {
		return nil, &domain.CodedError{Code: CodeUnavailable, Message: "ffmpeg was built without libsrt, so it can't receive SRT"}
	}
	port := s.port(c, sessionID)
	if port == 0 {
		return nil, &domain.CodedError{Code: CodePortBusy, Message: fmt.Sprintf("no SRT port left from %d", c.port),
			Params: map[string]any{"port": c.port}}
	}
	if err := portFree(port); err != nil {
		return nil, &domain.CodedError{Code: CodePortBusy, Message: fmt.Sprintf("UDP port %d is in use", port),
			Params: map[string]any{"port": port}}
	}
	pass, err := s.passphrase(ctx)
	if err != nil {
		return nil, &domain.CodedError{Code: CodeUnavailable, Message: "read the SRT passphrase: " + err.Error()}
	}
	src, err := ffmpeg.NewSRTSource(ffmpeg.SRTOptions{
		Binary:     s.opts.Binary,
		Port:       port,
		Latency:    c.latency,
		Passphrase: pass,
		Logger:     s.log.With("session", sessionID),
	})
	if errors.Is(err, ffmpeg.ErrSRTPassphrase) {
		return nil, &domain.CodedError{Code: CodePassphraseInvalid, Message: err.Error(),
			Params: map[string]any{"min": ffmpeg.MinSRTPassphrase, "max": ffmpeg.MaxSRTPassphrase}}
	}
	if err != nil {
		return nil, err
	}
	return src, nil
}

// passphrase reads the optional srt_passphrase secret.
func (s *Service) passphrase(ctx context.Context) (string, error) {
	if s.opts.Secrets == nil {
		return "", nil
	}
	v, _, err := s.opts.Secrets.GetSecret(ctx, string(api.SrtPassphrase))
	if errors.Is(err, secrets.ErrNotFound) || errors.Is(err, domain.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if v != "" && s.opts.Redact != nil {
		s.opts.Redact(v)
	}
	return v, nil
}

// portFree checks that nothing else listens on the UDP port.
func portFree(port int) error {
	c, err := net.ListenPacket("udp4", ":"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	return c.Close()
}
