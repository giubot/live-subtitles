// SPDX-License-Identifier: Apache-2.0

package recording

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// memStore is an in-memory Store.
type memStore struct {
	mu   sync.Mutex
	recs map[string]domain.Recording
}

func newMemStore() *memStore { return &memStore{recs: map[string]domain.Recording{}} }

func (m *memStore) CreateRecording(_ context.Context, r domain.Recording) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.recs[r.Id]; ok {
		return domain.ErrConflict
	}
	m.recs[r.Id] = r
	return nil
}

func (m *memStore) GetRecording(_ context.Context, id string) (domain.Recording, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[id]
	if !ok {
		return r, domain.ErrNotFound
	}
	return r, nil
}

func (m *memStore) ListRecordings(_ context.Context, sessionID string) ([]domain.Recording, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []domain.Recording{}
	for _, r := range m.recs {
		if sessionID == "" || r.SessionId == sessionID {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b domain.Recording) int { return b.StartedAt.Compare(a.StartedAt) })
	return out, nil
}

func (m *memStore) UpdateRecording(_ context.Context, r domain.Recording) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.recs[r.Id]; !ok {
		return domain.ErrNotFound
	}
	m.recs[r.Id] = r
	return nil
}

func (m *memStore) DeleteRecording(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.recs[id]; !ok {
		return domain.ErrNotFound
	}
	delete(m.recs, id)
	return nil
}

// fakeSettings holds the recording settings.
type fakeSettings struct{ s *api.Settings }

func (f fakeSettings) Settings(context.Context) (api.Settings, error) {
	if f.s == nil {
		return api.Settings{}, domain.ErrNotFound
	}
	return *f.s, nil
}

func (fakeSettings) PutSettings(context.Context, api.Settings) error { return nil }

func settings(bitrate api.SettingsRecordingBitrateKbps, retentionDays int) fakeSettings {
	var s api.Settings
	s.Recording.BitrateKbps, s.Recording.RetentionDays = bitrate, retentionDays
	return fakeSettings{&s}
}

// fakeClock is a settable clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

var t0 = time.Date(2026, 9, 25, 13, 30, 0, 0, time.UTC)

func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

// fixturePCM is the committed 16 kHz mono English clip as samples.
func fixturePCM(t *testing.T) []int16 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", "fixtures", "en.wav"))
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(b, []byte("data"))
	if i < 0 {
		t.Fatal("no data chunk in the fixture")
	}
	b = b[i+8:]
	pcm := make([]int16, len(b)/2)
	for j := range pcm {
		pcm[j] = int16(binary.LittleEndian.Uint16(b[2*j:]))
	}
	return pcm
}

// frames cuts pcm into 20 ms frames on the session clock from start.
func frames(pcm []int16, start time.Duration) []domain.AudioFrame {
	var out []domain.AudioFrame
	for i := 0; i+domain.FrameSamples <= len(pcm); i += domain.FrameSamples {
		out = append(out, domain.AudioFrame{PCM: pcm[i : i+domain.FrameSamples], T: start + time.Duration(i)*time.Second/domain.SampleRate})
	}
	return out
}

func newTestRecorder(t *testing.T, opts Options) (*Recorder, *memStore) {
	t.Helper()
	st := newMemStore()
	if opts.Dir == "" {
		opts.Dir = t.TempDir()
	}
	opts.Store = st
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	r := New(opts)
	t.Cleanup(r.Close)
	return r, st
}

// ffprobeDuration is the playable duration of a file, or -1 without ffprobe.
func ffprobeDuration(t *testing.T, path string) float64 {
	t.Helper()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return -1
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		t.Fatalf("ffprobe duration %q: %v", out, err)
	}
	return d
}

