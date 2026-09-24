// SPDX-License-Identifier: Apache-2.0

// Command livesubs is the Live Subtitles server: a single binary that serves
// the web app, the HTTP API and the caption WebSockets.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/iencodev/live-subtitles/internal/app"
	"github.com/iencodev/live-subtitles/internal/config"
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

	log := cfg.Logger(os.Stderr)
	log.Info("starting livesubs", "version", version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.New(cfg, log, web.Dist()).Run(ctx); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
