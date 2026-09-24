// SPDX-License-Identifier: Apache-2.0

package netinfo

import (
	"net/netip"
	"strings"
	"testing"

	"rsc.io/qr"
)

func iface(name, ip string) Iface { return Iface{Name: name, IP: netip.MustParseAddr(ip)} }

func TestUsable(t *testing.T) {
	got := Usable([]Iface{
		iface("en0", "192.168.1.20"),
		iface("en0", "fe80::1"),
		iface("lo", "127.0.0.1"),
		iface("en0", "2800:810::20"),
		iface("x", "0.0.0.0"),
	})
	if len(got) != 2 || got[0].IP.String() != "192.168.1.20" || got[1].IP.String() != "2800:810::20" {
		t.Fatalf("Usable = %v", got)
	}
}

func TestPreferred(t *testing.T) {
	tests := []struct {
		name      string
		ifs       []Iface
		preferred string
		want      string
	}{
		{"192.168 over 10/8", []Iface{iface("en1", "10.0.0.5"), iface("en0", "192.168.1.20")}, "", "192.168.1.20"},
		{"10/8 over 172.16/12", []Iface{iface("eth0", "172.17.0.9"), iface("eth1", "10.1.2.3")}, "", "10.1.2.3"},
		{"IPv4 over IPv6", []Iface{iface("en0", "2800:810::20"), iface("en0", "192.168.1.20")}, "", "192.168.1.20"},
		{"physical over unknown", []Iface{iface("zz0", "192.168.5.1"), iface("wlan0", "192.168.1.9")}, "", "192.168.1.9"},
		{"named interface wins", []Iface{iface("en0", "192.168.1.20"), iface("en8", "10.9.9.9")}, "en8", "10.9.9.9"},
		{"unknown name falls back", []Iface{iface("en0", "192.168.1.20")}, "eth7", "192.168.1.20"},
		{"public IPv4 after private", []Iface{iface("en0", "203.0.113.4"), iface("en1", "172.20.0.2")}, "", "172.20.0.2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Preferred(tt.ifs, tt.preferred)
			if !ok || got.IP.String() != tt.want {
				t.Fatalf("Preferred = %v, want %s", got, tt.want)
			}
		})
	}
	if _, ok := Preferred(nil, ""); ok {
		t.Fatal("Preferred(nil) = ok")
	}
}

func TestInfo(t *testing.T) {
	ifs := []Iface{iface("en0", "192.168.1.20"), iface("en0", "2800:810::20")}
	tests := []struct {
		name   string
		ifs    []Iface
		opts   Options
		viewer string
	}{
		{"LAN", ifs, Options{HTTPPort: 8080}, "http://192.168.1.20:8080"},
		{"public URL wins", ifs, Options{HTTPPort: 8080, PublicBaseURL: "https://subs.example.com/"}, "https://subs.example.com"},
		{"IPv6 only", []Iface{iface("en0", "2800:810::20")}, Options{HTTPPort: 8080}, "http://[2800:810::20]:8080"},
		{"no network", nil, Options{HTTPPort: 8080}, "http://localhost:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := Info(tt.ifs, tt.opts)
			if info.ViewerBaseUrl != tt.viewer {
				t.Errorf("ViewerBaseUrl = %q, want %q", info.ViewerBaseUrl, tt.viewer)
			}
			if len(info.Interfaces) != len(tt.ifs) {
				t.Errorf("%d interfaces, want %d", len(info.Interfaces), len(tt.ifs))
			}
			if info.HttpsPort != nil || info.SrtPort != nil {
				t.Error("HTTPS/SRT ports set while off")
			}
		})
	}
	info := Info(ifs, Options{HTTPPort: 8080, HTTPSPort: 8443, SRTPort: 9000})
	if *info.HttpsPort != 8443 || *info.SrtPort != 9000 || *info.PreferredIp != "192.168.1.20" || info.Interfaces[1].Family != "ipv6" {
		t.Errorf("Info = %+v", info)
	}
}

// Decoding the half blocks back into modules must reproduce the QR code.
func TestTerminalQR(t *testing.T) {
	const url = "http://192.168.1.20:8080/s"
	out, err := TerminalQR(url)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := qr.Encode(url, qr.M)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	const quiet = 2
	if want := (code.Size + 2*quiet + 1) / 2; len(lines) != want {
		t.Fatalf("%d lines, want %d", len(lines), want)
	}
	for row, line := range lines {
		cells := []rune(strings.TrimSuffix(strings.TrimPrefix(line, "\x1b[30;47m"), "\x1b[0m"))
		if len(cells) != code.Size+2*quiet {
			t.Fatalf("line %d has %d cells", row, len(cells))
		}
		for col, c := range cells {
			x, y := col-quiet, 2*row-quiet
			top := strings.ContainsRune("█▀", c)
			bottom := strings.ContainsRune("█▄", c)
			if top != black(code, x, y) || bottom != black(code, x, y+1) {
				t.Fatalf("cell (%d,%d) = %q does not match the code", col, row, c)
			}
		}
	}
}

func black(c *qr.Code, x, y int) bool {
	return x >= 0 && y >= 0 && x < c.Size && y < c.Size && c.Black(x, y)
}
