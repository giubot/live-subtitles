// SPDX-License-Identifier: Apache-2.0

// Command livesubs is the Live Subtitles server: a single binary that serves
// the web app, the HTTP API and the caption WebSockets.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/iencodev/live-subtitles/internal/app"
	"github.com/iencodev/live-subtitles/internal/config"
	"github.com/iencodev/live-subtitles/internal/secrets"
	"github.com/iencodev/live-subtitles/web"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "livesubs:", err)
		os.Exit(2)
	}
	if cfg.Version {
		fmt.Println(version)
		return
	}

	// Every log line goes through the redactor, which learns secret values
	// as the secrets store and the app read them.
	red := secrets.NewRedactor()
	log := slog.New(secrets.NewRedactingHandler(cfg.Logger(os.Stderr).Handler(), red))
	log.Info("starting livesubs", "version", version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, log, red); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config, log *slog.Logger, red *secrets.Redactor) error {
	a, err := app.New(ctx, cfg, log, web.Dist(), red)
	if err != nil {
		return err
	}
	defer func() { _ = a.Close() }()
	return a.Run(ctx)
}
