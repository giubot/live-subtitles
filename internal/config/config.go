// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/iencodev/live-subtitles/internal/metrics"
)

// MinAdminTokenLength keeps a guessable bearer token out of the config.
const MinAdminTokenLength = 16

// Config is the process configuration. Runtime settings that the admin can
// change (languages, providers, recording…) live in the store, not here.
type Config struct {
	Addr          string // HTTP listen address
	DataDir       string // SQLite, recordings, local CA, secrets file
	LogFormat     string // text | json
	LogLevel      slog.Level
	PublicBaseURL string // overrides LAN detection for generated URLs
	NoKeychain    bool   // never use the OS keychain for secrets
	FFmpeg        string // ffmpeg executable for file, URL and SRT sources
	Version       bool   // print the version and exit

	// AdminToken is accepted as a bearer token on admin endpoints. It is
	// only read from LIVESUBS_ADMIN_TOKEN: a flag would show in `ps`.
	AdminToken string

	// GeminiASRPrices and GeminiTranslationPrices estimate the cost of
	// Gemini usage in session status (AI-9), for speech recognition and for
	// translation. The defaults are list-price estimates; see
	// metrics.DefaultGeminiASRPrices and metrics.DefaultGeminiTranslationPrices.
	GeminiASRPrices         metrics.Prices
	GeminiTranslationPrices metrics.Prices
	// TLS configures the HTTPS listener next to the HTTP one (tls.go).
	TLS TLS
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
	noKeychain, err := strconv.ParseBool(env("NO_KEYCHAIN", "false"))
	if err != nil {
		return c, fmt.Errorf("LIVESUBS_NO_KEYCHAIN: %w", err)
	}
	fs.BoolVar(&c.NoKeychain, "no-keychain", noKeychain, "keep secrets in the encrypted file only, never the OS keychain (LIVESUBS_NO_KEYCHAIN)")
	fs.StringVar(&c.FFmpeg, "ffmpeg", env("FFMPEG", "ffmpeg"), "ffmpeg executable (LIVESUBS_FFMPEG)")
	fs.BoolVar(&c.Version, "version", false, "print the version and exit")
	c.AdminToken = getenv("LIVESUBS_ADMIN_TOKEN")
	for _, p := range []struct {
		flag, env string
		dst       *float64
		def       float64
		what      string
	}{
		{"gemini-asr-audio-usd-per-min", "GEMINI_ASR_AUDIO_USD_PER_MIN", &c.GeminiASRPrices.AudioPerMin,
			metrics.DefaultGeminiASRPrices.AudioPerMin, "estimated Gemini speech recognition price per minute of audio, USD"},
		{"gemini-asr-output-usd-per-mtok", "GEMINI_ASR_OUTPUT_USD_PER_MTOK", &c.GeminiASRPrices.OutputPerMTok,
			metrics.DefaultGeminiASRPrices.OutputPerMTok, "estimated Gemini speech recognition price per million transcript tokens, USD"},
		{"gemini-translation-input-usd-per-mtok", "GEMINI_TRANSLATION_INPUT_USD_PER_MTOK", &c.GeminiTranslationPrices.InputPerMTok,
			metrics.DefaultGeminiTranslationPrices.InputPerMTok, "estimated Gemini translation price per million input tokens, USD"},
		{"gemini-translation-output-usd-per-mtok", "GEMINI_TRANSLATION_OUTPUT_USD_PER_MTOK", &c.GeminiTranslationPrices.OutputPerMTok,
			metrics.DefaultGeminiTranslationPrices.OutputPerMTok, "estimated Gemini translation price per million output tokens, USD"},
	} {
		def := p.def
		if v := env(p.env, ""); v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return c, fmt.Errorf("LIVESUBS_%s: %w", p.env, err)
			}
			def = f
		}
		fs.Float64Var(p.dst, p.flag, def, fmt.Sprintf("%s (LIVESUBS_%s)", p.what, p.env))
	}
	tlsFlags(fs, &c.TLS, env)

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
	if err := errors.Join(c.GeminiASRPrices.Validate(), c.GeminiTranslationPrices.Validate()); err != nil {
		return c, fmt.Errorf("gemini prices: %w", err)
	}
	if c.AdminToken != "" && len(c.AdminToken) < MinAdminTokenLength {
		return c, fmt.Errorf("LIVESUBS_ADMIN_TOKEN must be at least %d characters", MinAdminTokenLength)
	}
	if err := c.TLS.finish(); err != nil {
		return c, err
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
