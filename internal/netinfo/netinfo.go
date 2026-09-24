// SPDX-License-Identifier: Apache-2.0

package netinfo

import (
	"net"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/iencodev/live-subtitles/internal/api"
)

// Iface is one usable address on a network interface.
type Iface struct {
	Name string
	IP   netip.Addr
}

// virtual matches interfaces that aren't reachable from the LAN: container
// bridges, VM adapters, VPN tunnels and Apple's peer-to-peer links.
var virtual = regexp.MustCompile(`^(docker|br-|veth|virbr|vmnet|vboxnet|cni|flannel|cali|utun|tun|tap|wg|zt|tailscale|awdl|llw|anpi|ap\d|bridge|gif|stf|lo)`)

// Interfaces lists the LAN addresses of this host: up, not loopback, not
// virtual, not link-local.
func Interfaces() ([]Iface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Iface
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || virtual.MatchString(ifc.Name) {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
					out = append(out, Iface{Name: ifc.Name, IP: ip.Unmap()})
				}
			}
		}
	}
	return Usable(out), nil
}

// Usable drops loopback, link-local and unspecified addresses.
func Usable(ifs []Iface) []Iface {
	return slices.DeleteFunc(slices.Clone(ifs), func(i Iface) bool {
		ip := i.IP
		return !ip.IsValid() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()
	})
}

// Preferred picks the address QR codes should use. A named interface wins
// if it has an IPv4 address; otherwise private IPv4 on a physical-looking
// interface (192.168/16, then 10/8, then 172.16/12), then any IPv4, then IPv6.
func Preferred(ifs []Iface, iface string) (Iface, bool) {
	if len(ifs) == 0 {
		return Iface{}, false
	}
	best := slices.MinFunc(ifs, func(a, b Iface) int { return rank(a, iface) - rank(b, iface) })
	return best, true
}

func rank(i Iface, preferred string) int {
	r := 0
	if preferred != "" && i.Name != preferred {
		r += 1000
	}
	switch {
	case !i.IP.Is4():
		r += 100
	case i.IP.IsPrivate():
		switch i.IP.As4()[0] {
		case 192:
		case 10:
			r += 1
		default:
			r += 2
		}
	default:
		r += 10
	}
	if !physical(i.Name) {
		r += 5
	}
	return r
}

// physical guesses Ethernet/Wi-Fi from the name across macOS, Linux and Windows.
func physical(name string) bool {
	for _, p := range []string{"en", "eth", "wl", "Ethernet", "Wi-Fi", "WLAN"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// apiIface is the generated (anonymous) element type of NetworkInfo.Interfaces.
type apiIface = struct {
	Family api.NetworkInfoInterfacesFamily `json:"family"`
	Ip     string                          `json:"ip"`
	Name   string                          `json:"name"`
}

// Options configure the NetworkInfo answer.
type Options struct {
	HTTPPort           int
	HTTPSPort          int    // 0 when HTTPS is off
	SRTPort            int    // 0 when SRT ingest is off
	PublicBaseURL      string // overrides the LAN URL, e.g. https://subs.example.com
	PreferredInterface string
}

// Info builds the /api/network answer from the given interfaces.
func Info(ifs []Iface, o Options) api.NetworkInfo {
	info := api.NetworkInfo{HttpPort: o.HTTPPort, Interfaces: []apiIface{}}
	if h, err := os.Hostname(); err == nil {
		info.Hostname = &h
	}
	for _, i := range ifs {
		fam := api.Ipv4
		if !i.IP.Is4() {
			fam = api.Ipv6
		}
		info.Interfaces = append(info.Interfaces, apiIface{Family: fam, Ip: i.IP.String(), Name: i.Name})
	}
	if o.HTTPSPort > 0 {
		info.HttpsPort = &o.HTTPSPort
	}
	if o.SRTPort > 0 {
		info.SrtPort = &o.SRTPort
	}
	host := "localhost"
	if p, ok := Preferred(ifs, o.PreferredInterface); ok {
		ip := p.IP.String()
		info.PreferredIp = &ip
		host = ip
		if !p.IP.Is4() {
			host = "[" + ip + "]"
		}
	}
	info.ViewerBaseUrl = "http://" + host + ":" + strconv.Itoa(o.HTTPPort)
	if base := strings.TrimRight(o.PublicBaseURL, "/"); base != "" {
		info.PublicBaseUrl = &base
		info.ViewerBaseUrl = base
	}
	return info
}
