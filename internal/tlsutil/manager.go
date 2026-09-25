// SPDX-License-Identifier: Apache-2.0

package tlsutil

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/netinfo"
)

// Dir is the directory inside the data directory that holds the local CA,
// the leaf certificate and the autocert cache.
const Dir = "tls"

// CheckInterval is how often Run looks for LAN or hostname changes and for
// certificates close to expiry.
const CheckInterval = 5 * time.Minute

// Options configure a Manager.
type Options struct {
	Mode          string // config.TLS.Mode: auto, local-ca, provided, acme or disabled
	DataDir       string
	CertFile      string // provided mode
	KeyFile       string
	PublicBaseURL string // acme domain; its host is also a local-CA SAN
	ACMEEmail     string

	// LANAddrs lists this host's LAN addresses; nil uses netinfo.Interfaces.
	LANAddrs func() ([]netip.Addr, error)
	// Hostname defaults to os.Hostname.
	Hostname func() (string, error)
	// Now defaults to time.Now.
	Now    func() time.Time
	Logger *slog.Logger
}

// Manager owns the HTTPS certificate: it creates and renews the local CA
// leaf, reloads provided files and drives autocert. The listener reads the
// current certificate through TLSConfig().GetCertificate, so a renewal
// needs no restart.
type Manager struct {
	opts   Options
	mode   Mode
	domain string // acme
	dir    string
	log    *slog.Logger

	mu       sync.Mutex // serializes Refresh
	ca       *keyPair   // local-ca
	fileStat [2]fileStamp
	acm      *autocert.Manager

	cert      atomic.Pointer[tls.Certificate] // served (local-ca, provided) or last issued (acme)
	httpsPort atomic.Int32
}

type fileStamp struct {
	mod  time.Time
	size int64
}

// New resolves the mode and loads or creates the certificate, so a bad
// --tls-cert or an unwritable data directory fails at startup.
func New(o Options) (*Manager, error) {
	mode, domain, err := SelectMode(o.Mode, o.CertFile, o.PublicBaseURL)
	if err != nil {
		return nil, err
	}
	if o.LANAddrs == nil {
		o.LANAddrs = lanAddrs
	}
	if o.Hostname == nil {
		o.Hostname = os.Hostname
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	m := &Manager{opts: o, mode: mode, domain: domain, dir: filepath.Join(o.DataDir, Dir), log: o.Logger}

	switch mode {
	case ModeDisabled:
		return m, nil
	case ModeACME:
		cache := filepath.Join(m.dir, "acme")
		if err := os.MkdirAll(cache, stateDirMode); err != nil {
			return nil, fmt.Errorf("tls: %w", err)
		}
		m.acm = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(cache),
			HostPolicy: autocert.HostWhitelist(domain),
			Email:      o.ACMEEmail,
		}
		return m, nil
	case ModeProvided:
		if err := m.loadProvided(true); err != nil {
			return nil, err
		}
		return m, nil
	}
	if err := os.MkdirAll(m.dir, stateDirMode); err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	if err := m.Refresh(); err != nil {
		return nil, err
	}
	return m, nil
}

func lanAddrs() ([]netip.Addr, error) {
	ifs, err := netinfo.Interfaces()
	addrs := make([]netip.Addr, 0, len(ifs))
	for _, i := range ifs {
		addrs = append(addrs, i.IP)
	}
	return addrs, err
}

// Mode is the resolved mode.
func (m *Manager) Mode() Mode { return m.mode }

// Enabled reports whether an HTTPS listener should run.
func (m *Manager) Enabled() bool { return m.mode != ModeDisabled }

// SetHTTPSPort records the port the HTTPS listener bound, for Info and the
// network answer; 0 means not listening.
func (m *Manager) SetHTTPSPort(port int) { m.httpsPort.Store(int32(port)) }

// HTTPSPort is the port set by SetHTTPSPort.
func (m *Manager) HTTPSPort() int { return int(m.httpsPort.Load()) }

