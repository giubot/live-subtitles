// SPDX-License-Identifier: Apache-2.0

package ffmpeg

import (
	"context"
	"errors"
	"testing"
	"time"
)

const protocolsWithSRT = `Supported file protocols:
Input:
  async
  file
  srtp
  srt
  tcp
Output:
  file
  srt
`

func TestHasInputProtocol(t *testing.T) {
	for _, c := range []struct {
		name, out, proto string
		want             bool
	}{
		{"srt listed", protocolsWithSRT, "srt", true},
		{"only srtp", "Input:\n  srtp\n  file\nOutput:\n  srtp\n", "srt", false},
		{"only output", "Input:\n  file\nOutput:\n  srt\n", "srt", false},
		{"crlf", "Input:\r\n  srt\r\nOutput:\r\n", "srt", true},
		{"empty", "", "srt", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := HasInputProtocol([]byte(c.out), c.proto); got != c.want {
				t.Errorf("HasInputProtocol = %v, want %v", got, c.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	for _, c := range []struct{ out, want string }{
		{"ffmpeg version 8.1.2 Copyright (c) 2000-2026 the FFmpeg developers\nbuilt with clang\n", "8.1.2"},
		{"ffmpeg version n7.0-12-g3f2b Copyright", "n7.0-12-g3f2b"},
		{"something else", ""},
		{"", ""},
	} {
		if got := ParseVersion([]byte(c.out)); got != c.want {
			t.Errorf("ParseVersion(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestProbeMissingBinary(t *testing.T) {
	if _, err := Probe(t.Context(), "/nonexistent/ffmpeg"); err == nil {
		t.Fatal("Probe of a missing binary succeeded")
	}
}

func TestProberCaches(t *testing.T) {
	calls := 0
	fail := true
	p := &Prober{RetryAfter: time.Hour, probe: func(context.Context, string) (Capabilities, error) {
		calls++
		if fail {
			return Capabilities{}, errors.New("no ffmpeg")
		}
		return Capabilities{Version: "8", SRT: true}, nil
	}}
	for range 2 {
		if p.SupportsSRT(t.Context()) {
			t.Fatal("SupportsSRT with a failing probe")
		}
	}
	if calls != 1 {
		t.Fatalf("failure probed %d times within RetryAfter, want 1", calls)
	}
	p.RetryAfter, fail = time.Nanosecond, false
	time.Sleep(time.Millisecond)
	if !p.SupportsSRT(t.Context()) {
		t.Fatal("SupportsSRT after the failure aged = false")
	}
	p.SupportsSRT(t.Context())
	if calls != 2 {
		t.Fatalf("probed %d times, want 2 (success is cached)", calls)
	}
}
