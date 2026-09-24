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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/api/handlers"
	"github.com/iencodev/live-subtitles/internal/config"
	"github.com/iencodev/live-subtitles/internal/netinfo"
)

// ShutdownTimeout bounds how long in-flight requests get after a stop signal.
const ShutdownTimeout = 10 * time.Second

// App is the wired server.
type App struct {
	cfg     config.Config
	log     *slog.Logger
	handler http.Handler
	out     io.Writer    // startup banner (URLs + QR)
	port    atomic.Int32 // HTTP port, known for sure once listening
}

// New wires services and routes. dist is the built web app.
func New(cfg config.Config, log *slog.Logger, dist fs.FS) *App {
	a := &App{cfg: cfg, log: log, out: os.Stderr}
	if _, port, err := net.SplitHostPort(cfg.Addr); err == nil {
		if n, err := strconv.Atoi(port); err == nil {
			a.port.Store(int32(n))
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/", spa(dist))
	srv := handlers.New()
	srv.Network = a.network
	a.handler = srv.Handler(mux, log)
	return a
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
