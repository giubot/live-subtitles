// SPDX-License-Identifier: Apache-2.0

package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

// host is a fake machine whose LAN addresses, hostname and clock the tests change.
type host struct {
	name string
	lan  []netip.Addr
	now  time.Time
}

func (h *host) options(dir string) Options {
	return Options{
		Mode:     "auto",
		DataDir:  dir,
		LANAddrs: func() ([]netip.Addr, error) { return slices.Clone(h.lan), nil },
		Hostname: func() (string, error) { return h.name, nil },
		Now:      func() time.Time { return h.now },
	}
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, len(s))
	for i, a := range s {
		out[i] = netip.MustParseAddr(a)
	}
	return out
}

func served(t *testing.T, m *Manager) *x509.Certificate {
	t.Helper()
	c, err := m.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	return c.Leaf
}

func TestDesiredSANs(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		lan      []netip.Addr
		extra    []string
		want     []string
	}{
		{"plain hostname", "mini-pc", addrs("192.168.1.20", "2800:810::20"), nil,
			[]string{"localhost", "mini-pc", "mini-pc.local", "127.0.0.1", "192.168.1.20", "2800:810::20", "::1"}},
		{"macOS .local hostname", "Mini-PC.local.", addrs("10.0.0.5"), nil,
			[]string{"localhost", "mini-pc", "mini-pc.local", "10.0.0.5", "127.0.0.1", "::1"}},
		{"FQDN", "stage.example.org", nil, nil,
			[]string{"localhost", "stage", "stage.example.org", "stage.local", "127.0.0.1", "::1"}},
		{"invalid hostname is skipped", "my host", nil, nil, []string{"localhost", "127.0.0.1", "::1"}},
		{"extra host and IP, duplicates dropped", "mini-pc", addrs("192.168.1.20", "192.168.1.20"), []string{"subs.lan", "192.168.1.20", ""},
			[]string{"localhost", "mini-pc", "mini-pc.local", "subs.lan", "127.0.0.1", "192.168.1.20", "::1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DesiredSANs(tt.hostname, tt.lan, tt.extra...).Strings(); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocalCAGeneration(t *testing.T) {
	dir := t.TempDir()
	h := &host{name: "mini-pc", lan: addrs("192.168.1.20"), now: t0}
	m, err := New(h.options(dir))
	if err != nil {
		t.Fatal(err)
	}
	if m.Mode() != ModeLocalCA || !m.Enabled() {
		t.Fatalf("mode %q enabled %v", m.Mode(), m.Enabled())
	}

	caPEM := m.CACertPEM()
	block, _ := pem.Decode(caPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("CACertPEM = %q", caPEM)
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !ca.IsCA || !ca.BasicConstraintsValid || ca.MaxPathLen != 0 || !ca.MaxPathLenZero {
		t.Error("CA basic constraints")
	}
	if pub, ok := ca.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
		t.Errorf("CA key %T, want ECDSA P-256", ca.PublicKey)
	}
	if got := ca.NotAfter.Sub(ca.NotBefore); got != CAValidity {
		t.Errorf("CA validity %v, want %v", got, CAValidity)
	}

	leaf := served(t, m)
	if got := leaf.NotAfter.Sub(leaf.NotBefore); got > 397*24*time.Hour {
		t.Errorf("leaf validity %v exceeds 397 days", got)
	}
	if leaf.IsCA || !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		t.Error("leaf must be a server certificate")
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	for _, name := range []string{"localhost", "127.0.0.1", "::1", "192.168.1.20", "mini-pc", "mini-pc.local"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: name, CurrentTime: t0}); err != nil {
			t.Errorf("verify %s: %v", name, err)
		}
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "192.168.1.99", CurrentTime: t0}); err == nil {
		t.Error("leaf verified for an address it doesn't cover")
	}

	info := m.Info()
	if !info.Enabled || info.Mode != ModeLocalCA || info.NotAfter == nil || !info.NotAfter.Equal(leaf.NotAfter) {
		t.Errorf("info %+v", info)
	}
	if info.CaFingerprintSha256 == nil || *info.CaFingerprintSha256 != Fingerprint(ca.Raw) || len(*info.CaFingerprintSha256) != 95 {
		t.Errorf("fingerprint %v", info.CaFingerprintSha256)
	}
	if info.Sans == nil || !slices.Contains(*info.Sans, "192.168.1.20") || !slices.Contains(*info.Sans, "mini-pc.local") {
		t.Errorf("sans %v", info.Sans)
	}
	if info.HttpsPort != nil {
		t.Errorf("httpsPort %d before listening", *info.HttpsPort)
	}
	m.SetHTTPSPort(8443)
	if p := m.Info().HttpsPort; p == nil || *p != 8443 {
		t.Errorf("httpsPort %v", p)
	}

	for name, mode := range map[string]os.FileMode{caFile: 0o644, caKeyFile: 0o600, leafFile: 0o644, leafKeyFile: 0o600} {
		st, err := os.Stat(filepath.Join(dir, Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && st.Mode().Perm() != mode {
			t.Errorf("%s mode %v, want %v", name, st.Mode().Perm(), mode)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, Dir))
	if len(entries) != 4 {
		t.Errorf("tls dir has %d entries, want 4 (no temp files left)", len(entries))
	}
}

func TestLocalCARefresh(t *testing.T) {
	const day = 24 * time.Hour
	tests := []struct {
		name      string
		change    func(h *host, dir string)
		wantLeaf  bool // a new leaf was issued
		wantCA    bool // a new CA was created
		wantInSAN string
		// diskOnly damages files: only a restart notices, the running
		// manager keeps serving what it has in memory.
		diskOnly bool
	}{
		{"nothing changed", func(*host, string) {}, false, false, "192.168.1.20", false},
		{"a day later", func(h *host, _ string) { h.now = h.now.Add(day) }, false, false, "192.168.1.20", false},
		{"new LAN IP", func(h *host, _ string) { h.lan = addrs("192.168.1.20", "10.0.0.7") }, true, false, "10.0.0.7", false},
		{"LAN IP moved", func(h *host, _ string) { h.lan = addrs("192.168.1.33") }, true, false, "192.168.1.33", false},
		{"hostname changed", func(h *host, _ string) { h.name = "stage-pc" }, true, false, "stage-pc.local", false},
		{"leaf near expiry", func(h *host, _ string) { h.now = h.now.Add(LeafValidity - RenewBefore) }, true, false, "192.168.1.20", false},
		{"CA near expiry", func(h *host, _ string) { h.now = h.now.Add(CAValidity - RenewBefore) }, true, true, "192.168.1.20", false},
		{"leaf file corrupted", func(_ *host, dir string) {
			_ = os.WriteFile(filepath.Join(dir, Dir, leafFile), []byte("junk"), 0o644)
		}, true, false, "192.168.1.20", true},
		{"CA key lost", func(_ *host, dir string) { _ = os.Remove(filepath.Join(dir, Dir, caKeyFile)) }, true, true, "192.168.1.20", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			h := &host{name: "mini-pc", lan: addrs("192.168.1.20"), now: t0}
			first, err := New(h.options(dir))
			if err != nil {
				t.Fatal(err)
			}
			oldLeaf, oldCA := served(t, first).SerialNumber, first.Info().CaFingerprintSha256

			tt.change(h, dir)
			// Both paths: the running manager's periodic Refresh, and a
			// restart that reloads the persisted files.
			if err := first.Refresh(); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(h.options(dir))
			if err != nil {
				t.Fatal(err)
			}
			managers := map[string]*Manager{"refresh": first, "restart": restarted}
			if tt.diskOnly {
				if served(t, first).SerialNumber.Cmp(oldLeaf) != 0 {
					t.Error("refresh reacted to damaged files while serving")
				}
				delete(managers, "refresh")
				first = restarted
			}
			for name, m := range managers {
				leaf := served(t, m)
				if got := leaf.SerialNumber.Cmp(oldLeaf) != 0; got != tt.wantLeaf {
					t.Errorf("%s: new leaf %v, want %v", name, got, tt.wantLeaf)
				}
				if got := *m.Info().CaFingerprintSha256 != *oldCA; got != tt.wantCA {
					t.Errorf("%s: new CA %v, want %v", name, got, tt.wantCA)
				}
				if !slices.Contains(*m.Info().Sans, tt.wantInSAN) {
					t.Errorf("%s: SANs %v lack %s", name, *m.Info().Sans, tt.wantInSAN)
				}
				pool := x509.NewCertPool()
				pool.AppendCertsFromPEM(m.CACertPEM())
				if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost", CurrentTime: h.now}); err != nil {
					t.Errorf("%s: leaf doesn't verify against the CA: %v", name, err)
				}
			}
			// The restarted manager adopted what the running one wrote.
			if served(t, first).SerialNumber.Cmp(served(t, restarted).SerialNumber) != 0 {
				t.Error("restart issued another leaf instead of reloading the persisted one")
			}
		})
	}
}

