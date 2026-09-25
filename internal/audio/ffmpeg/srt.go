// SPDX-License-Identifier: Apache-2.0

package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// SupportsSRT reports whether binary (default DefaultBinary) can read
// srt:// inputs, i.e. was built with libsrt: `ffmpeg -protocols` lists srt
// under Input.
func SupportsSRT(ctx context.Context, binary string) (bool, error) {
	if binary == "" {
		binary = DefaultBinary
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "-hide_banner", "-protocols").Output()
	if err != nil {
		return false, fmt.Errorf("ffmpeg -protocols: %w", err)
	}
	return HasInputProtocol(out, "srt"), nil
}

// Passphrase length limits of libsrt (SRTO_PASSPHRASE).
const (
	MinSRTPassphrase = 10
	MaxSRTPassphrase = 79
)

// ErrSRTPassphrase: the passphrase is not 10–79 characters long.
var ErrSRTPassphrase = fmt.Errorf("the SRT passphrase must be %d to %d characters long", MinSRTPassphrase, MaxSRTPassphrase)

// SRTOptions configure an SRT listener source.
type SRTOptions struct {
	// Binary is the ffmpeg executable (default DefaultBinary).
	Binary string
	// Port is the UDP port listened on, on every IPv4 interface.
	Port int
	// Latency is the SRT receiver latency; 0 keeps libsrt's default (120 ms).
	Latency time.Duration
	// Passphrase turns on AES encryption; empty accepts unencrypted callers
	// only. It is passed to ffmpeg as an option, never in the URL, so it
	// doesn't show up in ffmpeg's error messages.
	Passphrase string
	Logger     *slog.Logger
	// Level tunes the level meter (AUD-6).
	Level audio.LevelMeterConfig
	// RestartDelay is the pause before the listener reopens after a
	// sender left (default 250 ms). Repeated failures back off up to 5 s.
	RestartDelay time.Duration
	// now is the wall clock (tests).
	now func() time.Time
}

// SRTSource is a domain.AudioSource that listens for one SRT caller at a
// time (vMix, OBS, a hardware encoder) and decodes the first audio stream
// of its MPEG-TS (AAC, MP2, Opus, AC-3…) to 16 kHz mono (AUD-5).
//
// The source outlives the sender: when the caller disconnects, ffmpeg ends
// and the listener is reopened, so the session stays live and the next
// connection continues the same frame stream. As with browser ingest, T
// jumps forward by the wall time between the last audio of one connection
// and the first of the next (AudioStatus.lastGapMs).
//
// The source only fails (Err) when ffmpeg can't even open the listener
// three times in a row, e.g. the port is taken.
type SRTSource struct {
	opts SRTOptions
	log  *slog.Logger

	mu        sync.Mutex
	running   bool
	connected bool
	meter     *audio.LevelMeter
	level     audio.Level
	rate      rate
	lastGap   time.Duration
	gapKnown  bool
	rejected  int
	err       error
}

var _ domain.AudioSource = (*SRTSource)(nil)

