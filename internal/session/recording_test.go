// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fakeRecorder hands out sinks that fail after failAfter writes (0: never).
type fakeRecorder struct {
	startErr  error
	failAfter int

	mu    sync.Mutex
	sinks []*fakeSink
}

func (r *fakeRecorder) Start(_ context.Context, sessionID string) (domain.RecordingSink, error) {
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := &fakeSink{id: "rec-" + sessionID, failAfter: r.failAfter}
	r.sinks = append(r.sinks, s)
	return s, nil
}

type fakeSink struct {
	id        string
	failAfter int

	mu     sync.Mutex
	frames []domain.AudioFrame
	closes int
}

func (s *fakeSink) ID() string { return s.id }

func (s *fakeSink) Write(f domain.AudioFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failAfter > 0 && len(s.frames) >= s.failAfter {
		return errors.New("disk full")
	}
	s.frames = append(s.frames, f)
	return nil
}

func (s *fakeSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}

func TestRecorderHook(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		rec        *fakeRecorder
		wantSinks  int
		wantFrames int // 0: don't check
	}{
		{"recording off", false, &fakeRecorder{}, 0, 0},
		{"records every frame", true, &fakeRecorder{}, 1, 50},
		{"a failing sink is dropped", true, &fakeRecorder{failAfter: 10}, 1, 10},
		{"recorder can't start", true, &fakeRecorder{startErr: errors.New("no ffmpeg")}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := sess("main", "es")
			s.RecordingEnabled = tt.enabled
			e := newEnv(t, Options{Recorder: tt.rec}, s)
			if _, err := e.m.Start(t.Context(), "main", &fake.Source{Duration: time.Second}); err != nil {
				t.Fatal(err)
			}
			waitState(t, e.m, "main", api.SessionStateIdle)
			tt.rec.mu.Lock()
			defer tt.rec.mu.Unlock()
			if len(tt.rec.sinks) != tt.wantSinks {
				t.Fatalf("%d sinks, want %d", len(tt.rec.sinks), tt.wantSinks)
			}
			for _, sink := range tt.rec.sinks {
				sink.mu.Lock()
				if len(sink.frames) != tt.wantFrames || sink.closes != 1 {
					t.Errorf("%d frames, %d closes; want %d frames, 1 close", len(sink.frames), sink.closes, tt.wantFrames)
				}
				for i, f := range sink.frames {
					if f.T != time.Duration(i)*domain.FrameDuration {
						t.Fatalf("frame %d at %v: not on the session clock", i, f.T)
					}
				}
				sink.mu.Unlock()
			}
		})
	}
}

func TestStatusRecordingID(t *testing.T) {
	s := sess("main", "es")
	s.RecordingEnabled = true
	e := newEnv(t, Options{Recorder: &fakeRecorder{}}, s)
	if _, err := e.m.Start(t.Context(), "main", &fake.Source{Realtime: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, _ := e.m.Status(t.Context(), "main")
		if st.RecordingId != nil && *st.RecordingId == "rec-main" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never showed the recording: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st, _ := e.m.Stop(t.Context(), "main"); st.RecordingId != nil {
		t.Errorf("idle status still has recording %q", *st.RecordingId)
	}
}

// listingRecorder also lists the session's recordings, as
// recording.Recorder does.
type listingRecorder struct {
	fakeRecorder
	recs []domain.Recording
}

func (r *listingRecorder) List(_ context.Context, sessionID string) ([]domain.Recording, error) {
	var out []domain.Recording
	for _, rec := range r.recs {
		if rec.SessionId == sessionID {
			out = append(out, rec)
		}
	}
	return out, nil
}

// After a server restart the session clock continues past the session's
// recordings too, not only its captions: audio often runs on after the last
// caption (silence, applause, a crash mid-sentence), and a run that started
// inside an earlier recording's window would show its captions in that
// recording's replay.
func TestClockOriginAfterRestartSkipsRecordings(t *testing.T) {
	f := func(v float32) *float32 { return &v }
	tests := []struct {
		name string
		recs []domain.Recording
		want time.Duration
	}{
		{"no recordings: after the last caption", nil, 6 * time.Second},
		{"a recording that ends after the last caption", []domain.Recording{
			{Id: "rec-1", SessionId: "main", OffsetSec: f(0), DurationSec: f(59.4)},
		}, 61 * time.Second},
		{"the latest of several", []domain.Recording{
			{Id: "rec-2", SessionId: "main", OffsetSec: f(70), DurationSec: f(10.2)},
			{Id: "rec-1", SessionId: "main", OffsetSec: f(0), DurationSec: f(59.4)},
		}, 82 * time.Second},
		{"a recording within the captions", []domain.Recording{
			{Id: "rec-1", SessionId: "main", OffsetSec: f(1), DurationSec: f(2)},
		}, 6 * time.Second},
		{"another session's recording", []domain.Recording{
			{Id: "rec-1", SessionId: "side", OffsetSec: f(0), DurationSec: f(500)},
		}, 6 * time.Second},
		{"a recording without a duration yet", []domain.Recording{
			{Id: "rec-1", SessionId: "main", OffsetSec: f(30)},
		}, 31 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, Options{}, sess("main", "es"))
			if err := e.store.SaveCaption(t.Context(), domain.CaptionEvent{SessionId: "main", SegmentId: "r0-s-1",
				Lang: domain.SourceTrack, Final: true, Start: 1, End: 4.5, Text: "hola"}); err != nil {
				t.Fatal(err)
			}
			m := New(Options{Sessions: e.store, Captions: e.store, Bus: e.bus, Recorder: &listingRecorder{recs: tt.recs},
				IngestSource: func(string) domain.AudioSource { return &fake.Source{} }})
			defer m.Close()
			if got := m.clockOrigin(t.Context(), "main"); got != tt.want {
				t.Errorf("clock origin %v, want %v", got, tt.want)
			}
		})
	}
}
