// SPDX-License-Identifier: Apache-2.0

package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// probeTimeout bounds each ffmpeg call of Probe.
const probeTimeout = 5 * time.Second

// Capabilities is what an ffmpeg binary can do.
type Capabilities struct {
	// Version is the first line's version, e.g. "8.1.2" or "n7.0-12-g3f2b".
	Version string
	// SRT means ffmpeg was built with libsrt and can read srt:// inputs
	// (SRT ingest, P3-11).
	SRT bool
}

// Probe runs `ffmpeg -version` and `ffmpeg -protocols` with binary (default
// DefaultBinary). An error means ffmpeg can't be run at all.
func Probe(ctx context.Context, binary string) (Capabilities, error) {
	if binary == "" {
		binary = DefaultBinary
	}
	run := func(arg string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, binary, "-hide_banner", arg).Output()
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", binary, arg, err)
		}
		return out, nil
	}
	v, err := run("-version")
	if err != nil {
		return Capabilities{}, err
	}
	caps := Capabilities{Version: ParseVersion(v)}
	if p, err := run("-protocols"); err == nil {
		caps.SRT = HasInputProtocol(p, "srt")
	}
	return caps, nil
}

// ParseVersion reads the version from `ffmpeg -version` output
// ("ffmpeg version 8.1.2 Copyright …").
func ParseVersion(out []byte) string {
	line, _, _ := bytes.Cut(out, []byte("\n"))
	f := strings.Fields(string(line))
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "version" {
			return f[i+1]
		}
	}
	return ""
}

// HasInputProtocol reports whether `ffmpeg -protocols` output lists name
// under "Input:". Names are matched whole, so srtp doesn't count as srt.
func HasInputProtocol(out []byte, name string) bool {
	input := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.EqualFold(line, "Input:"):
			input = true
		case strings.EqualFold(line, "Output:"):
			input = false
		case input && line == name:
			return true
		}
	}
	return false
}

// Prober caches Probe for a binary: ffmpeg doesn't change while the server
// runs, but a missing ffmpeg may be installed, so failures are retried
// after RetryAfter.
type Prober struct {
	Binary string
	// RetryAfter is how long a failed probe is remembered (default 30 s).
	RetryAfter time.Duration
	// probe defaults to Probe; tests replace it.
	probe func(ctx context.Context, binary string) (Capabilities, error)

	mu   sync.Mutex
	caps Capabilities
	err  error
	at   time.Time
	done bool
}

// Capabilities returns the cached probe result, probing on first use and
// after a failure has aged.
func (p *Prober) Capabilities(ctx context.Context) (Capabilities, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	retry := p.RetryAfter
	if retry <= 0 {
		retry = 30 * time.Second
	}
	if p.done && (p.err == nil || time.Since(p.at) < retry) {
		return p.caps, p.err
	}
	probe := p.probe
	if probe == nil {
		probe = Probe
	}
	p.caps, p.err = probe(ctx, p.Binary)
	p.at, p.done = time.Now(), true
	return p.caps, p.err
}

// SupportsSRT reports whether ffmpeg can read srt:// inputs.
func (p *Prober) SupportsSRT(ctx context.Context) bool {
	c, err := p.Capabilities(ctx)
	return err == nil && c.SRT
}
