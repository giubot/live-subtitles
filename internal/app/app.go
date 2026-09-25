// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	stdlog "log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/api/handlers"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
	"github.com/iencodev/live-subtitles/internal/audio/ingest"
	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/bus"
	"github.com/iencodev/live-subtitles/internal/config"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/metrics"
	"github.com/iencodev/live-subtitles/internal/netinfo"
	"github.com/iencodev/live-subtitles/internal/provider/gemini"
	"github.com/iencodev/live-subtitles/internal/provider/local/gemma"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
	"github.com/iencodev/live-subtitles/internal/secrets"
	"github.com/iencodev/live-subtitles/internal/session"
	"github.com/iencodev/live-subtitles/internal/store"
	"github.com/iencodev/live-subtitles/internal/tlsutil"
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
	tls     *tlsutil.Manager // HTTPS certificate; mode disabled when off
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

	// HTTPS (TLS-1, TLS-5): load or create the certificate first, so a bad
	// --tls-cert fails before anything else is opened.
	tlsm, err := tlsutil.New(tlsutil.Options{
		Mode:          cfg.TLS.Mode,
		DataDir:       cfg.DataDir,
		CertFile:      cfg.TLS.CertFile,
		KeyFile:       cfg.TLS.KeyFile,
		PublicBaseURL: cfg.PublicBaseURL,
		ACMEEmail:     cfg.TLS.ACMEEmail,
		Logger:        log,
	})
	if err != nil {
		return nil, err
	}
	a.tls = tlsm

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
	srv.TLS = a.tls

	// Realtime: browser audio in (/ws/ingest), sessions, captions out
	// (/ws/captions) and admin events (/ws/admin).
	captionBus := bus.New()
	// The Gemini key and model are read at each session start (AI-11).
	geminiKey := googleAPIKey(sec)
	a.hub = ingest.NewHub(srv.Auth.VerifyIngestToken, ingest.Options{Logger: log})
	a.manager = session.New(session.Options{
		Sessions: st,
		Captions: st,
		Settings: st,
		Bus:      captionBus,
		// Until the default-provider rule (P2-07), `default` resolves to mock.
		Providers: map[domain.ProviderKind]session.Provider{
			api.ProviderKindMock: {ASR: &mock.ASR{Latency: mockLatency}, Translator: &mock.Translator{}},
			api.ProviderKindGemini: {
				ASR:        &gemini.ASR{APIKey: geminiKey, Settings: st.Settings, Logger: log},
				Translator: &gemini.Translator{APIKey: geminiKey, Settings: st.Settings},
			},
			api.ProviderKindLocal: {Translator: &gemma.Translator{Settings: st.Settings}},
		},
		IngestSource:  func(id string) domain.AudioSource { return a.hub.Source(id) },
		IngestStatus:  a.hub.Status,
		ReleaseIngest: a.hub.Remove,
		Pricing:       &metrics.Pricing{Gemini: cfg.GeminiPrices},
		Logger:        log,
	})
	srv.Manager = a.manager
	// Test sources may read files from the data directory and ./testdata.
	srv.Files = &ffmpeg.Files{Binary: cfg.FFmpeg, Roots: []string{cfg.DataDir, "testdata"}}
	srv.CaptionsWS = bus.NewCaptionsHandler(captionBus, log, bus.WithSessionLookup(func(ctx context.Context, id string) error {
		_, err := st.GetSession(ctx, id)
		return err
	})).Serve
	srv.IngestWS = a.hub.ServeIngest
	srv.AdminWS = a.manager.AdminHandler(log)

	a.handler = srv.Handler(mux, log)
	return a, nil
}

// googleAPIKey reads the Google API key (Gemini) from the secret store
// at call time, so a key saved in Settings works without a restart.
func googleAPIKey(sec domain.SecretStore) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		v, _, err := sec.GetSecret(ctx, string(api.GoogleApiKey))
		if errors.Is(err, secrets.ErrNotFound) {
			return "", gemini.ErrNoAPIKey
		}
		return v, err
	}
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
		HTTPSPort:     a.tls.HTTPSPort(),
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
	a.httpsBanner(&b, info)
	if f, ok := a.out.(*os.File); ok && isTerminal(f) {
		if qr, err := netinfo.TerminalQR(base + "/s"); err == nil {
			b.WriteString("\n" + qr)
		}
	}
	b.WriteString("\n")
	_, _ = io.WriteString(a.out, b.String())
}

