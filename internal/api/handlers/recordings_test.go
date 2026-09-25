// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/store"
)

// fakeRecordings serves recordings from memory and files from a temp dir.
type fakeRecordings struct {
	mu    sync.Mutex
	dir   string
	recs  []api.Recording
	usage api.StorageUsage
	fail  error
}

func (f *fakeRecordings) List(_ context.Context, sessionID string) ([]api.Recording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := []api.Recording{}
	for _, r := range f.recs {
		if sessionID == "" || r.SessionId == sessionID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRecordings) Get(_ context.Context, id string) (api.Recording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.recs {
		if r.Id == id {
			return r, nil
		}
	}
	return api.Recording{}, domain.ErrNotFound
}

func (f *fakeRecordings) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.recs, func(r api.Recording) bool { return r.Id == id })
	if i < 0 {
		return domain.ErrNotFound
	}
	f.recs = slices.Delete(f.recs, i, i+1)
	return nil
}

func (f *fakeRecordings) Open(ctx context.Context, id string) (*os.File, api.Recording, error) {
	rec, err := f.Get(ctx, id)
	if err != nil {
		return nil, rec, err
	}
	file, err := os.Open(filepath.Join(f.dir, id+".m4a"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, rec, domain.ErrNotFound
	}
	return file, rec, err
}

func (f *fakeRecordings) Usage(context.Context) (api.StorageUsage, error) { return f.usage, f.fail }

func ptr[T any](v T) *T { return &v }

func recordingsServer(t *testing.T) (http.Handler, *fakeRecordings) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	t0 := time.Date(2026, 9, 25, 13, 30, 0, 0, time.UTC)
	fake := &fakeRecordings{dir: t.TempDir(), usage: api.StorageUsage{UsedBytes: 1234, FreeBytes: ptr(int64(1 << 30)), Recordings: 3}}
	fake.recs = []api.Recording{
		{Id: "rec-a", SessionId: "main", StartedAt: t0, Status: api.RecordingStatusComplete, Languages: []string{"es"}, OffsetSec: ptr(float32(0)), DurationSec: ptr(float32(10))},
		{Id: "rec-b", SessionId: "main", StartedAt: t0.Add(time.Hour), Status: api.RecordingStatusRecording, Languages: []string{"es"}, OffsetSec: ptr(float32(10))},
		{Id: "rec-c", SessionId: "other", StartedAt: t0, Status: api.RecordingStatusFailed, Languages: []string{}},
	}
	audio := make([]byte, 1000)
	for i := range audio {
		audio[i] = byte(i)
	}
	for _, id := range []string{"rec-a", "rec-b"} {
		if err := os.WriteFile(filepath.Join(fake.dir, id+".m4a"), audio, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	s.Recordings = fake
	s.Auth = auth.New(st, auth.Options{AdminToken: testAdminToken, PIN: auth.PINParams{Time: 1, MemKiB: 64, Threads: 1}})
	return s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler)), fake
}

func TestRecordings(t *testing.T) {
	h, fake := recordingsServer(t)
	for _, c := range []call{
		{"GET", "/api/recordings", "", "", "", 200, `"id":"rec-c"`},
		{"GET", "/api/recordings?sessionId=other", "", "", "", 200, `[{"id":"rec-c"`},
		{"GET", "/api/recordings?sessionId=nope", "", "", "", 200, `[]`},
		{"GET", "/api/recordings/rec-a", "", "", "", 200, `"offsetSec":0`},
		{"GET", "/api/recordings/nope", "", "", "", 404, `"code":"recording.not_found"`},
		{"GET", "/api/recordings/usage", "", "", "", 401, `"code":"auth.required"`},
		{"GET", "/api/recordings/usage", "", "", testAdminToken, 200, `{"freeBytes":1073741824,"recordings":3,"usedBytes":1234}`},
		{"DELETE", "/api/recordings/rec-c", "", "", "", 401, `"code":"auth.required"`},
		{"DELETE", "/api/recordings/nope", "", "", testAdminToken, 404, `"code":"recording.not_found"`},
		{"DELETE", "/api/recordings/rec-c", "", "", testAdminToken, 204, ""},
		{"GET", "/api/recordings/rec-c", "", "", "", 404, `"code":"recording.not_found"`},
		{"POST", "/api/recordings/rec-a/reprocess", "", "", testAdminToken, 501, `"code":"not_implemented"`},
	} {
		c.do(t, h)
	}

	fake.fail = errors.New("disk on fire")
	call{"GET", "/api/recordings", "", "", "", 500, `"code":"internal"`}.do(t, h)
}

