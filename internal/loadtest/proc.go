// SPDX-License-Identifier: Apache-2.0

package loadtest

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ProcSample is the resource use of a process and of its direct children
// (the ffmpeg of each file source) at one instant.
type ProcSample struct {
	At       time.Time
	CPU      time.Duration // user + system CPU time used so far
	RSS      int64         // resident set size, bytes
	Children int
	ChildCPU time.Duration
	ChildRSS int64
}

// ErrNoProcess means the process wasn't in the ps listing.
var ErrNoProcess = errors.New("loadtest: process not found")

// SampleProc reads pid's CPU time and RSS, and its children's, with ps
// (macOS and Linux; the command is the same on both, only the time format
// differs). It doesn't work on Windows.
func SampleProc(ctx context.Context, pid int) (ProcSample, error) {
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=,rss=,time=").Output()
	if err != nil {
		return ProcSample{}, fmt.Errorf("ps: %w", err)
	}
	return parsePS(out, pid, time.Now())
}

// parsePS picks pid and its children out of `ps -o pid=,ppid=,rss=,time=`.
func parsePS(out []byte, pid int, at time.Time) (ProcSample, error) {
	s := ProcSample{At: at}
	found := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		p, err1 := strconv.Atoi(f[0])
		pp, err2 := strconv.Atoi(f[1])
		rss, err3 := strconv.ParseInt(f[2], 10, 64)
		cpu, err4 := ParseCPUTime(f[3])
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			continue
		}
		switch {
		case p == pid:
			s.CPU, s.RSS, found = cpu, rss*1024, true
		case pp == pid:
			s.Children++
			s.ChildCPU += cpu
			s.ChildRSS += rss * 1024
		}
	}
	if !found {
		return s, ErrNoProcess
	}
	return s, nil
}

// ParseCPUTime reads ps's cumulative CPU time: "[[dd-]hh:]mm:ss[.ff]"
// (Linux prints "00:01:02", macOS "1:02.34").
func ParseCPUTime(v string) (time.Duration, error) {
	var days int
	if d, rest, ok := strings.Cut(v, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, fmt.Errorf("cpu time %q: %w", v, err)
		}
		days, v = n, rest
	}
	parts := strings.Split(v, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("cpu time %q: want [[dd-]hh:]mm:ss", v)
	}
	sec, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil || sec < 0 {
		return 0, fmt.Errorf("cpu time %q: bad seconds", v)
	}
	total := sec
	unit := 60.0
	for i := len(parts) - 2; i >= 0; i-- {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return 0, fmt.Errorf("cpu time %q: bad field %q", v, parts[i])
		}
		total += float64(n) * unit
		unit *= 60
	}
	total += float64(days) * 86400
	return time.Duration(total * float64(time.Second)), nil
}

// CPUPercent is the CPU used between two samples as a percentage of one
// core (200 = two cores busy).
func CPUPercent(from, to ProcSample, children bool) float64 {
	wall := to.At.Sub(from.At)
	if wall <= 0 {
		return 0
	}
	used := to.CPU - from.CPU
	if children {
		used = to.ChildCPU - from.ChildCPU
	}
	return float64(used) / float64(wall) * 100
}
