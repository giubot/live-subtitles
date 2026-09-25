// SPDX-License-Identifier: Apache-2.0

// Command loadtest drives a Live Subtitles server with N sessions (mock
// provider, looping file source) and M caption viewers per session, and
// reports caption delivery latency, drops, throughput and the server's CPU
// and memory (P4-05). Run it with `task loadtest`; see docs/scaling.md.
//
// By default it starts the server binary given by -server on a free
// loopback port with a throwaway data directory. With -addr it targets a
// server that is already running instead (LIVESUBS_ADMIN_TOKEN must match
// the server's, and -pid enables CPU/RSS sampling).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/iencodev/live-subtitles/internal/loadtest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		sessions = flag.Int("sessions", 1, "sessions to create")
		viewers  = flag.Int("viewers", 100, "caption viewers per session")
		duration = flag.Duration("duration", 30*time.Second, "how long the sources play once every viewer is connected")
		addr     = flag.String("addr", "", "base URL of a running server (http://host:port); empty starts -server")
		server   = flag.String("server", "bin/loadtest/livesubs", "livesubs binary to start when -addr is empty")
		pid      = flag.Int("pid", 0, "with -addr: the server's process ID, to sample its CPU and RSS")
		fixture  = flag.String("file", "testdata/audio/fixtures/en.wav", "audio file each session plays in a loop")
		langs    = flag.String("langs", "source,es", "tracks each viewer subscribes to")
		dial     = flag.Int("dial-concurrency", 100, "viewers connecting at once")
		jsonOut  = flag.String("json", "", "also write the report as JSON to this file")
		serverLg = flag.Bool("server-log", false, "show the started server's log (warnings and errors)")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := loadtest.Config{
		Sessions: *sessions, Viewers: *viewers, Duration: *duration,
		Langs: strings.Split(*langs, ","), DialConcurrency: *dial, Log: os.Stderr,
	}
	if *addr != "" {
		cfg.BaseURL = strings.TrimRight(*addr, "/")
		cfg.Token = os.Getenv("LIVESUBS_ADMIN_TOKEN")
		cfg.FileURI = *fixture // resolved by the server, against its working directory
		cfg.PID = *pid
		if cfg.Token == "" {
			return errors.New("set LIVESUBS_ADMIN_TOKEN to the server's admin token")
		}
	} else {
		var logw io.Writer = io.Discard
		if *serverLg {
			logw = os.Stderr
		}
		srv, err := loadtest.StartServer(ctx, *server, *fixture, logw)
		if err != nil {
			return err
		}
		defer func() { _ = srv.Close() }()
		fmt.Fprintf(os.Stderr, "started %s at %s (pid %d)\n", *server, srv.URL, srv.PID)
		cfg.BaseURL, cfg.Token, cfg.FileURI, cfg.PID = srv.URL, srv.Token, srv.Fixture, srv.PID
	}

	rep, err := loadtest.Run(ctx, cfg)
	rep.Print(os.Stdout)
	if *jsonOut != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		if werr := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); werr != nil && err == nil {
			err = werr
		}
	}
	return err
}