// TLSConfig is the listener configuration; nil when TLS is disabled.
func (m *Manager) TLSConfig() *tls.Config {
	switch m.mode {
	case ModeDisabled:
		return nil
	case ModeACME:
		c := m.acm.TLSConfig() // adds acme-tls/1 for the TLS-ALPN-01 challenge
		get := c.GetCertificate
		c.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, err := get(hello)
			if err == nil && cert != nil && cert.Leaf != nil {
				m.cert.Store(cert)
			}
			return cert, err
		}
		c.MinVersion = tls.VersionTLS12
		return c
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if c := m.cert.Load(); c != nil {
				return c, nil
			}
			return nil, errors.New("tls: no certificate loaded")
		},
	}
}

// HTTPHandler wraps the plain-HTTP handler: in acme mode it answers the
// HTTP-01 challenge and passes everything else to next (the HTTP listener
// keeps serving the app, TLS-3); otherwise it returns next.
func (m *Manager) HTTPHandler(next http.Handler) http.Handler {
	if m.acm == nil {
		return next
	}
	return m.acm.HTTPHandler(next)
}

// Info is the GET /api/tls answer.
func (m *Manager) Info() api.TlsInfo {
	info := api.TlsInfo{Enabled: m.Enabled(), Mode: m.mode}
	if !info.Enabled {
		return info
	}
	if p := m.HTTPSPort(); p > 0 {
		info.HttpsPort = &p
	}
	var sans []string
	if c := m.cert.Load(); c != nil && c.Leaf != nil {
		sans = certSANs(c.Leaf).Strings()
		notAfter := c.Leaf.NotAfter.UTC()
		info.NotAfter = &notAfter
	} else if m.domain != "" {
		sans = []string{m.domain}
	}
	if sans != nil {
		info.Sans = &sans
	}
	if ca := m.caPair(); ca != nil {
		fp := Fingerprint(ca.cert.Raw)
		info.CaFingerprintSha256 = &fp
	}
	return info
}

// CACertPEM is the local CA certificate for devices to install; nil unless
// the mode is local-ca.
func (m *Manager) CACertPEM() []byte {
	if ca := m.caPair(); ca != nil {
		return slices.Clone(ca.certPEM)
	}
	return nil
}

func (m *Manager) caPair() *keyPair {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ca
}

// Run calls Refresh every CheckInterval until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	if m.mode != ModeLocalCA && m.mode != ModeProvided {
		return // autocert renews on its own
	}
	t := time.NewTicker(CheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Refresh(); err != nil {
				m.log.Warn("tls refresh failed, keeping the current certificate", "err", err)
			}
		}
	}
}

// Refresh brings the certificate up to date. local-ca: creates the CA if
// missing or near expiry, and reissues the leaf when its SANs no longer
// match the LAN addresses and hostname, when it's near expiry, or when the
// CA changed. provided: reloads the files when they change on disk. On
// error the previous certificate stays in service.
func (m *Manager) Refresh() error {
	switch m.mode {
	case ModeLocalCA:
		return m.refreshLocalCA()
	case ModeProvided:
		return m.loadProvided(false)
	}
	return nil
}

