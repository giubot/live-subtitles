// SPDX-License-Identifier: Apache-2.0

package recording

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

var (
	errClosed  = errors.New("recording: sink closed")
	errDeleted = errors.New("recording: deleted while recording")
)

// chunkMeta is what every file of one session run shares.
type chunkMeta struct {
	sessionID, title string
	languages        []string
	bitrate          int // kbps
}

// sink records one session run. Write only queues the frame, so the
// session pipeline never waits for ffmpeg or the disk; a goroutine (loop)
// feeds ffmpeg and cuts the run into chunks.
type sink struct {
	r      *Recorder
	meta   chunkMeta
	frames chan domain.AudioFrame
	aborts chan abort
	done   chan struct{}

	mu      sync.Mutex
	id      string // current (or last) chunk
	err     error  // why the sink stopped recording
	closed  bool
	dropped int
}

// abort asks the loop to drop the chunk being written (it was deleted).
type abort struct {
	id  string
	ack chan struct{}
}

var _ domain.RecordingSink = (*sink)(nil)

func newSink(r *Recorder, meta chunkMeta) *sink {
	return &sink{
		r: r, meta: meta,
		frames: make(chan domain.AudioFrame, r.opts.Buffer),
		aborts: make(chan abort),
		done:   make(chan struct{}),
	}
}

// ID is the recording being written; empty before the first frame.
func (s *sink) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// Write queues f. When the queue is full the frame is dropped (and logged);
// the gap is filled with silence so the file keeps the session clock. It
// returns an error once the sink can't record any more.
func (s *sink) Write(f domain.AudioFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.closed {
		return errClosed
	}
	select {
	case s.frames <- f:
	default:
		s.dropped++
		if s.dropped == 1 || s.dropped%250 == 0 {
			s.r.log.Warn("recording can't keep up; dropping audio", "session", s.meta.sessionID, "droppedFrames", s.dropped)
		}
	}
	return nil
}

// Close finishes the file with the queued audio. It's safe to call twice.
func (s *sink) Close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.frames)
	}
	s.mu.Unlock()
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if errors.Is(s.err, errDeleted) {
		return nil
	}
	return s.err
}

func (s *sink) fail(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *sink) failed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err != nil
}

// abort drops the chunk id if this sink is writing it, and waits for that.
func (s *sink) abort(id string) {
	a := abort{id: id, ack: make(chan struct{})}
	select {
	case s.aborts <- a:
		<-a.ack
	case <-s.done:
	}
}

func (s *sink) loop() {
	defer close(s.done)
	var c *chunk
	for {
		select {
		case f, ok := <-s.frames:
			if !ok {
				if c != nil {
					c.finish(nil)
				}
				return
			}
			if !s.failed() {
				c = s.frame(c, f)
			}
		case a := <-s.aborts:
			if c != nil && c.rec.Id == a.id {
				c.kill()
				c = nil
				s.fail(errDeleted)
				s.r.log.Info("recording deleted while recording; stopped", "recording", a.id, "session", s.meta.sessionID)
			}
			close(a.ack)
		}
	}
}

// frame writes f to the current chunk, starting a new one first when the
// chunk is full or a long pause came before f. It returns the chunk to
// continue with (nil after a failure).
func (s *sink) frame(c *chunk, f domain.AudioFrame) *chunk {
	o := s.r.opts
	if c != nil && (f.T-c.next() > o.MaxGap || f.T-c.offset >= o.Chunk) {
		c.finish(nil)
		c = nil
	}
	if c == nil {
		var err error
		if c, err = s.startChunk(f.T); err != nil {
			s.r.log.Error("recording did not start", "session", s.meta.sessionID, "err", err)
			s.fail(err)
			return nil
		}
		s.mu.Lock()
		s.id = c.rec.Id
		s.mu.Unlock()
	}
	// Pauses and dropped frames become silence, so the file stays on the
	// session clock.
	gap := int64((f.T - c.next()) * domain.SampleRate / time.Second)
	err := c.silence(gap)
	if err == nil {
		err = c.write(f.PCM)
	}
	if err != nil {
		c.finish(err)
		s.fail(fmt.Errorf("recording: ffmpeg stopped: %w", err))
		return nil
	}
	if now := o.Clock.Now(); now.Sub(c.saved) >= o.Progress {
		c.saved = now
		c.save()
	}
	return c
}

// chunk is one recording file being written by one ffmpeg.
type chunk struct {
	s      *sink
	rec    domain.Recording
	path   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *tail
	offset time.Duration // session clock of the first sample
	n      int64         // samples written
	buf    []byte
	saved  time.Time // last progress update of the row
}

