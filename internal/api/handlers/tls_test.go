// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
)

type fakeTLS struct {
	info api.TlsInfo
	ca   []byte
}

func (f fakeTLS) Info() api.TlsInfo { return f.info }
func (f fakeTLS) CACertPEM() []byte { return f.ca }

func TestTLSHandlers(t *testing.T) {
	const caPEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	port, fp := 8443, "AB:CD"
	notAfter := time.Date(2027, 10, 27, 12, 0, 0, 0, time.UTC)
	localCA := fakeTLS{
		info: api.TlsInfo{Enabled: true, Mode: api.TlsInfoModeLocalCa, HttpsPort: &port, Sans: &[]string{"localhost", "127.0.0.1"}, NotAfter: &notAfter, CaFingerprintSha256: &fp},
		ca:   []byte(caPEM),
	}
	provided := fakeTLS{info: api.TlsInfo{Enabled: true, Mode: api.TlsInfoModeProvided}}
	tests := []struct {
		name, path  string
		tls         TLSService
		wantStatus  int
		wantType    string
		wantBody    []string
		wantHeaders map[string]string
	}{
		{"info", "/api/tls", localCA, 200, "application/json",
			[]string{`"mode":"local-ca"`, `"httpsPort":8443`, `"sans":["localhost","127.0.0.1"]`, `"notAfter":"2027-10-27T12:00:00Z"`, `"caFingerprintSha256":"AB:CD"`, `"enabled":true`}, nil},
		{"info without a service", "/api/tls", nil, 501, "application/json", []string{`"code":"not_implemented"`}, nil},
		{"ca download", "/api/tls/ca.crt", localCA, 200, "application/x-x509-ca-cert", []string{caPEM},
			map[string]string{"Content-Disposition": `attachment; filename="livesubs-ca.crt"`, "Content-Length": "59"}},
		{"no local CA", "/api/tls/ca.crt", provided, 404, "application/json", []string{`"code":"tls.no_local_ca"`}, nil},
		{"ca without a service", "/api/tls/ca.crt", nil, 501, "application/json", []string{`"code":"not_implemented"`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.TLS = tt.tls
			rec := httptest.NewRecorder()
			// Both operations are public (security: []): no credentials sent.
			s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler)).ServeHTTP(rec, httptest.NewRequest("GET", tt.path, nil))
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantType {
				t.Errorf("Content-Type %q, want %q", got, tt.wantType)
			}
			for _, want := range tt.wantBody {
				if !strings.Contains(rec.Body.String(), want) {
					t.Errorf("body %s lacks %s", rec.Body, want)
				}
			}
			for k, v := range tt.wantHeaders {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s %q, want %q", k, got, v)
				}
			}
		})
	}
}
