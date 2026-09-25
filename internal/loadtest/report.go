// SPDX-License-Identifier: Apache-2.0

package loadtest

import (
	"fmt"
	"io"
	"strings"
)

// Print writes the report as a short human-readable table.
func (r Report) Print(w io.Writer) {
	var b strings.Builder
	total := r.Sessions * r.Viewers
	fmt.Fprintf(&b, "\nLoad test: %d sessions × %d viewers = %d viewers, measured %.0f s\n\n",
		r.Sessions, r.Viewers, total, r.Measured.Seconds())
	server := "n/a"
	if r.ServerViewers >= 0 {
		server = fmt.Sprintf("%.0f", r.ServerViewers)
	}
	fmt.Fprintf(&b, "  viewers        %d connected, %s seen by the server, %d dial failures\n", r.Connected, server, r.DialFailures)
	fmt.Fprintf(&b, "  slow/dropped   %d dropped by the server (1013), %d other disconnects\n", r.Drops, r.Disconnects)
	fmt.Fprintf(&b, "  throughput     %.0f msgs/s, %.2f MB/s to viewers; %.1f caption events/s per session\n",
		r.MessagesPerSec, r.BytesPerSec/1e6, r.EventsPerSecond)
	fmt.Fprintf(&b, "  delivery lag   %s  (finals, behind the session's fastest)\n", r.Lag)
	fmt.Fprintf(&b, "  fan-out skew   %s  (after the session's first viewer)\n", r.Fanout)
	fmt.Fprintf(&b, "  pipeline       %s  (caption latencyMs)\n", r.Pipeline)
	if p := r.Proc; p != nil {
		fmt.Fprintf(&b, "  server CPU     %.1f%% of one core (%.2f%% per session)\n", p.CPUPercent, p.CPUPercent/float64(r.Sessions))
		fmt.Fprintf(&b, "  server RSS     %s peak, %s before sessions (+%s, %s per session)\n",
			mib(p.PeakRSS), mib(p.BaselineRSS), mib(p.PeakRSS-p.BaselineRSS), mib((p.PeakRSS-p.BaselineRSS)/int64(r.Sessions)))
		if p.Children > 0 {
			fmt.Fprintf(&b, "  ffmpeg         %d processes, %.1f%% of one core, %s RSS in total\n", p.Children, p.ChildCPUPercent, mib(p.ChildRSS))
		}
	} else {
		b.WriteString("  server CPU/RSS n/a (pass -pid for a server you started yourself)\n")
	}
	b.WriteString("\n")
	_, _ = io.WriteString(w, b.String())
}

func mib(n int64) string { return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20)) }
