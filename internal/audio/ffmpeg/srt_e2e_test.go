// SPDX-License-Identifier: Apache-2.0

package ffmpeg

import (
	"context"
	"log/slog"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/audio"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// These tests push the committed fixture through a real SRT connection.
// They are skipped unless ffmpeg was built with libsrt.

func needSRT(t *testing.T) {
	t.Helper()
	needFFmpeg(t)
	ok, err := SupportsSRT(t.Context(), DefaultBinary)
	if err != nil || !ok {
		t.Skip("ffmpeg without libsrt")
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// send pushes the fixture's first seconds to port as MPEG-TS over SRT,
// in real time, like `ffmpeg -re -i clip -f mpegts srt://…`.
func send(ctx context.Context, port int, codec, passphrase string, seconds int) error {
	abs, err := filepath.Abs(fixture)
	if err != nil {
		return err
	}
	q := url.Values{"streamid": {"main"}}
	if passphrase != "" {
		q.Set("passphrase", passphrase)
	}
	target := "srt://127.0.0.1:" + strconv.Itoa(port) + "?" + q.Encode()
	return exec.CommandContext(ctx, DefaultBinary, "-hide_banner", "-loglevel", "error", "-nostdin", "-re",
		"-i", abs, "-t", strconv.Itoa(seconds), "-c:a", codec, "-ar", "48000", "-f", "mpegts", target).Run()
}

// receive collects frames from ch until it goes quiet for idle.
func receive(ch <-chan domain.AudioFrame, idle time.Duration) (frames []domain.AudioFrame, closed bool) {
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return frames, true
			}
			frames = append(frames, f)
		case <-time.After(idle):
			return frames, false
		}
	}
}

func TestSRTEndToEnd(t *testing.T) {
	needSRT(t)
	const pass = "correct horse battery"
	port := freeUDPPort(t)
	src, err := NewSRTSource(SRTOptions{Port: port, Latency: 120 * time.Millisecond, Passphrase: pass,
		Logger: slog.New(slog.DiscardHandler), RestartDelay: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // let the listener bind

	// A caller with the wrong passphrase is refused; the listener keeps waiting.
	if err := send(ctx, port, "aac", "wrong passphrase", 1); err == nil {
		t.Error("a caller with the wrong passphrase connected")
	}

	var last time.Duration
	for i, codec := range []string{"aac", "mp2", "libopus"} {
		t.Run(codec, func(t *testing.T) {
			sent := make(chan error, 1)
			go func() { sent <- send(ctx, port, codec, pass, 3) }()
			var frames []domain.AudioFrame
			var sawConnected, sawBitrate bool
			deadline := time.After(20 * time.Second)
		loop:
			for {
				select {
				case f := <-ch:
					frames = append(frames, f)
					if src.Status().Connected {
						sawConnected = true
					}
					if st := src.SRTStats(); st.BitrateKbps != nil && *st.BitrateKbps > 0 {
						sawBitrate = true
					}
				case err := <-sent:
					if err != nil {
						t.Fatalf("send: %v", err)
					}
					more, _ := receive(ch, time.Second)
					frames = append(frames, more...)
					break loop
				case <-deadline:
					t.Fatal("timed out")
				}
			}
			// 3 s is 150 frames; the listener loses up to ~1 s to probing.
			if len(frames) < 80 || len(frames) > 170 {
				t.Errorf("%d frames from 3 s of %s", len(frames), codec)
			}
			var loud int
			for _, f := range frames {
				if f.T < last {
					t.Fatalf("T went back from %v to %v", last, f.T)
				}
				last = f.End()
				if audio.RMSDBFS(f.PCM) > -50 {
					loud++
				}
			}
			if loud < len(frames)/4 {
				t.Errorf("only %d of %d frames have speech", loud, len(frames))
			}
			if !sawConnected {
				t.Error("never reported connected")
			}
			if !sawBitrate && len(frames) > 120 {
				t.Error("never reported a bitrate")
			}
			if i > 0 && src.Status().LastGapMs == nil {
				t.Error("no reconnect gap reported")
			}
			if src.Err() != nil {
				t.Errorf("Err() = %v after a sender left", src.Err())
			}
		})
	}

	cancel()
	if _, closed := receive(ch, 5*time.Second); !closed {
		t.Fatal("stream still open after cancel")
	}
	if err := src.Err(); err != nil {
		t.Errorf("cancel reported %v", err)
	}
	if st := src.SRTStats(); st.Connected == nil || *st.Connected {
		t.Errorf("stats after cancel %+v", st)
	}
}

func TestSRTPortTaken(t *testing.T) {
	needSRT(t)
	c, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	src, err := NewSRTSource(SRTOptions{Port: c.LocalAddr().(*net.UDPAddr).Port, Logger: slog.New(slog.DiscardHandler),
		RestartDelay: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := src.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, closed := receive(ch, 15*time.Second); !closed {
		t.Fatal("listener on a taken port kept running")
	}
	if src.Err() == nil {
		t.Error("no error for a taken port")
	}
}
