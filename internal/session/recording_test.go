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