// NewSRTSource validates opts and returns the source; ErrSRTPassphrase
// for a passphrase libsrt would refuse.
func NewSRTSource(opts SRTOptions) (*SRTSource, error) {
	if n := len(opts.Passphrase); n > 0 && (n < MinSRTPassphrase || n > MaxSRTPassphrase) {
		return nil, ErrSRTPassphrase
	}
	if opts.Port < 1 || opts.Port > 65535 {
		return nil, fmt.Errorf("srt: invalid port %d", opts.Port)
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.RestartDelay <= 0 {
		opts.RestartDelay = 250 * time.Millisecond
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	return &SRTSource{
		opts:  opts,
		log:   opts.Logger.With("port", opts.Port),
		meter: audio.NewLevelMeter(opts.Level),
		level: audio.Level{RMSDBFS: audio.FloorDBFS, PeakDBFS: audio.FloorDBFS},
	}, nil
}

// Kind is srt.
func (s *SRTSource) Kind() api.AudioSourceKind { return api.AudioSourceKindSrt }

// Port is the UDP port the source listens on.
func (s *SRTSource) Port() int { return s.opts.Port }

// Err reports why the stream ended: nil when its context was cancelled,
// else why the listener couldn't be opened.
func (s *SRTSource) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// input is the ffmpeg input of one listener run.
func (s *SRTSource) input() *Source {
	args := []string{
		// Start decoding within about a second of the caller connecting.
		"-analyzeduration", "1000000", "-probesize", "500000",
	}
	if s.opts.Latency > 0 {
		args = append(args, "-latency", strconv.FormatInt(s.opts.Latency.Microseconds(), 10))
	}
	if s.opts.Passphrase != "" {
		args = append(args, "-passphrase", s.opts.Passphrase)
	}
	return &Source{
		Binary:    s.opts.Binary,
		Input:     "srt://0.0.0.0:" + strconv.Itoa(s.opts.Port) + "?mode=listener",
		Protocols: []string{"srt"},
		InputArgs: args,
		Tap:       tapWriter{s},
		Stderr:    &lineWriter{line: s.ffmpegLine},
		kind:      api.AudioSourceKindSrt,
	}
}

// Start opens the listener and returns the frame stream, which stays open
// across sender reconnects until ctx is cancelled or the listener fails.
func (s *SRTSource) Start(ctx context.Context) (<-chan domain.AudioFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil, errors.New("srt: source already started")
	}
	first := s.input()
	frames, err := first.Start(ctx)
	if err != nil {
		return nil, s.scrub(err)
	}
	s.running, s.err = true, nil
	out := make(chan domain.AudioFrame)
	go s.loop(ctx, first, frames, out)
	s.log.Info("srt listener open", "latency_ms", s.opts.Latency.Milliseconds(), "encrypted", s.opts.Passphrase != "")
	return out, nil
}

// quickFailure is how soon after opening an ffmpeg failure counts as the
// listener not opening at all (a caller needs longer to connect and probe).
const quickFailure = time.Second

func (s *SRTSource) loop(ctx context.Context, src *Source, frames <-chan domain.AudioFrame, out chan<- domain.AudioFrame) {
	defer func() {
		s.mu.Lock()
		s.running, s.connected = false, false
		s.mu.Unlock()
		close(out)
	}()
	var (
		nextT     time.Duration // T of the next frame
		lastAudio time.Time
		hadAudio  bool
		fails     int
		delay     = s.opts.RestartDelay
	)
	for {
		opened := s.opts.now()
		got := false
		var offset time.Duration // this connection's T origin
		for f := range frames {
			now := s.opts.now()
			if !got {
				got = true
				offset = nextT
				s.mu.Lock()
				if hadAudio {
					gap := max(0, now.Sub(lastAudio)).Round(domain.FrameDuration)
					offset += gap
					s.lastGap, s.gapKnown = gap, true
				}
				s.connected = true
				s.mu.Unlock()
				s.log.Info("srt sender connected")
			}
			f.T += offset
			nextT, lastAudio, hadAudio = f.End(), now, true
			s.mu.Lock()
			s.meter.Write(f.PCM)
			s.level = s.meter.Level()
			s.mu.Unlock()
			select {
			case out <- f:
			case <-ctx.Done():
			}
		}
		s.mu.Lock()
		s.connected, s.rate = false, rate{}
		s.level = audio.Level{RMSDBFS: audio.FloorDBFS, PeakDBFS: audio.FloorDBFS}
		s.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		switch err := s.scrub(src.Err()); {
		case got:
			s.log.Info("srt sender disconnected")
			fails, delay = 0, s.opts.RestartDelay
		case err == nil:
			fails = 0
		case s.opts.now().Sub(opened) < quickFailure:
			if fails++; fails >= 3 {
				s.mu.Lock()
				s.err = err
				s.mu.Unlock()
				s.log.Warn("srt listener failed", "err", err)
				return
			}
			delay = min(2*delay, 5*time.Second)
		default: // a caller connected but sent nothing ffmpeg could decode
			s.log.Warn("srt stream ended without audio", "err", err)
			fails, delay = 0, min(2*delay, 5*time.Second)
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		src = s.input()
		var serr error
		if frames, serr = src.Start(ctx); serr != nil {
			s.mu.Lock()
			s.err = s.scrub(serr)
			s.mu.Unlock()
			return
		}
	}
}

// scrub removes the passphrase from an error text.
func (s *SRTSource) scrub(err error) error {
	if err == nil || s.opts.Passphrase == "" || !strings.Contains(err.Error(), s.opts.Passphrase) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), s.opts.Passphrase, "[redacted]"))
}