func record(t *testing.T, r *Recorder, fs []domain.AudioFrame) domain.RecordingSink {
	t.Helper()
	sink, err := r.Start(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if err := sink.Write(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	return sink
}

func TestRecord(t *testing.T) {
	needFFmpeg(t)
	pcm := fixturePCM(t) // 7.85 s
	tests := []struct {
		name   string
		opts   Options
		frames []domain.AudioFrame
		// Per chunk: session clock offset and duration, in seconds.
		wantOffsets, wantDurations []float64
	}{
		{"one file", Options{}, frames(pcm, 0), []float64{0}, []float64{7.84}},
		{"later run of the session", Options{}, frames(pcm, 42*time.Second), []float64{42}, []float64{7.84}},
		{"chunked", Options{Chunk: 3 * time.Second}, frames(pcm, 0), []float64{0, 3, 6}, []float64{3, 3, 1.84}},
		{"short pause is silence", Options{},
			append(frames(pcm[:32000], 0), frames(pcm[32000:64000], 5*time.Second)...), []float64{0}, []float64{7}},
		{"long pause starts a new file", Options{MaxGap: time.Second},
			append(frames(pcm[:32000], 0), frames(pcm[32000:64000], 5*time.Second)...), []float64{0, 5}, []float64{2, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Sessions = fakeSessions{}
			tt.opts.Settings = settings(api.N48, 30)
			tt.opts.Clock = &fakeClock{t: t0}
			r, _ := newTestRecorder(t, tt.opts)
			sink := record(t, r, tt.frames)
			list, _ := r.List(t.Context(), "main")
			if len(list) != len(tt.wantOffsets) {
				t.Fatalf("%d recordings, want %d: %+v", len(list), len(tt.wantOffsets), list)
			}
			slices.SortFunc(list, func(a, b domain.Recording) int { return int(*a.OffsetSec - *b.OffsetSec) })
			if last := list[len(list)-1].Id; sink.ID() != last {
				t.Errorf("sink ID %q, want the last chunk %q", sink.ID(), last)
			}
			for i, rec := range list {
				if rec.Status != api.RecordingStatusComplete || rec.EndedAt == nil || rec.SizeBytes == nil || *rec.SizeBytes < 1000 {
					t.Errorf("chunk %d: %+v", i, rec)
				}
				if got := float64(*rec.OffsetSec); got != tt.wantOffsets[i] {
					t.Errorf("chunk %d offset %v, want %v", i, got, tt.wantOffsets[i])
				}
				if got := float64(*rec.DurationSec); got < tt.wantDurations[i]-0.05 || got > tt.wantDurations[i]+0.05 {
					t.Errorf("chunk %d duration %v, want %v", i, got, tt.wantDurations[i])
				}
				if rec.Title == nil || *rec.Title != "Main stage" || !slices.Equal(rec.Languages, []string{"es", "en"}) {
					t.Errorf("chunk %d title %v, languages %v", i, rec.Title, rec.Languages)
				}
				if want := t0.Add(30 * 24 * time.Hour); rec.ExpiresAt == nil || !rec.ExpiresAt.Equal(want) {
					t.Errorf("chunk %d expires %v, want %v", i, rec.ExpiresAt, want)
				}
				path := filepath.Join(r.opts.Dir, "main", rec.Id+FileExt)
				if !playable(path) {
					t.Errorf("chunk %d: %s isn't a playable fragmented MP4", i, path)
				}
				// AAC frames make the file a little longer than the PCM.
				if d := ffprobeDuration(t, path); d >= 0 && (d < tt.wantDurations[i]-0.1 || d > tt.wantDurations[i]+0.2) {
					t.Errorf("chunk %d plays %.2f s, want %.2f", i, d, tt.wantDurations[i])
				}
			}
		})
	}
}

// fakeSessions knows one session, "main".
type fakeSessions struct{ domain.SessionStore }

func (fakeSessions) GetSession(_ context.Context, id string) (domain.Session, error) {
	if id != "main" {
		return domain.Session{}, domain.ErrNotFound
	}
	return domain.Session{Id: id, Name: "Main stage", TargetLanguages: []string{"es", "en"}}, nil
}

func TestBitrate(t *testing.T) {
	needFFmpeg(t)
	pcm := fixturePCM(t)
	var sizes []int64
	for _, kbps := range []api.SettingsRecordingBitrateKbps{api.N32, api.N64} {
		r, _ := newTestRecorder(t, Options{Settings: settings(kbps, 0)})
		sink := record(t, r, frames(pcm, 0))
		rec, err := r.Get(t.Context(), sink.ID())
		if err != nil {
			t.Fatal(err)
		}
		if rec.ExpiresAt != nil {
			t.Errorf("retention 0 expires %v", rec.ExpiresAt)
		}
		sizes = append(sizes, *rec.SizeBytes)
	}
	if sizes[1] < sizes[0]*3/2 {
		t.Errorf("64 kbps file (%d B) isn't much bigger than 32 kbps (%d B)", sizes[1], sizes[0])
	}
}

// Write never waits for ffmpeg: with an encoder that doesn't read, frames
// are dropped and the pipeline goes on.
func TestWriteNeverBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as the fake ffmpeg")
	}
	stuck := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(stuck, []byte("#!/bin/sh\nsleep 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	r, _ := newTestRecorder(t, Options{FFmpeg: stuck, Buffer: 10})
	sink, err := r.Start(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]int16, 50*domain.SampleRate) // 50 s, far more than the pipe holds
	start := time.Now()
	for _, f := range frames(pcm, 0) {
		if err := sink.Write(f); err != nil {
			break // ffmpeg exited: the sink gave up, which is fine too
		}
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("writing 50 s of audio took %v", d)
	}
	_ = sink.Close()
	rec, err := r.Get(t.Context(), sink.ID())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != api.RecordingStatusFailed {
		t.Errorf("status %s for an encoder that wrote nothing", rec.Status)
	}
}

func TestStartWithoutFFmpeg(t *testing.T) {
	r, _ := newTestRecorder(t, Options{FFmpeg: filepath.Join(t.TempDir(), "no-ffmpeg")})
	if _, err := r.Start(t.Context(), "main"); !errors.Is(err, ErrFFmpeg) {
		t.Errorf("err = %v, want ErrFFmpeg", err)
	}
}

// ffmpeg killed mid-recording (as by kill -9): the file plays up to its
// last fragment and the recording is complete; the sink stops recording.
func TestFFmpegKilled(t *testing.T) {
	needFFmpeg(t)
	pcm := fixturePCM(t)
	r, _ := newTestRecorder(t, Options{})
	sinkI, err := r.Start(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	s := sinkI.(*sink)
	for _, f := range frames(pcm, 0) {
		_ = s.Write(f)
	}
	path := waitForFragments(t, r, s, 3)
	killFFmpeg(t, path)
	// Writes fail once the loop notices the dead encoder.
	deadline := time.Now().Add(5 * time.Second)
	for next := 8 * time.Second; s.Write(domain.AudioFrame{PCM: pcm[:domain.FrameSamples], T: next}) == nil; next += domain.FrameDuration {
		if time.Now().After(deadline) {
			t.Fatal("the sink kept accepting audio after ffmpeg died")
		}
		time.Sleep(domain.FrameDuration)
	}
	if err := s.Close(); err == nil {
		t.Error("Close reported no error after ffmpeg died")
	}
	rec, _ := r.Get(t.Context(), s.ID())
	if rec.Status != api.RecordingStatusComplete || !playable(path) {
		t.Errorf("killed recording: %+v, playable %v", rec, playable(path))
	}
	if d := ffprobeDuration(t, path); d == 0 {
		t.Error("the killed file doesn't play")
	}
}

// waitForFragments waits until the file being written holds n fragments.
func waitForFragments(t *testing.T, r *Recorder, s *sink, n int) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if id := s.ID(); id != "" {
			path := filepath.Join(r.opts.Dir, "main", id+FileExt)
			if b, err := os.ReadFile(path); err == nil && bytes.Count(b, []byte("moof")) >= n {
				return path
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no fragments written")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// killFFmpeg sends SIGKILL to the ffmpeg writing path.
func killFFmpeg(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pkill")
	}
	if err := exec.Command("pkill", "-9", "-f", path).Run(); err != nil {
		t.Fatalf("pkill: %v", err)
	}
}

// After a crash of the whole server, Recover settles the rows it left in
// the recording state from what's on disk.
func TestRecover(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	// A real recording to cut short.
	full, _ := newTestRecorder(t, Options{Dir: dir})
	sink := record(t, full, frames(fixturePCM(t), 0))
	whole, err := os.ReadFile(filepath.Join(dir, "main", sink.ID()+FileExt))
	if err != nil {
		t.Fatal(err)
	}
	firstMoof := bytes.Index(whole, []byte("moof")) - 4

	r, st := newTestRecorder(t, Options{Dir: dir})
	tests := []struct {
		name string
		file []byte // nil: no file
		want api.RecordingStatus
	}{
		{"cut mid-fragment", whole[:len(whole)*2/3], api.RecordingStatusComplete},
		{"complete file", whole, api.RecordingStatusComplete},
		{"header only", whole[:firstMoof], api.RecordingStatusFailed},
		{"cut in the first fragment", whole[:firstMoof+100], api.RecordingStatusFailed},
		{"no file", nil, api.RecordingStatusFailed},
	}
	for i, tt := range tests {
		rec := domain.Recording{Id: "rec-" + strconv.Itoa(i), SessionId: "main", StartedAt: t0, Status: api.RecordingStatusRecording, Languages: []string{}}
		if err := st.CreateRecording(t.Context(), rec); err != nil {
			t.Fatal(err)
		}
		if tt.file != nil {
			if err := os.WriteFile(filepath.Join(dir, "main", rec.Id+FileExt), tt.file, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := r.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, _ := st.GetRecording(t.Context(), "rec-"+strconv.Itoa(i))
			if rec.Status != tt.want {
				t.Errorf("status %s, want %s", rec.Status, tt.want)
			}
			if tt.file != nil && (rec.SizeBytes == nil || *rec.SizeBytes != int64(len(tt.file)) || rec.EndedAt == nil) {
				t.Errorf("size %v, ended %v", rec.SizeBytes, rec.EndedAt)
			}
			if _, err := exec.LookPath("ffprobe"); err == nil && tt.want == api.RecordingStatusComplete &&
				(rec.DurationSec == nil || *rec.DurationSec < 1) {
				t.Errorf("duration %v not taken from the file", rec.DurationSec)
			}
		})
	}
}

func TestRetention(t *testing.T) {
	day := 24 * time.Hour
	now := t0.Add(100 * day)
	tests := []struct {
		name        string
		settings    fakeSettings
		wantDeleted []string
	}{
		{"7 days", settings(api.N32, 7), []string{"old", "old-failed"}},
		{"60 days", settings(api.N32, 60), []string{"old"}},
		{"forever", settings(api.N32, 0), nil},
		{"default 30 days", fakeSettings{}, []string{"old", "old-failed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &fakeClock{t: now}
			r, st := newTestRecorder(t, Options{Settings: tt.settings, Clock: clock})
			ended := func(d time.Duration) *time.Time { e := now.Add(-d); return &e }
			recs := []domain.Recording{
				{Id: "old", StartedAt: now.Add(-90 * day), EndedAt: ended(89 * day), Status: api.RecordingStatusComplete},
				{Id: "old-failed", StartedAt: now.Add(-45 * day), EndedAt: ended(45 * day), Status: api.RecordingStatusFailed},
				{Id: "new", StartedAt: now.Add(-2 * day), EndedAt: ended(2 * day), Status: api.RecordingStatusComplete},
				{Id: "live", StartedAt: now.Add(-200 * day), Status: api.RecordingStatusRecording},
			}
			for i := range recs {
				recs[i].SessionId, recs[i].Languages = "main", []string{}
				rec := recs[i]
				if err := st.CreateRecording(t.Context(), rec); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(r.opts.Dir, "main"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(r.path(rec), []byte("audio"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			n, err := r.SweepExpired(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if n != len(tt.wantDeleted) {
				t.Errorf("deleted %d, want %d", n, len(tt.wantDeleted))
			}
			for _, rec := range recs {
				_, err := st.GetRecording(t.Context(), rec.Id)
				_, ferr := os.Stat(r.path(rec))
				gone := slices.Contains(tt.wantDeleted, rec.Id)
				if gone != errors.Is(err, domain.ErrNotFound) || gone != errors.Is(ferr, os.ErrNotExist) {
					t.Errorf("%s: row err %v, file err %v, want deleted %v", rec.Id, err, ferr, gone)
				}
			}
			// Nothing more expires until the clock moves on.
			if n, _ := r.SweepExpired(t.Context()); n != 0 {
				t.Errorf("second sweep deleted %d", n)
			}
		})
	}
}

func TestDeleteWhileRecording(t *testing.T) {
	needFFmpeg(t)
	pcm := fixturePCM(t)
	r, st := newTestRecorder(t, Options{})
	sinkI, err := r.Start(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	s := sinkI.(*sink)
	for _, f := range frames(pcm, 0) {
		_ = s.Write(f)
	}
	path := waitForFragments(t, r, s, 1)
	if err := r.Delete(t.Context(), s.ID()); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(domain.AudioFrame{PCM: pcm[:domain.FrameSamples], T: time.Hour}); err == nil {
		t.Error("the sink records on after its recording was deleted")
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file left: %v", err)
	}
	if list, _ := st.ListRecordings(t.Context(), ""); len(list) != 0 {
		t.Errorf("rows left: %+v", list)
	}
	if err := r.Delete(t.Context(), s.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestUsage(t *testing.T) {
	r, st := newTestRecorder(t, Options{Dir: filepath.Join(t.TempDir(), "not", "yet")})
	u, err := r.Usage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if u.UsedBytes != 0 || u.Recordings != 0 {
		t.Errorf("empty usage %+v", u)
	}
	if runtime.GOOS != "plan9" && (u.FreeBytes == nil || *u.FreeBytes <= 0) {
		t.Errorf("free bytes %v", u.FreeBytes)
	}
	for i, size := range []int{100, 250} {
		rec := domain.Recording{Id: "r" + strconv.Itoa(i), SessionId: "s" + strconv.Itoa(i), StartedAt: t0, Languages: []string{}}
		_ = st.CreateRecording(t.Context(), rec)
		_ = os.MkdirAll(filepath.Dir(r.path(rec)), 0o750)
		if err := os.WriteFile(r.path(rec), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if u, _ = r.Usage(t.Context()); u.UsedBytes != 350 || u.Recordings != 2 {
		t.Errorf("usage %+v", u)
	}
}

func TestPlayable(t *testing.T) {
	box := func(typ string, payload int) []byte {
		b := make([]byte, 8+payload)
		binary.BigEndian.PutUint32(b, uint32(8+payload))
		copy(b[4:], typ)
		return b
	}
	large := func(typ string, payload int) []byte {
		b := make([]byte, 16+payload)
		binary.BigEndian.PutUint32(b, 1)
		copy(b[4:], typ)
		binary.BigEndian.PutUint64(b[8:], uint64(16+payload))
		return b
	}
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	head := cat(box("ftyp", 16), box("moov", 40))
	frag := cat(box("moof", 30), box("mdat", 100))
	tests := []struct {
		name string
		file []byte
		want bool
	}{
		{"fragments", cat(head, frag, frag, box("mfra", 8)), true},
		{"last fragment cut", cat(head, frag, frag[:50]), true},
		{"64-bit mdat", cat(head, box("moof", 30), large("mdat", 100)), true},
		{"mdat to the end", cat(head, box("moof", 30), []byte{0, 0, 0, 0, 'm', 'd', 'a', 't', 1, 2, 3}), true},
		{"header only", head, false},
		{"first mdat cut", cat(head, box("moof", 30), box("mdat", 100)[:60]), false},
		{"no moov", cat(box("ftyp", 16), frag), false},
		{"mdat without moof", cat(head, box("mdat", 100)), false},
		{"garbage", []byte("not an mp4 file at all"), false},
		{"empty", nil, false},
	}
	dir := t.TempDir()
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strconv.Itoa(i)+FileExt)
			if err := os.WriteFile(path, tt.file, 0o600); err != nil {
				t.Fatal(err)
			}
			if got := playable(path); got != tt.want {
				t.Errorf("playable = %v, want %v", got, tt.want)
			}
		})
	}
	if playable(filepath.Join(dir, "missing.m4a")) {
		t.Error("a missing file is playable")
	}
}
