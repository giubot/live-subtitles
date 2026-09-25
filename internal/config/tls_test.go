// SPDX-License-Identifier: Apache-2.0

package config

import "testing"

func TestLoadTLS(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    TLS
		wantErr bool
	}{
		{name: "defaults", want: TLS{Mode: TLSModeAuto}},
		{name: "https addr flag", args: []string{"-https-addr", ":9443"}, want: TLS{Mode: TLSModeAuto, HTTPSAddr: ":9443"}},
		{name: "https addr env", env: map[string]string{"LIVESUBS_HTTPS_ADDR": "0.0.0.0:443"}, want: TLS{Mode: TLSModeAuto, HTTPSAddr: "0.0.0.0:443"}},
		{name: "tls=false", args: []string{"-tls=false"}, want: TLS{Mode: TLSModeDisabled}},
		{name: "tls off from env", env: map[string]string{"LIVESUBS_TLS": "OFF"}, want: TLS{Mode: TLSModeDisabled}},
		{name: "tls=true is auto", args: []string{"-tls=true"}, want: TLS{Mode: TLSModeAuto}},
		{name: "explicit local-ca", args: []string{"-tls", "Local-CA"}, want: TLS{Mode: TLSModeLocalCA}},
		{name: "explicit acme with email", args: []string{"-tls", "acme", "-acme-email", "ops@example.com"}, want: TLS{Mode: TLSModeACME, ACMEEmail: "ops@example.com"}},
		{
			name: "bring your own from flags",
			args: []string{"-tls-cert", "/etc/ssl/subs.pem", "-tls-key", "/etc/ssl/subs.key"},
			want: TLS{Mode: TLSModeAuto, CertFile: "/etc/ssl/subs.pem", KeyFile: "/etc/ssl/subs.key"},
		},
		{
			name: "bring your own from env, flag wins",
			args: []string{"-tls-cert", "/flag.pem"},
			env:  map[string]string{"LIVESUBS_TLS_CERT": "/env.pem", "LIVESUBS_TLS_KEY": "/env.key", "LIVESUBS_TLS": "provided"},
			want: TLS{Mode: TLSModeProvided, CertFile: "/flag.pem", KeyFile: "/env.key"},
		},
		{name: "cert without key", args: []string{"-tls-cert", "/c.pem"}, wantErr: true},
		{name: "key without cert", env: map[string]string{"LIVESUBS_TLS_KEY": "/k.pem"}, wantErr: true},
		{name: "provided without files", args: []string{"-tls", "provided"}, wantErr: true},
		{name: "cert with local-ca", args: []string{"-tls", "local-ca", "-tls-cert", "/c", "-tls-key", "/k"}, wantErr: true},
		{name: "unknown mode", args: []string{"-tls", "maybe"}, wantErr: true},
		{name: "bad https addr", args: []string{"-https-addr", "8443"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(tt.args, func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got.TLS != tt.want {
				t.Errorf("got %+v, want %+v", got.TLS, tt.want)
			}
		})
	}
}

func TestDefaultHTTPSAddr(t *testing.T) {
	tests := map[string]string{
		"0.0.0.0:8080":   "0.0.0.0:8443",
		":9090":          ":8443",
		"127.0.0.1:8080": "127.0.0.1:8443",
		"[::1]:8080":     "[::1]:8443",
		"garbage":        ":8443",
	}
	for in, want := range tests {
		if got := DefaultHTTPSAddr(in); got != want {
			t.Errorf("DefaultHTTPSAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