func TestRecordingAudio(t *testing.T) {
	h, _ := recordingsServer(t)
	tests := []struct {
		name, id, rangeHdr string
		wantStatus         int
		wantRange          string // Content-Range
		wantBody           []byte
		wantCache          string
	}{
		{"whole file", "rec-a", "", 200, "", nil, ""},
		{"range", "rec-a", "bytes=10-19", 206, "bytes 10-19/1000", []byte{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}, ""},
		{"suffix range", "rec-a", "bytes=-2", 206, "bytes 998-999/1000", []byte{byte(998 % 256), byte(999 % 256)}, ""},
		{"unsatisfiable", "rec-a", "bytes=5000-", 416, "bytes */1000", nil, ""},
		{"growing file", "rec-b", "", 200, "", nil, "no-store"},
		{"no file", "rec-c", "", 404, "", nil, ""},
		{"unknown", "nope", "", 404, "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/recordings/"+tt.id+"/audio", nil)
			if tt.rangeHdr != "" {
				req.Header.Set("Range", tt.rangeHdr)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			res := rec.Result()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d: %s", res.StatusCode, tt.wantStatus, body)
			}
			if got := res.Header.Get("Content-Range"); got != tt.wantRange {
				t.Errorf("Content-Range %q, want %q", got, tt.wantRange)
			}
			if got := res.Header.Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control %q, want %q", got, tt.wantCache)
			}
			switch {
			case tt.wantStatus == 404:
				if !strings.Contains(string(body), `"code":"recording.not_found"`) {
					t.Errorf("body %s", body)
				}
			case tt.wantStatus == 200:
				if len(body) != 1000 || res.Header.Get("Content-Type") != "audio/mp4" || res.Header.Get("Accept-Ranges") != "bytes" {
					t.Errorf("%d bytes, headers %v", len(body), res.Header)
				}
			case tt.wantBody != nil && string(body) != string(tt.wantBody):
				t.Errorf("body %v, want %v", body, tt.wantBody)
			}
		})
	}
}

func TestRecordingsNotImplemented(t *testing.T) {
	h := New().Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
	for _, path := range []string{"/api/recordings", "/api/recordings/usage", "/api/recordings/x", "/api/recordings/x/audio"} {
		call{"GET", path, "", "", "", 501, `"code":"not_implemented"`}.do(t, h)
	}
}

// Captions and subtitles of one recording cover its window of the session
// clock, shifted to start at 0.
func TestRecordingCaptions(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now()
	for _, id := range []string{"main", "other"} {
		if err := st.CreateSession(ctx, domain.Session{Id: id, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for i, start := range []float32{1, 9, 12, 25} {
		if err := st.SaveCaption(ctx, domain.CaptionEvent{SessionId: "main", Lang: "es", SegmentId: fmt.Sprintf("s-%d", i),
			Final: true, Start: start, End: start + 2, Text: fmt.Sprintf("frase %d", i), SourceLang: "en"}); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeRecordings{recs: []api.Recording{
		{Id: "rec-a", SessionId: "main", Status: api.RecordingStatusComplete, OffsetSec: ptr(float32(0)), DurationSec: ptr(float32(10))},
		{Id: "rec-b", SessionId: "main", Status: api.RecordingStatusRecording, OffsetSec: ptr(float32(10)), DurationSec: ptr(float32(5))},
		{Id: "rec-o", SessionId: "other", Status: api.RecordingStatusComplete},
	}}
	s := New()
	s.Sessions, s.Captions, s.Settings, s.Recordings = st, st, st, fake
	h := s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))

	tests := []struct {
		name, query string
		wantStatus  int
		want        []string // text@start
	}{
		{"whole session", "", 200, []string{"frase 0@1", "frase 1@9", "frase 2@12", "frase 3@25"}},
		{"first recording", "&recordingId=rec-a", 200, []string{"frase 0@1", "frase 1@9"}},
		{"recording in progress", "&recordingId=rec-b", 200, []string{"frase 2@2", "frase 3@15"}},
		{"other session's recording", "&recordingId=rec-o", 404, nil},
		{"unknown recording", "&recordingId=nope", 404, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, body := get(t, h, "/api/public/sessions/main/captions?lang=es"+tt.query)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d: %s", res.StatusCode, body)
			}
			if tt.wantStatus != 200 {
				if !strings.Contains(body, "recording.not_found") {
					t.Errorf("body %s", body)
				}
				return
			}
			var page api.CaptionPage
			if err := json.Unmarshal([]byte(body), &page); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, c := range page.Items {
				got = append(got, fmt.Sprintf("%s@%g", c.Text, c.Start))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	res, body := get(t, h, "/api/public/sessions/main/subtitles?lang=es&format=srt&recordingId=rec-b")
	if res.StatusCode != 200 || !strings.HasPrefix(body, "1\n00:00:02,000 --> 00:00:04,000\nfrase 2") ||
		res.Header.Get("Content-Disposition") != `attachment; filename="rec-b-es.srt"` {
		t.Errorf("recording SRT %d %v:\n%s", res.StatusCode, res.Header, body)
	}
	if res, _ := get(t, h, "/api/public/sessions/main/subtitles?lang=es&format=vtt&recordingId=nope"); res.StatusCode != 404 {
		t.Errorf("unknown recording subtitles: %d", res.StatusCode)
	}
}
