// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"strings"
)

// TLS modes accepted by --tls. TLSModeAuto picks acme when --public-base-url
// is https with a public domain, provided when --tls-cert is set, and
// local-ca otherwise; internal/tlsutil resolves it.
const (
	TLSModeAuto     = "auto"
	TLSModeLocalCA  = "local-ca"
	TLSModeProvided = "provided"
	TLSModeACME     = "acme"
	TLSModeDisabled = "disabled"
)

// DefaultHTTPSPort is used when --https-addr is not set.
const DefaultHTTPSPort = "8443"

// TLS is the HTTPS configuration (P3-09).
type TLS struct {
	Mode      string // auto | local-ca | provided | acme | disabled
	HTTPSAddr string // HTTPS listen address; "" is DefaultHTTPSAddr(Addr), where a busy port isn't fatal
	CertFile  string // bring-your-own certificate (PEM, may include the chain)
	KeyFile   string // its private key (PEM)
	ACMEEmail string // contact address for Let's Encrypt, optional
}

// tlsFlags registers the TLS flags; finish validates them after parsing.
func tlsFlags(fs *flag.FlagSet, t *TLS, env func(name, def string) string) {
	fs.StringVar(&t.Mode, "tls", env("TLS", TLSModeAuto), "HTTPS mode: auto, local-ca, provided, acme, or disabled/false (LIVESUBS_TLS)")
	fs.StringVar(&t.HTTPSAddr, "https-addr", env("HTTPS_ADDR", ""), "HTTPS listen address; default: the --addr host on port "+DefaultHTTPSPort+" (LIVESUBS_HTTPS_ADDR)")
	fs.StringVar(&t.CertFile, "tls-cert", env("TLS_CERT", ""), "bring-your-own TLS certificate, PEM with chain (LIVESUBS_TLS_CERT)")
	fs.StringVar(&t.KeyFile, "tls-key", env("TLS_KEY", ""), "private key PEM for --tls-cert (LIVESUBS_TLS_KEY)")
	fs.StringVar(&t.ACMEEmail, "acme-email", env("ACME_EMAIL", ""), "contact e-mail for Let's Encrypt, optional (LIVESUBS_ACME_EMAIL)")
}

// finish normalizes the mode and checks the flags fit together.
func (t *TLS) finish() error {
	mode := strings.ToLower(strings.TrimSpace(t.Mode))
	switch mode {
	case "", TLSModeAuto, "true", "on", "1":
		t.Mode = TLSModeAuto
	case TLSModeDisabled, "false", "off", "0":
		t.Mode = TLSModeDisabled
	case TLSModeLocalCA, TLSModeProvided, TLSModeACME:
		t.Mode = mode
	default:
		return fmt.Errorf("tls mode %q: want auto, local-ca, provided, acme or disabled", t.Mode)
	}
	if (t.CertFile == "") != (t.KeyFile == "") {
		return errors.New("--tls-cert and --tls-key go together")
	}
	switch {
	case t.Mode == TLSModeProvided && t.CertFile == "":
		return errors.New("--tls=provided needs --tls-cert and --tls-key")
	case t.CertFile != "" && (t.Mode == TLSModeLocalCA || t.Mode == TLSModeACME):
		return fmt.Errorf("--tls-cert can't be combined with --tls=%s", t.Mode)
	}
	if t.HTTPSAddr != "" {
		if _, _, err := net.SplitHostPort(t.HTTPSAddr); err != nil {
			return fmt.Errorf("https addr: %w", err)
		}
	}
	return nil
}

// DefaultHTTPSAddr is the HTTP address's host on DefaultHTTPSPort, so a
// loopback-only --addr keeps HTTPS on loopback too.
func DefaultHTTPSAddr(httpAddr string) string {
	host, _, err := net.SplitHostPort(httpAddr)
	if err != nil {
		host = ""
	}
	return net.JoinHostPort(host, DefaultHTTPSPort)
}
