// SPDX-License-Identifier: Apache-2.0

// Package ffmpeg decodes audio with an ffmpeg subprocess into 16 kHz mono
// s16le frames: local files and http(s) URLs (AUD-3) and the SRT listener
// (AUD-5, srt.go).
package ffmpeg

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// DefaultBinary is the ffmpeg looked up in PATH when no path is configured.
const DefaultBinary = "ffmpeg"

// Source is a domain.AudioSource reading one ffmpeg input.
type Source struct {
	// Binary is the ffmpeg executable (default DefaultBinary).
	Binary string
	// Input is ffmpeg's -i argument; Protocols its protocol whitelist, so a
	// file input can't be turned into a network request and vice versa.
	Input     string
	Protocols []string
	// Loop repeats the input forever; StartAt skips its beginning.
	Loop    bool
	StartAt time.Duration
	// Realtime reads the input at its native rate (-re), as a live source.
	Realtime bool
	// InputArgs are input options placed before -i (protocol options such
	// as an SRT passphrase: they may hold secrets and are never logged).
	InputArgs []string
	// Tap, if set, also gets the input's first audio stream as it arrives,
	// stream-copied into MPEG-TS, so its bitrate can be measured. Not
	// supported on Windows, where it is ignored.
	Tap io.Writer
	// Stderr, if set, gets ffmpeg's log output as well.
	Stderr io.Writer
	kind   api.AudioSourceKind

	mu  sync.Mutex
	err error
}

var _ domain.AudioSource = (*Source)(nil)

// Kind is file for files and URLs.
func (s *Source) Kind() api.AudioSourceKind {
	if s.kind == "" {
		return api.AudioSourceKindFile
	}
	return s.kind
}

// Err reports why the last run ended: nil at the end of the input or when
// its context was cancelled, else ffmpeg's error with its last output lines.
func (s *Source) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Source) args() []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if s.Realtime {
		args = append(args, "-re")
	}
	if s.Loop {
		args = append(args, "-stream_loop", "-1")
	}
	if s.StartAt > 0 {
		args = append(args, "-ss", strconv.FormatFloat(s.StartAt.Seconds(), 'f', 3, 64))
	}
	if len(s.Protocols) > 0 {
		args = append(args, "-protocol_whitelist", strings.Join(s.Protocols, ","))
	}
	args = append(args, s.InputArgs...)
	args = append(args, "-i", s.Input,
		"-vn", "-sn", "-dn", "-ac", "1", "-ar", strconv.Itoa(domain.SampleRate),
		"-acodec", "pcm_s16le", "-f", "s16le", "pipe:1")
	if s.tapping() {
		// The tap is the child's fd 3 (cmd.ExtraFiles[0]).
		args = append(args, "-map", "0:a:0", "-c", "copy", "-f", "mpegts", "-flush_packets", "1", "pipe:3")
	}
	return args
}

func (s *Source) tapping() bool { return s.Tap != nil && runtime.GOOS != "windows" }

// Start runs ffmpeg and returns its audio in 20 ms frames, T counting from
// 0. The channel closes at the end of the input, on an ffmpeg error (see
// Err) or when ctx is cancelled, which kills ffmpeg.
func (s *Source) Start(ctx context.Context) (<-chan domain.AudioFrame, error) {
	bin := s.Binary
	if bin == "" {
		bin = DefaultBinary
	}
	cmd := exec.CommandContext(ctx, bin, s.args()...)
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &tail{max: 2048}
	cmd.Stderr = stderr
	if s.Stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, s.Stderr)
	}
	var tapDone chan struct{}
	if s.tapping() {
		r, w, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		cmd.ExtraFiles = []*os.File{w}
		defer func() { _ = w.Close() }() // the child has its own copy once started
		tapDone = make(chan struct{})
		go func() {
			defer close(tapDone)
			_, _ = io.Copy(s.Tap, r)
			_ = r.Close()
		}()
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	s.mu.Lock()
	s.err = nil
	s.mu.Unlock()

	out := make(chan domain.AudioFrame)
	go func() {
		defer close(out)
		readErr := readFrames(ctx, stdout, out)
		waitErr := cmd.Wait()
		if tapDone != nil {
			<-tapDone
		}
		if ctx.Err() != nil {
			return // cancelled: a killed ffmpeg is expected
		}
		err := readErr
		if waitErr != nil {
			err = waitErr
		}
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				err = fmt.Errorf("ffmpeg: %w: %s", err, msg)
			}
			s.mu.Lock()
			s.err = err
			s.mu.Unlock()
		}
	}()
	return out, nil
}

const frameBytes = domain.FrameSamples * 2

// readFrames decodes s16le from r into frames until EOF. A partial frame
// at the end is padded with silence.
func readFrames(ctx context.Context, r io.Reader, out chan<- domain.AudioFrame) error {
	buf := make([]byte, frameBytes)
	var t time.Duration
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			clear(buf[n:])
			pcm := make([]int16, domain.FrameSamples)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(buf[2*i:]))
			}
			select {
			case out <- domain.AudioFrame{PCM: pcm, T: t}:
			case <-ctx.Done():
				return nil
			}
			t += domain.FrameDuration
		}
		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return nil
		case err != nil:
			return err
		}
	}
}

// tail keeps the last max bytes written, for error messages.
type tail struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if over := len(t.b) - t.max; over > 0 {
		t.b = t.b[over:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}