// httpsBanner adds the HTTPS address for remote capture and admin (TLS-2)
// and, with the local CA, where devices download it. The audience URL and
// its QR code stay on plain HTTP (TLS-3).
func (a *App) httpsBanner(b *strings.Builder, info api.NetworkInfo) {
	if info.HttpsPort == nil {
		return
	}
	if !strings.HasPrefix(info.ViewerBaseUrl, "https://") {
		host := "localhost"
		if u, err := url.Parse(info.ViewerBaseUrl); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
		fmt.Fprintf(b, "  HTTPS     https://%s/admin (remote capture and admin)\n", net.JoinHostPort(host, strconv.Itoa(*info.HttpsPort)))
	}
	if a.tls.Mode() == tlsutil.ModeLocalCA {
		fmt.Fprintf(b, "  Local CA  %s/api/tls/ca.crt (install it on devices that use HTTPS)\n", info.ViewerBaseUrl)
	}
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// Handler is the root HTTP handler.
func (a *App) Handler() http.Handler { return a.handler }

// Run serves HTTP and, unless TLS is disabled, HTTPS until ctx is
// cancelled, then shuts both down gracefully. A busy default HTTPS port
// only costs HTTPS, since the audience needs nothing but HTTP (TLS-3); an
// explicit --https-addr that can't be bound is an error.
func (a *App) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", a.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	var tlsLn net.Listener
	if a.tls.Enabled() {
		addr := a.cfg.TLS.HTTPSAddr
		if addr == "" {
			addr = config.DefaultHTTPSAddr(a.cfg.Addr)
		}
		if tlsLn, err = net.Listen("tcp", addr); err != nil {
			if a.cfg.TLS.HTTPSAddr != "" {
				_ = ln.Close()
				return fmt.Errorf("listen https: %w", err)
			}
			a.log.Warn("HTTPS is off: its port is taken; set --https-addr to use another", "addr", addr, "err", err)
			tlsLn = nil
		}
	}
	return a.ServeListeners(ctx, ln, tlsLn)
}

// Serve is Run on an existing HTTP listener, without HTTPS.
func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	return a.ServeListeners(ctx, ln, nil)
}

// ServeListeners serves HTTP on ln and, when tlsLn isn't nil, HTTPS on it
// with the same handler. While running it renews the local-CA leaf when
// the LAN addresses change (TLS-1). If either listener fails, both stop.
func (a *App) ServeListeners(ctx context.Context, ln, tlsLn net.Listener) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	servers := []*http.Server{a.server(ctx, a.tls.HTTPHandler(a.handler), nil)}
	listeners := []net.Listener{ln}
	if tlsLn != nil {
		servers = append(servers, a.server(ctx, a.handler, a.tls.TLSConfig()))
		listeners = append(listeners, tlsLn)
	}

	errc := make(chan error, len(servers))
	for i, srv := range servers {
		go func() {
			if srv.TLSConfig != nil {
				errc <- srv.ServeTLS(listeners[i], "", "") // the certificate comes from GetCertificate
			} else {
				errc <- srv.Serve(listeners[i])
			}
		}()
	}
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		a.port.Store(int32(tcp.Port))
	}
	a.log.Info("listening", "addr", ln.Addr().String())
	if tlsLn != nil {
		if tcp, ok := tlsLn.Addr().(*net.TCPAddr); ok {
			a.tls.SetHTTPSPort(tcp.Port)
		}
		defer a.tls.SetHTTPSPort(0)
		a.log.Info("listening", "addr", tlsLn.Addr().String(), "tls", a.tls.Mode())
	}
	var renew sync.WaitGroup
	renew.Go(func() { a.tls.Run(ctx) })
	defer func() { stop(); renew.Wait() }()
	a.banner()

	var first error
	pending := len(servers)
	select {
	case first = <-errc:
		pending--
	case <-ctx.Done():
	}
	a.log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownTimeout)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(sctx); err != nil && first == nil {
			first = fmt.Errorf("shutdown: %w", err)
		}
	}
	for ; pending > 0; pending-- {
		if err := <-errc; first == nil {
			first = err
		}
	}
	if errors.Is(first, http.ErrServerClosed) {
		return nil
	}
	return first
}

// server builds the HTTP or, with a tlsConfig, the HTTPS server.
func (a *App) server(ctx context.Context, h http.Handler, tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Handler:           h,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          stdlog.New(serverErrorLog{a.log}, "", 0),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
}

// serverErrorLog sends net/http's own errors to the log at warn, except
// TLS handshake failures: every device that hasn't installed the local CA
// causes one per visit, so those go to debug.
type serverErrorLog struct{ log *slog.Logger }

func (l serverErrorLog) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	level := slog.LevelWarn
	if strings.Contains(msg, "TLS handshake error") {
		level = slog.LevelDebug
	}
	l.log.Log(context.Background(), level, msg)
	return len(p), nil
}
