// SPDX-License-Identifier: Apache-2.0

package ffmpeg

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
)

const protocolsOutput = `Supported file protocols:
Input:
  async
  file
  http
  srt
  srtp
Output:
  file
  srt
  srtp
`

func TestHasInputProtocol(t *testing.T) {
	tests := []struct {
		name, out, proto string
		want             bool
	}{
		{"srt input", protocolsOutput, "srt", true},
		{"srtp is not srt", strings.ReplaceAll(protocolsOutput, "  srt\n", ""), "srt", false},
		{"output only", "Input:\n  file\nOutput:\n  srt\n", "srt", false},
		{"windows line ends", "Input:\r\n  srt\r\nOutput:\r\n", "srt", true},
		{"empty", "", "srt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasInputProtocol(tt.out, tt.proto); got != tt.want {
				t.Errorf("hasInputProtocol = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewSRTSource(t *testing.T) {
	tests := []struct {
		name    string
		opts    SRTOptions
		wantErr error
	}{
		{"no passphrase", SRTOptions{Port: 9000}, nil},
		{"passphrase", SRTOptions{Port: 9000, Passphrase: "0123456789"}, nil},
		{"short passphrase", SRTOptions{Port: 9000, Passphrase: "short"}, ErrSRTPassphrase},
		{"long passphrase", SRTOptions{Port: 9000, Passphrase: strings.Repeat("x", 80)}, ErrSRTPassphrase},
		{"no port", SRTOptions{}, errAny},
		{"port out of range", SRTOptions{Port: 70000}, errAny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewSRTSource(tt.opts)
			switch {
			case tt.wantErr == errAny && err == nil, tt.wantErr != errAny && !errors.Is(err, tt.wantErr):
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && s.Kind() != api.AudioSourceKindSrt {
				t.Errorf("kind %q", s.Kind())
			}
		})
	}
}

var errAny = errors.New("any error")

func TestSRTInputKeepsPassphraseOutOfTheURL(t *testing.T) {
	tests := []struct {
		name     string
		opts     SRTOptions
		wantArgs []string
	}{
		{"defaults", SRTOptions{Port: 9001}, nil},
		{"latency", SRTOptions{Port: 9001, Latency: 200 * time.Millisecond}, []string{"-latency", "200000"}},
		{"passphrase", SRTOptions{Port: 9001, Passphrase: "s3cret&pass=word"}, []string{"-passphrase", "s3cret&pass=word"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewSRTSource(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			in := s.input()
			if in.Input != "srt://0.0.0.0:9001?mode=listener" || !slices.Equal(in.Protocols, []string{"srt"}) || in.Realtime {
				t.Errorf("input %q %v realtime=%v", in.Input, in.Protocols, in.Realtime)
			}
			args := in.args()
			i := slices.Index(args, "-i")
			for k := 0; k < len(tt.wantArgs); k += 2 {
				j := slices.Index(args, tt.wantArgs[k])
				if j < 0 || j > i || args[j+1] != tt.wantArgs[k+1] {
					t.Errorf("args %q lack input option %s %s", args, tt.wantArgs[k], tt.wantArgs[k+1])
				}
			}
			if tt.opts.Passphrase != "" && strings.Contains(in.Input, tt.opts.Passphrase) {
				t.Error("passphrase in the URL")
			}
		})
	}
}

func TestSRTScrub(t *testing.T) {
	s, err := NewSRTSource(SRTOptions{Port: 9000, Passphrase: "0123456789ab"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"no secret", errors.New("exit status 1"), "exit status 1"},
		{"secret", errors.New("bad -passphrase 0123456789ab here"), "bad -passphrase [redacted] here"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.scrub(tt.err)
			if (got == nil) != (tt.err == nil) || (got != nil && got.Error() != tt.want) {
				t.Errorf("scrub = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestRate(t *testing.T) {
	t0 := time.Unix(0, 0)
	tests := []struct {
		name   string
		writes []time.Duration // one 1000-byte write at each offset
		at     time.Duration
		want   float64
		wantOK bool
	}{
		{"nothing yet", nil, 0, 0, false},
		{"within the first window", []time.Duration{0, rateWindow / 2}, rateWindow / 2, 0, false},
		{"one window", []time.Duration{0, rateWindow / 2, rateWindow}, rateWindow, 24 * float64(time.Second) / float64(rateWindow), true},
		{"stale", []time.Duration{0, rateWindow / 2, rateWindow}, 4 * rateWindow, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r rate
			for _, d := range tt.writes {
				r.add(t0.Add(d), 1000)
			}
			got, ok := r.get(t0.Add(tt.at))
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("get = %v, %v; want %v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestLineWriter(t *testing.T) {
	var got []string
	w := &lineWriter{line: func(l string) { got = append(got, l) }}
	for _, p := range []string{"one\ntw", "o\r\n", "\nthree", "\n"} {
		if _, err := w.Write([]byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"one", "two", "three"}; !slices.Equal(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
}

// syncBuffer is a log sink safe for concurrent writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestSRTRejectedCallersAreLogged(t *testing.T) {
	var logs syncBuffer
	s, err := NewSRTSource(SRTOptions{Port: 9000, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	w := &lineWriter{line: s.ffmpegLine}
	// What libsrt prints on the listener for a caller with the wrong passphrase.
	_, _ = w.Write([]byte("01:47:59.110685/SRT:RcvQ:w1!W:SRT.cn: KMREQ/rcv: (snd) Rx process failure - BADSECRET\n" +
		"01:47:59.110979/SRT:RcvQ:w1!W:SRT.cn: processConnectRequest: rsp(REJECT): 1010 - Incorrect passphrase\n" +
		"01:48:01.196494/SRT:RcvQ:w1!W:SRT.cn: processConnectRequest: rsp(REJECT): 1011 - Password required or unexpected\n" +
		"[in#0/mpegts @ 0x1] Error during demuxing: Input/output error\n"))
	out := logs.String()
	if strings.Count(out, "srt caller rejected") != 2 || !strings.Contains(out, `reason="wrong passphrase"`) ||
		!strings.Contains(out, `reason="encryption mismatch"`) {
		t.Errorf("logs:\n%s", out)
	}
}