func (m *Manager) refreshLocalCA() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.opts.Now()
	hostname, err := m.opts.Hostname()
	if err != nil {
		m.log.Warn("read hostname for the TLS certificate", "err", err)
	}
	lan, err := m.opts.LANAddrs()
	if err != nil {
		m.log.Warn("list LAN addresses for the TLS certificate", "err", err)
	}
	want := DesiredSANs(hostname, lan, publicHost(m.opts.PublicBaseURL))

	caChanged := false
	if m.ca == nil {
		ca, err := loadKeyPair(m.dir, caFile, caKeyFile)
		switch {
		case err == nil:
			m.ca = ca
		case errors.Is(err, fs.ErrNotExist):
		default:
			m.log.Warn("local CA unreadable, creating a new one; devices must install it again", "dir", m.dir, "err", err)
		}
	}
	if m.ca == nil || now.Add(RenewBefore).After(m.ca.cert.NotAfter) {
		ca, err := newCA(hostname, now)
		if err != nil {
			return fmt.Errorf("tls: create local CA: %w", err)
		}
		if err := ca.save(m.dir, caFile, caKeyFile); err != nil {
			return fmt.Errorf("tls: save local CA: %w", err)
		}
		if m.ca != nil {
			m.log.Warn("local CA renewed; devices must install it again", "notAfter", ca.cert.NotAfter)
		}
		m.ca, caChanged = ca, true
		m.log.Info("created local CA", "path", filepath.Join(m.dir, caFile), "fingerprint", Fingerprint(ca.cert.Raw), "notAfter", ca.cert.NotAfter)
	}

	leaf := m.currentLeaf()
	if leaf == nil && !caChanged {
		if l, err := loadKeyPair(m.dir, leafFile, leafKeyFile); err == nil {
			leaf = l
		} else if !errors.Is(err, fs.ErrNotExist) {
			m.log.Warn("server certificate unreadable, issuing a new one", "err", err)
		}
	}
	reason := leafStale(leaf, m.ca, want, now)
	if reason == "" {
		if m.cert.Load() == nil {
			return m.serve(leaf)
		}
		return nil
	}
	if caChanged {
		reason = "new CA"
	}
	l, err := newLeaf(m.ca, want, now)
	if err != nil {
		return fmt.Errorf("tls: issue server certificate: %w", err)
	}
	if err := l.save(m.dir, leafFile, leafKeyFile); err != nil {
		return fmt.Errorf("tls: save server certificate: %w", err)
	}
	m.log.Info("issued TLS certificate", "reason", reason, "sans", want.Strings(), "notAfter", l.cert.NotAfter)
	return m.serve(l)
}

func (m *Manager) currentLeaf() *keyPair {
	c := m.cert.Load()
	if c == nil || c.Leaf == nil {
		return nil
	}
	return &keyPair{cert: c.Leaf}
}

func (m *Manager) serve(k *keyPair) error {
	if k.certPEM == nil {
		return nil // already serving this one
	}
	c, err := k.tlsCertificate()
	if err != nil {
		return fmt.Errorf("tls: %w", err)
	}
	m.cert.Store(c)
	return nil
}

// leafStale says why the leaf must be reissued, or "" when it's fine.
func leafStale(leaf, ca *keyPair, want SANs, now time.Time) string {
	switch {
	case leaf == nil:
		return "missing"
	case leaf.cert.CheckSignatureFrom(ca.cert) != nil:
		return "not signed by the local CA"
	case now.Add(RenewBefore).After(leaf.cert.NotAfter):
		return "expires soon"
	case now.Before(leaf.cert.NotBefore):
		return "not valid yet"
	case !slices.Equal(certSANs(leaf.cert).Strings(), want.Strings()):
		return "LAN addresses or hostname changed"
	}
	return ""
}

// publicHost is the host of --public-base-url, "" when unset.
func publicHost(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// loadProvided (re)loads --tls-cert/--tls-key when they changed on disk.
// At startup (first) any error is fatal; later the old pair stays in use.
func (m *Manager) loadProvided(first bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var stamps [2]fileStamp
	for i, p := range []string{m.opts.CertFile, m.opts.KeyFile} {
		st, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("tls: %w", err)
		}
		stamps[i] = fileStamp{st.ModTime(), st.Size()}
	}
	if !first && stamps == m.fileStat {
		return nil
	}
	c, err := tls.LoadX509KeyPair(m.opts.CertFile, m.opts.KeyFile)
	if err != nil {
		return fmt.Errorf("tls: load --tls-cert/--tls-key: %w", err)
	}
	if c.Leaf == nil {
		return errors.New("tls: --tls-cert has no certificate")
	}
	if now := m.opts.Now(); now.After(c.Leaf.NotAfter) {
		m.log.Warn("the provided TLS certificate has expired", "notAfter", c.Leaf.NotAfter)
	}
	m.cert.Store(&c)
	m.fileStat = stamps
	if !first {
		m.log.Info("reloaded TLS certificate", "path", m.opts.CertFile, "notAfter", c.Leaf.NotAfter)
	}
	return nil
}
