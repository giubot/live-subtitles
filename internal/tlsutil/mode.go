// SPDX-License-Identifier: Apache-2.0

package tlsutil

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/config"
)

// Mode is how the HTTPS listener gets its certificate (TlsInfo.mode).
type Mode = api.TlsInfoMode

// The modes, as the API reports them.
const (
	ModeLocalCA  = api.TlsInfoModeLocalCa
	ModeProvided = api.TlsInfoModeProvided
	ModeACME     = api.TlsInfoModeAcme
	ModeDisabled = api.TlsInfoModeDisabled
)

// SelectMode resolves the configured --tls mode. auto (or empty) means:
// provided when a certificate file is configured, acme when publicBaseURL
// is https with a public domain, local-ca otherwise. It also returns the
// domain acme should request a certificate for.
func SelectMode(mode, certFile, publicBaseURL string) (Mode, string, error) {
	domain := PublicDomain(publicBaseURL)
	switch mode {
	case config.TLSModeDisabled:
		return ModeDisabled, "", nil
	case config.TLSModeProvided:
		if certFile == "" {
			return "", "", errors.New("tls: --tls=provided needs --tls-cert and --tls-key")
		}
		return ModeProvided, "", nil
	case config.TLSModeLocalCA:
		return ModeLocalCA, "", nil
	case config.TLSModeACME:
		if domain == "" {
			return "", "", fmt.Errorf("tls: --tls=acme needs --public-base-url https://<public domain>, got %q", publicBaseURL)
		}
		return ModeACME, domain, nil
	case "", config.TLSModeAuto:
		switch {
		case certFile != "":
			return ModeProvided, "", nil
		case domain != "":
			return ModeACME, domain, nil
		}
		return ModeLocalCA, "", nil
	}
	return "", "", fmt.Errorf("tls: unknown mode %q", mode)
}

// privateSuffixes are names no public CA will issue for.
var privateSuffixes = []string{".local", ".localhost", ".localdomain", ".lan", ".home", ".internal", ".intranet", ".corp", ".home.arpa", ".test", ".example", ".invalid"}

// PublicDomain returns the host of an https URL when it's a domain Let's
// Encrypt can validate: not an IP, not localhost, not a LAN-only name. It
// returns "" otherwise.
func PublicDomain(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || !strings.Contains(host, ".") || host == "localhost" {
		return ""
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	for _, s := range privateSuffixes {
		if strings.HasSuffix(host, s) {
			return ""
		}
	}
	return host
}
