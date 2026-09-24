// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/iencodev/live-subtitles/internal/api/handlers"
	"github.com/iencodev/live-subtitles/internal/config"
)

// ShutdownTimeout bounds how long in-flight requests get after a stop signal.
const ShutdownTimeout = 10 * time.Second

// App is the wired server.
type App struct {
	cfg     config.Config
	log     *slog.Logger
	handler http.Handler
}

// New wires services and routes. dist is the built web app.
func New(cfg config.Config, log *slog.Logger, dist fs.FS) *App {
	mux := http.NewServeMux()
	mux.Handle("/", spa(dist))
	api := handlers.New()
	return &App{cfg: cfg, log: log, handler: api.Handler(mux, log)}
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