// writePair issues a certificate for names from a throwaway CA and writes it as PEM files.
func writePair(t *testing.T, dir, prefix string, names ...string) (certPath, keyPath string, cert *x509.Certificate) {
	t.Helper()
	ca, err := newCA("", t0)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := newLeaf(ca, SANs{DNS: names}, t0)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath = filepath.Join(dir, prefix+".pem"), filepath.Join(dir, prefix+".key")
	if err := os.WriteFile(certPath, append(leaf.certPEM, ca.certPEM...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, leaf.keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, leaf.cert
}

func TestProvided(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, want := writePair(t, dir, "subs", "subs.example.com")
	h := &host{now: t0}
	o := h.options(dir)
	o.CertFile, o.KeyFile = certPath, keyPath
	m, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	info := m.Info()
	if info.Mode != ModeProvided || info.CaFingerprintSha256 != nil || m.CACertPEM() != nil {
		t.Errorf("info %+v", info)
	}
	if info.Sans == nil || !slices.Equal(*info.Sans, []string{"subs.example.com"}) || !info.NotAfter.Equal(want.NotAfter) {
		t.Errorf("sans %v notAfter %v", info.Sans, info.NotAfter)
	}
	if _, err := os.Stat(filepath.Join(dir, Dir)); !os.IsNotExist(err) {
		t.Error("provided mode created a local CA directory")
	}

	// Renewed files on disk are picked up by Refresh, without a restart.
	_, _, renewed := writePair(t, dir, "subs", "subs.example.com", "www.example.com")
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(certPath, later, later)
	if err := m.Refresh(); err != nil {
		t.Fatal(err)
	}
	if got := served(t, m); got.SerialNumber.Cmp(renewed.SerialNumber) != 0 {
		t.Error("Refresh did not reload the renewed certificate")
	}

	// A broken file keeps the last good certificate in service.
	_ = os.WriteFile(keyPath, []byte("junk"), 0o600)
	_ = os.Chtimes(keyPath, later.Add(time.Minute), later.Add(time.Minute))
	if err := m.Refresh(); err == nil {
		t.Error("Refresh accepted a broken key")
	}
	if got := served(t, m); got.SerialNumber.Cmp(renewed.SerialNumber) != 0 {
		t.Error("a broken reload replaced the served certificate")
	}

	o.KeyFile = filepath.Join(dir, "missing.key")
	if _, err := New(o); err == nil {
		t.Error("New accepted a missing key file")
	}
}

func TestDisabled(t *testing.T) {
	h := &host{now: t0}
	o := h.options(t.TempDir())
	o.Mode = "disabled"
	m, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	if m.Enabled() || m.TLSConfig() != nil || m.CACertPEM() != nil {
		t.Error("disabled mode serves TLS")
	}
	if info := m.Info(); info.Enabled || info.Mode != ModeDisabled || info.Sans != nil {
		t.Errorf("info %+v", info)
	}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if got := m.HTTPHandler(next); got == nil {
		t.Error("HTTPHandler returned nil")
	}
}

func TestACME(t *testing.T) {
	dir := t.TempDir()
	h := &host{now: t0}
	o := h.options(dir)
	o.PublicBaseURL = "https://subs.example.com"
	m, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	if m.Mode() != ModeACME || m.CACertPEM() != nil {
		t.Fatalf("mode %q", m.Mode())
	}
	if info := m.Info(); info.Sans == nil || !slices.Equal(*info.Sans, []string{"subs.example.com"}) {
		t.Errorf("info %+v", info)
	}
	if st, err := os.Stat(filepath.Join(dir, Dir, "acme")); err != nil || !st.IsDir() {
		t.Errorf("acme cache dir: %v", err)
	}
	cfg := m.TLSConfig()
	if !slices.Contains(cfg.NextProtos, "acme-tls/1") {
		t.Errorf("NextProtos %v lacks acme-tls/1", cfg.NextProtos)
	}
	// Other hosts are refused before any ACME traffic.
	if _, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "evil.example.net"}); err == nil {
		t.Error("certificate offered for a host outside the policy")
	}

	h2 := m.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("app")) }))
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, httptest.NewRequest("GET", "http://subs.example.com/s", nil))
	if rec.Body.String() != "app" {
		t.Errorf("plain HTTP no longer serves the app: %d %q", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h2.ServeHTTP(rec, httptest.NewRequest("GET", "http://subs.example.com/.well-known/acme-challenge/token", nil))
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "app") {
		t.Errorf("challenge path: %d %q", rec.Code, rec.Body)
	}
}