// ffmpegLine watches ffmpeg's (and libsrt's) log for rejected callers.
func (s *SRTSource) ffmpegLine(line string) {
	reason := ""
	switch {
	case strings.Contains(line, "Incorrect passphrase"), strings.Contains(line, "BADSECRET"):
		reason = "wrong passphrase"
	case strings.Contains(line, "Password required"), strings.Contains(line, "UNSECURE"):
		reason = "encryption mismatch"
	default:
		return
	}
	// libsrt logs one rejection on several lines; count it once.
	if !strings.Contains(line, "processConnectRequest") {
		return
	}
	s.mu.Lock()
	s.rejected++
	n := s.rejected
	s.mu.Unlock()
	s.log.Warn("srt caller rejected", "reason", reason, "rejections", n)
}

// Status is the audio status for the dashboard: connected while a sender
// is streaming, with levels.
func (s *SRTSource) Status() api.AudioStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	kind := api.AudioSourceKindSrt
	rms, peak := float32(s.level.RMSDBFS), float32(s.level.PeakDBFS)
	silent, clipping := s.level.Silent, s.level.Clipping
	st := api.AudioStatus{Connected: s.connected, Source: &kind, LevelDbfs: &rms, PeakDbfs: &peak,
		Silent: &silent, Clipping: &clipping}
	if s.gapKnown {
		ms := int(s.lastGap.Milliseconds())
		st.LastGapMs = &ms
	}
	return st
}

// SRTStats reports the connection for SessionStatus.srt (SRT-4). ffmpeg
// doesn't expose libsrt's RTT and loss counters, so only connected and the
// received audio bitrate are known.
func (s *SRTSource) SRTStats() api.SrtStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	connected := s.connected
	st := api.SrtStats{Connected: &connected}
	if kbps, ok := s.rate.get(s.opts.now()); ok && connected {
		v := float32(kbps)
		st.BitrateKbps = &v
	}
	return st
}

// tapWriter counts the stream-copied audio ffmpeg writes to its fd 3.
type tapWriter struct{ s *SRTSource }

func (w tapWriter) Write(p []byte) (int, error) {
	now := w.s.opts.now()
	w.s.mu.Lock()
	w.s.rate.add(now, len(p))
	w.s.mu.Unlock()
	return len(p), nil
}

// rateWindow is how long bytes are counted for one bitrate reading.
const rateWindow = time.Second

// rate measures a byte rate over consecutive windows.
type rate struct {
	start time.Time
	n     int64
	kbps  float64
	ok    bool
}

func (r *rate) add(now time.Time, n int) {
	if r.start.IsZero() {
		r.start = now
	}
	r.n += int64(n)
	if d := now.Sub(r.start); d >= rateWindow {
		r.kbps = float64(r.n) * 8 / d.Seconds() / 1000
		r.ok, r.start, r.n = true, now, 0
	}
}

// get returns the last reading, unless no data came for two windows.
func (r *rate) get(now time.Time) (float64, bool) {
	if !r.ok || now.Sub(r.start) > 2*rateWindow {
		return 0, false
	}
	return r.kbps, true
}

// lineWriter calls line for each complete line written.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	line func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		if i > 0 {
			w.line(string(w.buf[:i]))
		}
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 4096 { // a runaway line: drop it
		w.buf = w.buf[:0]
	}
	return len(p), nil
}
