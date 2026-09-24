// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Config is the process configuration. Runtime settings that the admin can
// change (languages, providers, recording…) live in the store, not here.
type Config struct {
	Addr          string // HTTP listen address
	DataDir       string // SQLite, recordings, local CA, secrets file
	LogFormat     string // text | json
	LogLevel      slog.Level
	PublicBaseURL string // overrides LAN detection for generated URLs
	Version       bool   // print the version and exit
}

// Load parses args (without the program name) with defaults taken from
// LIVESUBS_* environment variables, so the precedence is flag > env > default.
func Load(args []string, getenv func(string) string) (Config, error) {
	env := func(name, def string) string {
		if v := getenv("LIVESUBS_" + name); v != "" {
			return v
		}
		return def
	}

	var c Config
	var level string
	fs := flag.NewFlagSet("livesubs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.Addr, "addr", env("ADDR", "0.0.0.0:8080"), "HTTP listen address (LIVESUBS_ADDR)")
	fs.StringVar(&c.DataDir, "data-dir", env("DATA_DIR", "./data"), "data directory (LIVESUBS_DATA_DIR)")
	fs.StringVar(&c.LogFormat, "log-format", env("LOG_FORMAT", "text"), "log format: text or json (LIVESUBS_LOG_FORMAT)")
	fs.StringVar(&level, "log-level", env("LOG_LEVEL", "info"), "log level: debug, info, warn or error (LIVESUBS_LOG_LEVEL)")
	fs.StringVar(&c.PublicBaseURL, "public-base-url", env("PUBLIC_BASE_URL", ""), "base URL for generated links, e.g. https://subs.example.com (LIVESUBS_PUBLIC_BASE_URL)")
	fs.BoolVar(&c.Version, "version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stderr)
			fs.Usage()
		}
		return c, err
	}
	if fs.NArg() > 0 {
		return c, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if err := c.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return c, fmt.Errorf("log level: %w", err)
	}
	c.LogFormat = strings.ToLower(c.LogFormat)
	if c.LogFormat != "text" && c.LogFormat != "json" {
		return c, fmt.Errorf("log format %q: want text or json", c.LogFormat)
	}
	return c, nil
}

// Logger builds the process logger.
func (c Config) Logger(w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
