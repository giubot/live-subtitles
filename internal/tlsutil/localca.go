// SPDX-License-Identifier: Apache-2.0

package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Validity of the generated certificates. Publicly trusted leaves are capped
// at 398 days; staying at 397 keeps every browser happy even where a
// platform applies that cap to locally installed roots. The leaf is renewed
// RenewBefore its expiry, so it never lapses while the server runs.
const (
	CAValidity    = 10 * 365 * 24 * time.Hour
	LeafValidity  = 397 * 24 * time.Hour
	RenewBefore   = 30 * 24 * time.Hour
	backdate      = time.Hour // tolerate clients whose clock is a little behind
	caCommonName  = "Live Subtitles Local CA"
	caFile        = "ca.crt"
	caKeyFile     = "ca.key"
	leafFile      = "server.crt"
	leafKeyFile   = "server.key"
	certFileMode  = 0o644
	keyFileMode   = 0o600
	stateDirMode  = 0o700
	pemCert       = "CERTIFICATE"
	pemPrivateKey = "PRIVATE KEY"
)

// SANs is the set of names and addresses a leaf certificate covers.
type SANs struct {
	DNS []string
	IPs []netip.Addr
}

// Strings lists the SANs sorted, DNS names first, for display and comparison.
func (s SANs) Strings() []string {
	dns := slices.Clone(s.DNS)
	slices.Sort(dns)
	ips := make([]string, 0, len(s.IPs))
	for _, ip := range s.IPs {
		ips = append(ips, ip.String())
	}
	slices.Sort(ips)
	return slices.Compact(append(dns, ips...))
}

// DesiredSANs is what the local-CA leaf must cover: localhost and loopback,
// every LAN address, the hostname and hostname.local, plus extra hosts
// (such as the --public-base-url host).
func DesiredSANs(hostname string, lan []netip.Addr, extra ...string) SANs {
	dns := []string{"localhost"}
	ips := []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.IPv6Loopback()}
	if h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), "."); validDNSName(h) {
		label, _, _ := strings.Cut(h, ".")
		dns = append(dns, h, label, label+".local")
	}
	for _, ip := range lan {
		if ip.IsValid() {
			ips = append(ips, ip.WithZone("").Unmap())
		}
	}
	for _, h := range extra {
		h = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
		if ip, err := netip.ParseAddr(h); err == nil {
			ips = append(ips, ip.WithZone("").Unmap())
		} else if validDNSName(h) {
			dns = append(dns, h)
		}
	}
	slices.Sort(dns)
	slices.SortFunc(ips, func(a, b netip.Addr) int { return a.Compare(b) })
	return SANs{DNS: slices.Compact(dns), IPs: slices.Compact(ips)}
}

// certSANs reads the SANs of a certificate.
func certSANs(c *x509.Certificate) SANs {
	s := SANs{DNS: slices.Clone(c.DNSNames)}
	for _, ip := range c.IPAddresses {
		if a, ok := netip.AddrFromSlice(ip); ok {
			s.IPs = append(s.IPs, a.Unmap())
		}
	}
	return s
}

// validDNSName accepts RFC 1123 host names (letters, digits, hyphens, dots).
func validDNSName(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for label := range strings.SplitSeq(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// Fingerprint is the SHA-256 of a DER certificate as colon-separated
// uppercase hex, the way browsers and OS certificate dialogs show it.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	var b strings.Builder
	for i, c := range sum {
		if i > 0 {
			b.WriteByte(':')
		}
		fmt.Fprintf(&b, "%02X", c)
	}
	return b.String()
}

// keyPair is a parsed certificate with its key and PEM encodings.
type keyPair struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	keyPEM  []byte
}

func (k *keyPair) tlsCertificate() (*tls.Certificate, error) {
	c, err := tls.X509KeyPair(k.certPEM, k.keyPEM)
	if err != nil {
		return nil, err
	}
	c.Leaf = k.cert
	return &c, nil
}

// newCA makes a self-signed ECDSA P-256 CA valid for CAValidity.
func newCA(hostname string, now time.Time) (*keyPair, error) {
	cn := caCommonName
	if hostname != "" {
		cn += " (" + hostname + ")"
	}
	tmpl := &x509.Certificate{
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"Live Subtitles"}},
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(-backdate).Add(CAValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	return sign(tmpl, nil)
}

// newLeaf makes a server certificate for sans signed by ca, valid for LeafValidity.
func newLeaf(ca *keyPair, sans SANs, now time.Time) (*keyPair, error) {
	cn := "localhost"
	for _, d := range sans.DNS {
		if d != "localhost" && !strings.HasSuffix(d, ".local") {
			cn = d
			break
		}
	}
	tmpl := &x509.Certificate{
		Subject:     pkix.Name{CommonName: cn, Organization: []string{"Live Subtitles"}},
		NotBefore:   now.Add(-backdate),
		NotAfter:    now.Add(-backdate).Add(LeafValidity),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    sans.DNS,
	}
	for _, ip := range sans.IPs {
		tmpl.IPAddresses = append(tmpl.IPAddresses, net.IP(ip.AsSlice()))
	}
	return sign(tmpl, ca)
}

// sign fills a random serial and a fresh key into tmpl and signs it with
// parent, or self-signs when parent is nil.
func sign(tmpl *x509.Certificate, parent *keyPair) (*keyPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	tmpl.SerialNumber = serial
	parentCert, signer := tmpl, key
	if parent != nil {
		parentCert, signer = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, &key.PublicKey, signer)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &keyPair{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: pemCert, Bytes: der}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: pemPrivateKey, Bytes: keyDER}),
	}, nil
}

// loadKeyPair reads a certificate and its ECDSA key from dir. A missing
// file answers os.ErrNotExist.
func loadKeyPair(dir, certName, keyName string) (*keyPair, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, certName))
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, keyName))
	if err != nil {
		return nil, err
	}
	cb, _ := pem.Decode(certPEM)
	if cb == nil || cb.Type != pemCert {
		return nil, fmt.Errorf("%s: no PEM certificate", certName)
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", certName, err)
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil || kb.Type != pemPrivateKey {
		return nil, fmt.Errorf("%s: no PEM private key", keyName)
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", keyName, err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok || !key.PublicKey.Equal(cert.PublicKey) {
		return nil, fmt.Errorf("%s doesn't match %s", keyName, certName)
	}
	return &keyPair{cert: cert, key: key, certPEM: certPEM, keyPEM: keyPEM}, nil
}

// save writes the key (0600) before the certificate (0644), each through a
// temporary file and a rename so a crash never leaves half a file.
func (k *keyPair) save(dir, certName, keyName string) error {
	if err := writeFileAtomic(filepath.Join(dir, keyName), k.keyPEM, keyFileMode); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, certName), k.certPEM, certFileMode)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // no-op after a successful rename
	// CreateTemp already makes the file 0600; certificates are then opened
	// up to 0644. On Windows Chmod only toggles read-only, which is fine.
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
