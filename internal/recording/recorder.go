// SPDX-License-Identifier: Apache-2.0

package recording

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Store persists recording rows (the recordings table).
type Store interface {
	CreateRecording(ctx context.Context, r domain.Recording) error
	GetRecording(ctx context.Context, id string) (domain.Recording, error)
	ListRecordings(ctx context.Context, sessionID string) ([]domain.Recording, error)
	UpdateRecording(ctx context.Context, r domain.Recording) error
	DeleteRecording(ctx context.Context, id string) error
}

// Defaults used when the settings don't say otherwise.
const (
	DefaultBitrateKbps   = 32
	DefaultRetentionDays = 30
	DefaultChunk         = 30 * time.Minute
	// DefaultMaxGap is the longest pause filled with silence; after a
	// longer one the recording continues in a new file.
	DefaultMaxGap = 5 * time.Minute
	// DefaultBuffer is how many frames (20 ms each) wait for ffmpeg before
	// new ones are dropped: 30 s.
	DefaultBuffer = 1500
)

// FileExt is the extension of recording files (AAC in fragmented MP4).
const FileExt = ".m4a"

// Options configure a Recorder. Dir and Store are required.
type Options struct {
	// Dir holds the files, one directory per session: <Dir>/<session>/<id>.m4a.
	Dir   string
	Store Store
	// Sessions gives a recording its title and languages; optional.
	Sessions domain.SessionStore
	// Settings gives the bitrate and the retention; optional (defaults).
	Settings domain.SettingsStore
	// FFmpeg is the ffmpeg executable (default "ffmpeg" in PATH).
	FFmpeg string
	Clock  domain.Clock
	Logger *slog.Logger
	// Chunk is the longest file; a longer run continues in a new one
	// (default 30 min).
	Chunk time.Duration
	// MaxGap: see DefaultMaxGap.
	MaxGap time.Duration
	// Buffer: see DefaultBuffer.
	Buffer int
	// Progress is how often a recording's row gets its duration and size
	// (default 5 s), so a crash loses little of it.
	Progress time.Duration
	// Sweep is how often the retention job runs (default 1 h).
	Sweep time.Duration
}

// Recorder records session audio (REC-1, REC-2) and applies the retention
// (REC-5). It implements domain.Recorder; Close stops the retention job.
type Recorder struct {
	opts Options
	log  *slog.Logger

	mu     sync.Mutex
	active map[string]*sink // by recording id, while its file is being written
	stop   chan struct{}
	wg     sync.WaitGroup
}

var _ domain.Recorder = (*Recorder)(nil)

// ErrFFmpeg: ffmpeg isn't available, so nothing can be recorded.
var ErrFFmpeg = errors.New("recording: ffmpeg not found")

// New returns a Recorder. Call Recover before the first session starts and
// Close when done.
func New(opts Options) *Recorder {
	if opts.FFmpeg == "" {
		opts.FFmpeg = "ffmpeg"
	}
	if opts.Clock == nil {
		opts.Clock = domain.SystemClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Chunk <= 0 {
		opts.Chunk = DefaultChunk
	}
	if opts.MaxGap <= 0 {
		opts.MaxGap = DefaultMaxGap
	}
	if opts.Buffer <= 0 {
		opts.Buffer = DefaultBuffer
	}
	if opts.Progress <= 0 {
		opts.Progress = 5 * time.Second
	}
	if opts.Sweep <= 0 {
		opts.Sweep = time.Hour
	}
	return &Recorder{opts: opts, log: opts.Logger, active: map[string]*sink{}, stop: make(chan struct{})}
}

// Start begins recording a session run. The file itself starts with the
// first frame, whose session clock time becomes the recording's offset.
func (r *Recorder) Start(ctx context.Context, sessionID string) (domain.RecordingSink, error) {
	if _, err := exec.LookPath(r.opts.FFmpeg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFFmpeg, err)
	}
	if err := os.MkdirAll(filepath.Join(r.opts.Dir, sessionID), 0o750); err != nil {
		return nil, fmt.Errorf("recording: %w", err)
	}
	meta := chunkMeta{sessionID: sessionID, bitrate: DefaultBitrateKbps, languages: []string{}}
	if st, ok := r.settings(ctx); ok && st.Recording.BitrateKbps.Valid() {
		meta.bitrate = int(st.Recording.BitrateKbps)
	}
	if r.opts.Sessions != nil {
		if sess, err := r.opts.Sessions.GetSession(ctx, sessionID); err == nil {
			meta.title = sess.Name
			meta.languages = append(meta.languages, sess.TargetLanguages...)
		}
	}
	s := newSink(r, meta)
	go s.loop()
	return s, nil
}

