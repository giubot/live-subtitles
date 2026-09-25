// SPDX-License-Identifier: Apache-2.0

package tlsutil

import "testing"

func TestSelectMode(t *testing.T) {
	tests := []struct {
		name, mode, cert, base string
		want                   Mode
		wantDomain             string
		wantErr                bool
	}{
		{name: "auto on the LAN", mode: "auto", want: ModeLocalCA},
		{name: "empty is auto", want: ModeLocalCA},
		{name: "auto with a cert", mode: "auto", cert: "/c.pem", base: "https://subs.example.com", want: ModeProvided},
		{name: "auto with a public https domain", mode: "auto", base: "https://Subs.Example.com/", want: ModeACME, wantDomain: "subs.example.com"},
		{name: "auto with an http domain", mode: "auto", base: "http://subs.example.com", want: ModeLocalCA},
		{name: "auto with an https IP", mode: "auto", base: "https://203.0.113.7", want: ModeLocalCA},
		{name: "auto with .local", mode: "auto", base: "https://mini-pc.local:8443", want: ModeLocalCA},
		{name: "local-ca wins over a domain", mode: "local-ca", base: "https://subs.example.com", want: ModeLocalCA},
		{name: "disabled", mode: "disabled", cert: "/c.pem", want: ModeDisabled},
		{name: "provided", mode: "provided", cert: "/c.pem", want: ModeProvided},
		{name: "provided without cert", mode: "provided", wantErr: true},
		{name: "acme", mode: "acme", base: "https://subs.example.org:443", want: ModeACME, wantDomain: "subs.example.org"},
		{name: "acme without a public domain", mode: "acme", base: "https://localhost", wantErr: true},
		{name: "unknown", mode: "maybe", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, domain, err := SelectMode(tt.mode, tt.cert, tt.base)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want || domain != tt.wantDomain {
				t.Errorf("got %q %q, want %q %q", got, domain, tt.want, tt.wantDomain)
			}
		})
	}
}

func TestPublicDomain(t *testing.T) {
	tests := map[string]string{
		"https://subs.example.com":         "subs.example.com",
		"https://subs.example.com.":        "subs.example.com",
		"https://subs.example.com:8443/x":  "subs.example.com",
		"http://subs.example.com":          "",
		"https://localhost":                "",
		"https://mini-pc":                  "",
		"https://mini-pc.local":            "",
		"https://router.home.arpa":         "",
		"https://app.localhost":            "",
		"https://192.168.1.20":             "",
		"https://[2800:810::20]":           "",
		"":                                 "",
		"not a url with spaces://%%%":      "",
		"https://subs.congregacion.org.ar": "subs.congregacion.org.ar",
	}
	for in, want := range tests {
		if got := PublicDomain(in); got != want {
			t.Errorf("PublicDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
