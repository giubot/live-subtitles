// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/config"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func get(t *testing.T, c *http.Client, url string) (*http.Response, []byte) {
	t.Helper()
	res, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}

// Dual listeners with the local CA: download the CA over plain HTTP, trust
// it, then complete a real TLS handshake on the HTTPS listener.
func TestDualListenersWithLocalCA(t *testing.T) {
	a := newTestApp(t, config.Config{TLS: config.TLS{Mode: config.TLSModeAuto}}, testDist)
	httpLn, httpsLn := listen(t), listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.ServeListeners(ctx, httpLn, httpsLn) }()
	httpURL := "http://" + httpLn.Addr().String()
	httpsPort := httpsLn.Addr().(*net.TCPAddr).Port

	res, caPEM := get(t, http.DefaultClient, httpURL+"/api/tls/ca.crt")
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/x-x509-ca-cert" {
		t.Fatalf("ca.crt: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatalf("ca.crt is not a PEM certificate: %q", caPEM)
	}

	// Without the CA the handshake fails, as it does on an untrusting phone.
	if _, err := http.Get("https://" + httpsLn.Addr().String() + "/healthz"); err == nil {
		t.Error("HTTPS handshake succeeded without trusting the local CA")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true}}
	for _, host := range []string{"127.0.0.1", "localhost"} {
		res, body := get(t, client, "https://"+net.JoinHostPort(host, strconv.Itoa(httpsPort))+"/healthz")
		if res.StatusCode != 200 || !strings.Contains(string(body), `"database":"ok"`) {
			t.Errorf("https %s: %d %s", host, res.StatusCode, body)
		}
		if res.TLS == nil || res.TLS.Version < tls.VersionTLS12 {
			t.Errorf("https %s: TLS state %+v", host, res.TLS)
		}
		if res.ProtoMajor != 2 {
			t.Errorf("https %s: %s, want HTTP/2", host, res.Proto)
		}
	}

	var info api.TlsInfo
	_, body := get(t, client, "https://127.0.0.1:"+strconv.Itoa(httpsPort)+"/api/tls")
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatal(err)
	}
	if !info.Enabled || info.Mode != api.TlsInfoModeLocalCa || info.HttpsPort == nil || *info.HttpsPort != httpsPort || info.CaFingerprintSha256 == nil {
		t.Errorf("GET /api/tls = %s", body)
	}
	// The QR code and viewer links stay on HTTP (TLS-3).
	var netInfo api.NetworkInfo
	_, body = get(t, http.DefaultClient, httpURL+"/api/network")
	if err := json.Unmarshal(body, &netInfo); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(netInfo.ViewerBaseUrl, "http://") || netInfo.HttpsPort == nil || *netInfo.HttpsPort != httpsPort {
		t.Errorf("GET /api/network = %s", body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeListeners returned %v, want nil", err)
		}
	case <-time.After(ShutdownTimeout):
		t.Fatal("ServeListeners did not return after cancel")
	}
	for _, ln := range []net.Listener{httpLn, httpsLn} {
		if c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
			_ = c.Close()
			t.Errorf("%s still accepts connections after shutdown", ln.Addr())
		}
	}
	if a.tls.HTTPSPort() != 0 {
		t.Error("HTTPS port still reported after shutdown")
	}
}

func TestTLSModes(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.Config
		wantMode   string
		wantCACode int
	}{
		{"local CA by default", config.Config{}, `"mode":"local-ca"`, 200},
		{"disabled", config.Config{TLS: config.TLS{Mode: config.TLSModeDisabled}}, `"enabled":false,"mode":"disabled"`, 404},
		{"acme for a public https domain", config.Config{PublicBaseURL: "https://subs.example.com"}, `"mode":"acme"`, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp(t, tt.cfg, testDist)
			srv := &http.Server{Handler: a.Handler()}
			ln := listen(t)
			go func() { _ = srv.Serve(ln) }()
			t.Cleanup(func() { _ = srv.Close() })
			base := "http://" + ln.Addr().String()
			if _, body := get(t, http.DefaultClient, base+"/api/tls"); !strings.Contains(string(body), tt.wantMode) {
				t.Errorf("GET /api/tls = %s, want %s", body, tt.wantMode)
			}
			if res, body := get(t, http.DefaultClient, base+"/api/tls/ca.crt"); res.StatusCode != tt.wantCACode {
				t.Errorf("GET /api/tls/ca.crt = %d %s, want %d", res.StatusCode, body, tt.wantCACode)
			}
		})
	}
}

func TestRunFailsWhenHTTPSPortIsTaken(t *testing.T) {
	taken := listen(t)
	defer func() { _ = taken.Close() }()
	a := newTestApp(t, config.Config{TLS: config.TLS{HTTPSAddr: taken.Addr().String()}}, testDist)
	if err := a.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "listen https") {
		t.Fatalf("Run = %v, want a listen https error", err)
	}
}

func TestRunWithoutHTTPSWhenDefaultPortIsTaken(t *testing.T) {
	// Hold the default HTTPS port for a loopback --addr; if something
	// else already holds it, it's just as taken.
	if taken, err := net.Listen("tcp", config.DefaultHTTPSAddr("127.0.0.1:0")); err == nil {
		defer func() { _ = taken.Close() }()
	}
	a := newTestApp(t, config.Config{}, testDist)
	var out strings.Builder
	a.out = &syncWriter{w: &out, ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-a.out.(*syncWriter).ready: // the banner is printed once both listeners are up
	case err := <-done:
		t.Fatalf("Run = %v, want it to serve HTTP without HTTPS", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not start")
	}
	if a.tls.HTTPSPort() != 0 {
		t.Error("HTTPS port reported although HTTPS did not start")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// syncWriter closes ready on the first write.
type syncWriter struct {
	w     io.Writer
	ready chan struct{}
	once  sync.Once
}

func (s *syncWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	s.once.Do(func() { close(s.ready) })
	return n, err
}

func TestBannerHTTPS(t *testing.T) {
	a := newTestApp(t, config.Config{Addr: "127.0.0.1:8080"}, testDist)
	a.tls.SetHTTPSPort(8443)
	var out strings.Builder
	a.out = &out
	a.banner()
	for _, want := range []string{
		"Audience  http://localhost:8080/s",
		"HTTPS     https://localhost:8443/admin",
		"Local CA  http://localhost:8080/api/tls/ca.crt",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("banner %q does not contain %q", out.String(), want)
		}
	}
}
