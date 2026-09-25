// SPDX-License-Identifier: Apache-2.0

package srt

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

type settings struct {
	s   *api.Settings
	err error
}

func (f settings) Settings(context.Context) (api.Settings, error) {
	if f.s == nil {
		return api.Settings{}, domain.ErrNotFound
	}
	return *f.s, f.err
}

func (settings) PutSettings(context.Context, api.Settings) error { return nil }

type secretStore struct{ pass string }

func (f secretStore) GetSecret(_ context.Context, name string) (string, domain.SecretSource, error) {
	if name != string(api.SrtPassphrase) || f.pass == "" {
		return "", "", secrets.ErrNotFound
	}
	return f.pass, domain.SecretFromFile, nil
}

func (secretStore) SetSecret(context.Context, string, string) error { return nil }
func (secretStore) DeleteSecret(context.Context, string) error      { return nil }
func (secretStore) SecretInfo(context.Context, string) (domain.SecretInfo, error) {
	return domain.SecretInfo{}, nil
}

func srtSettings(enabled bool, port, latencyMs int) *api.Settings {
	s := &api.Settings{}
	s.Srt = &struct {
		Enabled       *bool `json:"enabled,omitempty"`
		LatencyMs     *int  `json:"latencyMs,omitempty"`
		PassphraseSet *bool `json:"passphraseSet,omitempty"`
		Port          *int  `json:"port,omitempty"`
	}{Enabled: &enabled, Port: &port, LatencyMs: &latencyMs}
	return s
}

func newService(opts Options, libsrt bool) *Service {
	opts.Logger = slog.New(slog.DiscardHandler)
	opts.supports = func(context.Context) (bool, error) { return libsrt, nil }
	return New(opts)
}

// freePort returns a UDP port with the next one free as well.
func freePort(t *testing.T) int {
	t.Helper()
	for range 20 {
		c, err := net.ListenPacket("udp4", ":0")
		if err != nil {
			t.Fatal(err)
		}
		p := c.LocalAddr().(*net.UDPAddr).Port
		_ = c.Close()
		if p < 65535 && portFree(p+1) == nil {
			return p
		}
	}
	t.Fatal("no free UDP ports")
	return 0
}

func TestOpen(t *testing.T) {
	base := freePort(t)
	tests := []struct {
		name     string
		settings *api.Settings
		pass     string
		libsrt   bool
		busy     bool // something else holds the port
		wantCode string
		wantPort int
	}{
		{"defaults", nil, "", true, false, "", DefaultPort},
		{"from settings", srtSettings(true, base, 350), "", true, false, "", base},
		{"passphrase", srtSettings(true, base, 350), "0123456789", true, false, "", base},
		{"short passphrase", srtSettings(true, base, 350), "short", true, false, CodePassphraseInvalid, 0},
		{"disabled", srtSettings(false, base, 350), "", true, false, CodeDisabled, 0},
		{"no libsrt", nil, "", false, false, CodeUnavailable, 0},
		{"port taken", srtSettings(true, base, 350), "", true, true, CodePortBusy, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.busy {
				c, err := net.ListenPacket("udp4", ":"+strconv.Itoa(base))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = c.Close() }()
			}
			s := newService(Options{Settings: settings{s: tt.settings}, Secrets: secretStore{pass: tt.pass}}, tt.libsrt)
			src, err := s.Open(t.Context(), "main")
			if tt.wantCode != "" {
				var coded *domain.CodedError
				if !errors.As(err, &coded) || coded.Code != tt.wantCode {
					t.Fatalf("err = %v, want code %s", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				if tt.wantPort == DefaultPort && strings.Contains(err.Error(), "in use") {
					t.Skip("the default SRT port is in use on this machine")
				}
				t.Fatal(err)
			}
			if src.Port() != tt.wantPort || src.Kind() != api.AudioSourceKindSrt {
				t.Errorf("source on port %d (%s), want %d", src.Port(), src.Kind(), tt.wantPort)
			}
		})
	}
}

func TestPortsPerSession(t *testing.T) {
	s := newService(Options{Settings: settings{s: srtSettings(true, 7000, 200)}}, true)
	ctx := t.Context()
	steps := []struct {
		do       string // "url" or "release"
		session  string
		wantPort string
	}{
		{"url", "a", "7000"},
		{"url", "b", "7001"},
		{"url", "a", "7000"}, // sticky
		{"release", "a", ""},
		{"url", "c", "7000"}, // reuses the freed port
		{"url", "b", "7001"},
	}
	for i, st := range steps {
		if st.do == "release" {
			s.Release(st.session)
			continue
		}
		u, ok := s.IngestURL(ctx, "192.168.1.20", st.session)
		want := "srt://192.168.1.20:" + st.wantPort + "?streamid=" + st.session
		if !ok || u != want {
			t.Errorf("step %d: %q, %v; want %q", i, u, ok, want)
		}
	}
}

func TestIngestURL(t *testing.T) {
	tests := []struct {
		name     string
		settings *api.Settings
		libsrt   bool
		host     string
		want     string
	}{
		{"defaults", nil, true, "192.168.1.20", "srt://192.168.1.20:9000?streamid=main-stage"},
		{"ipv6 host", nil, true, "fe80::1", "srt://[fe80::1]:9000?streamid=main-stage"},
		{"disabled", srtSettings(false, 9000, 200), true, "192.168.1.20", ""},
		{"no libsrt", nil, false, "192.168.1.20", ""},
		{"no host", nil, true, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newService(Options{Settings: settings{s: tt.settings}}, tt.libsrt)
			got, ok := s.IngestURL(t.Context(), tt.host, "main-stage")
			if got != tt.want || ok != (tt.want != "") {
				t.Errorf("IngestURL = %q, %v; want %q", got, ok, tt.want)
			}
		})
	}
}

func TestPortsRunOut(t *testing.T) {
	s := newService(Options{}, true)
	for i := range MaxPorts {
		if _, ok := s.IngestURL(t.Context(), "h", "s"+strconv.Itoa(i)); !ok {
			t.Fatalf("session %d got no port", i)
		}
	}
	if u, ok := s.IngestURL(t.Context(), "h", "one-too-many"); ok {
		t.Errorf("got %q past MaxPorts", u)
	}
	var coded *domain.CodedError
	if _, err := s.Open(t.Context(), "one-too-many"); !errors.As(err, &coded) || coded.Code != CodePortBusy {
		t.Errorf("Open past MaxPorts: %v", err)
	}
}

func TestAvailableIsCached(t *testing.T) {
	calls := 0
	s := New(Options{Logger: slog.New(slog.DiscardHandler)})
	s.opts.supports = func(context.Context) (bool, error) { calls++; return calls > 1, nil }
	for range 3 {
		s.Available(t.Context())
	}
	if calls != 1 {
		t.Errorf("a negative probe ran %d times within %v", calls, reprobe)
	}
	s.mu.Lock()
	s.probed = time.Now().Add(-2 * reprobe)
	s.mu.Unlock()
	for range 2 { // positive now, and kept
		if !s.Available(t.Context()) {
			t.Error("reprobe didn't pick up libsrt")
		}
	}
	if calls != 2 {
		t.Errorf("reprobe: %d calls", calls)
	}
}

func TestPassphraseIsRedacted(t *testing.T) {
	var learned []string
	s := newService(Options{Secrets: secretStore{pass: "0123456789abc"}, Redact: func(v string) { learned = append(learned, v) }}, true)
	if _, err := s.passphrase(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(learned) != 1 || learned[0] != "0123456789abc" {
		t.Errorf("redactor learned %q", learned)
	}
}
