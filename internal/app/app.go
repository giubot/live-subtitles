// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/api/handlers"
	"github.com/iencodev/live-subtitles/internal/audio/ingest"
	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/bus"
	"github.com/iencodev/live-subtitles/internal/config"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/netinfo"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
	"github.com/iencodev/live-subtitles/internal/secrets"
	"github.com/iencodev/live-subtitles/internal/session"
	"github.com/iencodev/live-subtitles/internal/store"
)

// mockLatency makes the mock provider feel like a real one in the UI.
const mockLatency = 300 * time.Millisecond

// DBFile is the SQLite database inside the data directory.
const DBFile = "livesubs.db"

// ShutdownTimeout bounds how long in-flight requests get after a stop signal.
const ShutdownTimeout = 10 * time.Second

// App is the wired server.
type App struct {
	cfg     config.Config
	log     *slog.Logger
	handler http.Handler
	out     io.Writer    // startup banner (URLs + QR)
	port    atomic.Int32 // HTTP port, known for sure once listening
	store   *store.Store
	hub     *ingest.Hub
	manager *session.Manager
}

// New opens the data directory and wires services and routes. dist is the
// built web app; red, which may be nil, learns secret values so the log
// handler can mask them. Call Close when done.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, dist fs.FS, red *secrets.Redactor) (*App, error) {
	a := &App{cfg: cfg, log: log, out: os.Stderr}
	if _, port, err := net.SplitHostPort(cfg.Addr); err == nil {
		if n, err := strconv.Atoi(port); err == nil {
			a.port.Store(int32(n))
		}
	}

	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, DBFile))
	if err != nil {
		return nil, err
	}
	a.store = st
	sec, err := secrets.New(secrets.Options{
		DataDir:         cfg.DataDir,
		DisableKeychain: cfg.NoKeychain,
		Redactor:        red,
		Logger:          log,
	})
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("secrets: %w", err)
	}
	red.Add(cfg.AdminToken)

	mux := http.NewServeMux()
	mux.Handle("/", spa(dist))
	srv := handlers.New()
	srv.Network = a.network
	srv.Secrets = sec
	srv.Sessions, srv.Captions, srv.Settings = st, st, st
	srv.Auth = auth.New(st, auth.Options{AdminToken: cfg.AdminToken})

	// Realtime: browser audio in (/ws/ingest), sessions, captions out
	// (/ws/captions) and admin events (/ws/admin).
	captionBus := bus.New()
	a.hub = ingest.NewHub(srv.Auth.VerifyIngestToken, ingest.Options{Logger: log})
	a.manager = session.New(session.Options{
		Sessions: st,
		Captions: st,
		Bus:      captionBus,
		// Gemini (P2-01) and local (P2-03) register here; until the
		// default-provider rule (P2-07), `default` resolves to mock.
		Providers: map[domain.ProviderKind]session.Provider{
			api.ProviderKindMock: {ASR: &mock.ASR{Latency: mockLatency}, Translator: &mock.Translator{}},
		},
		IngestSource:  func(id string) domain.AudioSource { return a.hub.Source(id) },
		IngestStatus:  a.hub.Status,
		ReleaseIngest: a.hub.Remove,
		Logger:        log,
	})
	srv.Manager = a.manager
	srv.CaptionsWS = bus.NewCaptionsHandler(captionBus, log, bus.WithSessionLookup(func(ctx context.Context, id string) error {
		_, err := st.GetSession(ctx, id)
		return err
	})).Serve
	srv.IngestWS = a.hub.ServeIngest
	srv.AdminWS = a.manager.AdminHandler(log)

	a.handler = srv.Handler(mux, log)
	return a, nil
}

// Close stops running sessions and releases the data directory.
func (a *App) Close() error {
	a.manager.Close()
	a.hub.Close()
	return a.store.Close()
}

// network describes the LAN addresses and the base URL used in QR codes (OUT-4).
func (a *App) network() api.NetworkInfo {
	var ifs []netinfo.Iface
	if !a.loopbackOnly() {
		var err error
		if ifs, err = netinfo.Interfaces(); err != nil {
			a.log.Warn("list network interfaces", "err", err)
		}
	}
	return netinfo.Info(ifs, netinfo.Options{
		HTTPPort:      int(a.port.Load()),
		PublicBaseURL: a.cfg.PublicBaseURL,
	})
}

// loopbackOnly reports a listen address the LAN can't reach (127.0.0.1, ::1, localhost).
func (a *App) loopbackOnly() bool {
	host, _, err := net.SplitHostPort(a.cfg.Addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// banner prints where to open the app, with a QR code for phones when
// stderr is a terminal.
func (a *App) banner() {
	info := a.network()
	base := info.ViewerBaseUrl
	var b strings.Builder
	fmt.Fprintf(&b, "\n  Live Subtitles\n\n  Audience  %s/s\n  Admin     %s/admin\n", base, base)
	for _, i := range info.Interfaces {
		if i.Family == "ipv4" && (info.PreferredIp == nil || i.Ip != *info.PreferredIp) {
			fmt.Fprintf(&b, "  Also on   http://%s:%d (%s)\n", i.Ip, info.HttpPort, i.Name)
		}
	}
	if f, ok := a.out.(*os.File); ok && isTerminal(f) {
		if qr, err := netinfo.TerminalQR(base + "/s"); err == nil {
			b.WriteString("\n" + qr)
		}
	}
	b.WriteString("\n")
	_, _ = io.WriteString(a.out, b.String())
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// Handler is the root HTTP handler.
func (a *App) Handler() http.Handler { return a.handler }

// Run serves until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", a.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return a.Serve(ctx, ln)
}

// Serve is Run on an existing listener.
func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           a.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(a.log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	a.log.Info("listening", "addr", ln.Addr().String())
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		a.port.Store(int32(tcp.Port))
	}
	a.banner()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	a.log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