func (s *sink) startChunk(offset time.Duration) (*chunk, error) {
	r := s.r
	ctx := context.Background() // the loop outlives the request that started it
	now := r.opts.Clock.Now()
	off := float32(offset.Seconds())
	rec := domain.Recording{
		SessionId: s.meta.sessionID, StartedAt: now, Status: api.RecordingStatusRecording,
		Languages: s.meta.languages, OffsetSec: &off,
	}
	if s.meta.title != "" {
		rec.Title = &s.meta.title
	}
	rec.ExpiresAt = expiry(rec, r.retention(ctx))
	base := fmt.Sprintf("rec-%s-%s", s.meta.sessionID, now.UTC().Format("20060102-150405"))
	for i := 1; ; i++ {
		rec.Id = base
		if i > 1 {
			rec.Id = fmt.Sprintf("%s-%d", base, i)
		}
		err := r.opts.Store.CreateRecording(ctx, rec)
		if err == nil {
			break
		}
		if !errors.Is(err, domain.ErrConflict) || i >= 100 {
			return nil, fmt.Errorf("recording: create %s: %w", rec.Id, err)
		}
	}
	c := &chunk{s: s, rec: rec, path: r.path(rec), offset: offset, stderr: &tail{max: 2048}, saved: now}
	c.cmd = exec.Command(r.opts.FFmpeg, ffmpegArgs(s.meta.bitrate, c.path)...)
	c.cmd.Stderr = c.stderr
	c.cmd.WaitDelay = 5 * time.Second
	var err error
	if c.stdin, err = c.cmd.StdinPipe(); err == nil {
		err = c.cmd.Start()
	}
	if err != nil {
		c.rec.Status = api.RecordingStatusFailed
		c.update()
		return nil, fmt.Errorf("recording: start ffmpeg: %w", err)
	}
	r.register(rec.Id, s)
	r.log.Info("recording started", "recording", rec.Id, "session", rec.SessionId, "offsetSec", off, "bitrateKbps", s.meta.bitrate)
	return c, nil
}

// ffmpegArgs encode 16 kHz mono s16le from stdin to AAC-LC in fragmented
// MP4 (REC-2). empty_moov puts the header first and each ~1 s fragment is
// flushed to disk as it's done, so a file cut by a crash plays up to its
// last fragment.
func ffmpegArgs(kbps int, path string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "s16le", "-ar", strconv.Itoa(domain.SampleRate), "-ac", "1", "-i", "pipe:0",
		"-c:a", "aac", "-profile:a", "aac_low", "-b:a", strconv.Itoa(kbps) + "k",
		"-movflags", "+empty_moov+default_base_moof+frag_keyframe", "-frag_duration", "1000000",
		"-flush_packets", "1", "-f", "mp4", "-y", path,
	}
}

// next is the session clock just after the last sample written.
func (c *chunk) next() time.Duration {
	return c.offset + time.Duration(c.n)*time.Second/domain.SampleRate
}

func (c *chunk) write(pcm []int16) error {
	c.buf = c.buf[:0]
	for _, v := range pcm {
		c.buf = binary.LittleEndian.AppendUint16(c.buf, uint16(v))
	}
	if _, err := c.stdin.Write(c.buf); err != nil {
		return err
	}
	c.n += int64(len(pcm))
	return nil
}

var zeros = make([]int16, domain.SampleRate)

// silence writes n samples of silence.
func (c *chunk) silence(n int64) error {
	for n > 0 {
		k := min(n, int64(len(zeros)))
		if err := c.write(zeros[:k]); err != nil {
			return err
		}
		n -= k
	}
	return nil
}

// finish ends the file and settles its row: complete if it plays (even
// when ffmpeg stopped early, the fragments written so far do), else failed.
func (c *chunk) finish(cause error) {
	r := c.s.r
	_ = c.stdin.Close()
	waitErr := c.cmd.Wait()
	now := r.opts.Clock.Now()
	c.rec.EndedAt = &now
	c.rec.ExpiresAt = expiry(c.rec, r.retention(context.Background()))
	c.rec.Status = api.RecordingStatusFailed
	if playable(c.path) {
		c.rec.Status = api.RecordingStatusComplete
	}
	c.progress()
	c.update()
	r.unregister(c.rec.Id)
	if cause != nil || waitErr != nil {
		r.log.Warn("recording ended with an error", "recording", c.rec.Id, "session", c.rec.SessionId,
			"status", c.rec.Status, "err", errors.Join(cause, waitErr), "ffmpeg", strings.TrimSpace(c.stderr.String()))
		return
	}
	r.log.Info("recording finished", "recording", c.rec.Id, "session", c.rec.SessionId, "durationSec", *c.rec.DurationSec)
}

// kill stops ffmpeg without finishing the file (the recording was deleted).
func (c *chunk) kill() {
	_ = c.cmd.Process.Kill()
	_ = c.stdin.Close()
	_ = c.cmd.Wait()
	c.s.r.unregister(c.rec.Id)
}

// save stores the duration and size so far.
func (c *chunk) save() {
	c.progress()
	c.update()
}

func (c *chunk) progress() {
	d := float32(float64(c.n) / domain.SampleRate)
	c.rec.DurationSec = &d
	if fi, err := os.Stat(c.path); err == nil {
		size := fi.Size()
		c.rec.SizeBytes = &size
	}
}

func (c *chunk) update() {
	if err := c.s.r.opts.Store.UpdateRecording(context.Background(), c.rec); err != nil {
		c.s.r.log.Error("store recording", "recording", c.rec.Id, "err", err)
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
