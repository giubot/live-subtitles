// SPDX-License-Identifier: Apache-2.0

package session

import (
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fakeStreamCaptions records the run hooks.
type fakeStreamCaptions struct {
	mu      sync.Mutex
	started []string
	ended   []string
}

func (f *fakeStreamCaptions) RunStarted(sess domain.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, sess.Id)
}

func (f *fakeStreamCaptions) RunEnded(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = append(f.ended, id)
}

func (f *fakeStreamCaptions) Status(string) *api.StreamCaptionStatus {
	return &api.StreamCaptionStatus{State: api.StreamCaptionStatusStateOk}
}

func (f *fakeStreamCaptions) calls() (started, ended int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started), len(f.ended)
}

func TestStreamCaptionsHook(t *testing.T) {
	tests := []struct {
		name        string
		src         domain.AudioSource
		wantStarted int
	}{
		{"a run starts and ends", &fake.Source{Duration: time.Second}, 1},
		{"the source can't start", &failingSource{}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc := &fakeStreamCaptions{}
			e := newEnv(t, Options{StreamCaptions: cc}, sess("main", "es"))
			_, _ = e.m.Start(t.Context(), "main", tt.src)
			deadline := time.Now().Add(10 * time.Second)
			for {
				started, ended := cc.calls()
				if started == tt.wantStarted && ended >= started {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%d starts and %d ends, want %d balanced", started, ended, tt.wantStarted)
				}
				time.Sleep(5 * time.Millisecond)
			}
			st, err := e.m.Status(t.Context(), "main")
			if err != nil {
				t.Fatal(err)
			}
			if st.StreamCaptions == nil || st.StreamCaptions.State != api.StreamCaptionStatusStateOk {
				t.Errorf("status streamCaptions = %+v, want the service's status", st.StreamCaptions)
			}
		})
	}
}