// settings returns the stored settings; false when there are none yet (or
// they can't be read, which is logged).
func (r *Recorder) settings(ctx context.Context) (api.Settings, bool) {
	if r.opts.Settings == nil {
		return api.Settings{}, false
	}
	st, err := r.opts.Settings.Settings(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			r.log.Warn("read settings; using recording defaults", "err", err)
		}
		return api.Settings{}, false
	}
	return st, true
}

// retention is how long recordings are kept; 0 is forever.
func (r *Recorder) retention(ctx context.Context) time.Duration {
	days := DefaultRetentionDays
	if st, ok := r.settings(ctx); ok {
		days = st.Recording.RetentionDays
	}
	return time.Duration(max(days, 0)) * 24 * time.Hour
}

// expiry is when rec is deleted under the retention keep (0: never),
// counted from its end (or its start while it has none).
func expiry(rec domain.Recording, keep time.Duration) *time.Time {
	if keep <= 0 {
		return nil
	}
	from := rec.StartedAt
	if rec.EndedAt != nil {
		from = *rec.EndedAt
	}
	t := from.Add(keep)
	return &t
}

func (r *Recorder) path(rec domain.Recording) string {
	return filepath.Join(r.opts.Dir, rec.SessionId, rec.Id+FileExt)
}

func (r *Recorder) register(id string, s *sink) {
	r.mu.Lock()
	r.active[id] = s
	r.mu.Unlock()
}

func (r *Recorder) unregister(id string) {
	r.mu.Lock()
	delete(r.active, id)
	r.mu.Unlock()
}

// Recover settles the rows left in the recording state by a crash or a
// kill: a file with at least one complete fragment is playable, so it's
// complete; otherwise the recording failed. Call it before any session
// starts.
func (r *Recorder) Recover(ctx context.Context) error {
	list, err := r.opts.Store.ListRecordings(ctx, "")
	if err != nil {
		return fmt.Errorf("recording: recover: %w", err)
	}
	for _, rec := range list {
		if rec.Status != api.RecordingStatusRecording {
			continue
		}
		r.mu.Lock()
		_, live := r.active[rec.Id]
		r.mu.Unlock()
		if live {
			continue
		}
		path := r.path(rec)
		rec.Status = api.RecordingStatusFailed
		if fi, err := os.Stat(path); err == nil {
			size, end := fi.Size(), fi.ModTime()
			rec.SizeBytes, rec.EndedAt = &size, &end
			if playable(path) {
				rec.Status = api.RecordingStatusComplete
				if d, ok := r.probeDuration(ctx, path); ok {
					rec.DurationSec = &d
				}
			}
		}
		if err := r.opts.Store.UpdateRecording(ctx, rec); err != nil {
			return fmt.Errorf("recording: recover %s: %w", rec.Id, err)
		}
		r.log.Warn("recording was interrupted", "recording", rec.Id, "session", rec.SessionId, "status", rec.Status)
	}
	return nil
}

// probeDuration asks ffprobe (next to ffmpeg) how long a file plays. The
// row's duration is only saved every few seconds, so after a crash the
// file knows better.
func (r *Recorder) probeDuration(ctx context.Context, path string) (float32, bool) {
	bin := "ffprobe"
	if dir, base := filepath.Split(r.opts.FFmpeg); dir != "" {
		bin = filepath.Join(dir, strings.Replace(base, "ffmpeg", "ffprobe", 1))
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, false
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 32)
	return float32(d), err == nil && d > 0
}

// StartRetention runs the retention job now and then every Options.Sweep
// until Close.
func (r *Recorder) StartRetention() {
	r.wg.Go(func() {
		t := time.NewTicker(r.opts.Sweep)
		defer t.Stop()
		for {
			if _, err := r.SweepExpired(context.Background()); err != nil {
				r.log.Warn("recording retention", "err", err)
			}
			select {
			case <-r.stop:
				return
			case <-t.C:
			}
		}
	})
}

// SweepExpired deletes the finished recordings older than the retention
// (REC-5) and returns how many it deleted.
func (r *Recorder) SweepExpired(ctx context.Context) (int, error) {
	keep := r.retention(ctx)
	if keep <= 0 {
		return 0, nil
	}
	list, err := r.opts.Store.ListRecordings(ctx, "")
	if err != nil {
		return 0, err
	}
	now := r.opts.Clock.Now()
	n := 0
	for _, rec := range list {
		exp := expiry(rec, keep)
		if rec.Status == api.RecordingStatusRecording || exp == nil || now.Before(*exp) {
			continue
		}
		if err := r.Delete(ctx, rec.Id); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return n, err
		}
		r.log.Info("recording expired", "recording", rec.Id, "session", rec.SessionId)
		n++
	}
	return n, nil
}

// Close stops the retention job. Running recordings are finished by their
// sessions (their sinks' Close).
func (r *Recorder) Close() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	r.wg.Wait()
}
