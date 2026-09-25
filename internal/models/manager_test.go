// SPDX-License-Identifier: Apache-2.0

package models

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// blob is a fake whisper model file.
var blob = func() []byte {
	b := make([]byte, 3<<20)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}()

func blobSHA() string {
	s := sha256.Sum256(blob)
	return hex.EncodeToString(s[:])
}

// events collects published model states.
type events struct {
	mu  sync.Mutex
	all []api.LocalModel
	ch  chan api.LocalModel
}

func newEvents() *events { return &events{ch: make(chan api.LocalModel, 4096)} }

func (e *events) publish(m api.LocalModel) {
	e.mu.Lock()
	e.all = append(e.all, m)
	e.mu.Unlock()
	e.ch <- m
}

// wait returns the first ready or error state of id.
func (e *events) wait(t *testing.T, id string) api.LocalModel {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case m := <-e.ch:
			if m.Id == id && (m.Status == api.LocalModelStatusReady || m.Status == api.LocalModelStatusError) {
				return m
			}
		case <-timeout:
			t.Fatalf("no final state for %s", id)
		}
	}
}

func testModel(sha string) Model {
	return Model{ID: "whisper-test", Kind: api.Whisper, Name: "test", Size: int64(len(blob)), SHA256: sha}
}

// fileServer serves blob with Range support; hook may take over a request.
type fileServer struct {
	requests atomic.Int32
	ranges   []string
	mu       sync.Mutex
	hook     func(n int, w http.ResponseWriter, r *http.Request) bool
}

func (s *fileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := int(s.requests.Add(1))
	s.mu.Lock()
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.mu.Unlock()
	if r.URL.Path != "/ggml-test.bin" {
		http.NotFound(w, r)
		return
	}
	if s.hook != nil && s.hook(n, w, r) {
		return
	}
	http.ServeContent(w, r, "ggml-test.bin", time.Time{}, bytes.NewReader(blob))
}

func TestWhisperDownload(t *testing.T) {
	half := len(blob) / 2
	for _, c := range []struct {
		name         string
		sha          string
		partial      []byte // .part content before the download
		hook         func(n int, w http.ResponseWriter, r *http.Request) bool
		wantStatus   api.LocalModelStatus
		wantCode     string
		wantRequests int32
		wantRange    string // Range header of the first request
	}{
		{name: "fresh", sha: blobSHA(), wantStatus: api.LocalModelStatusReady, wantRequests: 1},
		{name: "resume", sha: blobSHA(), partial: blob[:half], wantStatus: api.LocalModelStatusReady,
			wantRequests: 1, wantRange: "bytes=" + strconv.Itoa(half) + "-"},
		{name: "server ignores range", sha: blobSHA(), partial: []byte("garbage"),
			hook: func(_ int, w http.ResponseWriter, _ *http.Request) bool {
				_, _ = w.Write(blob)
				return true
			}, wantStatus: api.LocalModelStatusReady, wantRequests: 1, wantRange: "bytes=7-"},
		{name: "partial larger than the model", sha: blobSHA(), partial: append(append([]byte{}, blob...), 1, 2),
			wantStatus: api.LocalModelStatusReady, wantRequests: 1},
		{name: "cut mid-body, resumed on retry", sha: blobSHA(),
			hook: func(n int, w http.ResponseWriter, _ *http.Request) bool {
				if n > 1 {
					return false
				}
				w.Header().Set("Content-Length", strconv.Itoa(len(blob)))
				_, _ = w.Write(blob[:half])
				w.(http.Flusher).Flush()
				panic(http.ErrAbortHandler)
			}, wantStatus: api.LocalModelStatusReady, wantRequests: 2},
		{name: "server error retried", sha: blobSHA(),
			hook: func(n int, w http.ResponseWriter, _ *http.Request) bool {
				if n == 1 {
					w.WriteHeader(http.StatusBadGateway)
					return true
				}
				return false
			}, wantStatus: api.LocalModelStatusReady, wantRequests: 2},
		{name: "checksum mismatch", sha: "00" + blobSHA()[2:], wantStatus: api.LocalModelStatusError,
			wantCode: CodeChecksumMismatch, wantRequests: 1},
		{name: "not found is not retried", sha: blobSHA(),
			hook: func(_ int, w http.ResponseWriter, r *http.Request) bool {
				http.NotFound(w, r)
				return true
			}, wantStatus: api.LocalModelStatusError, wantCode: CodeDownloadFailed, wantRequests: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			fs := &fileServer{hook: c.hook}
			srv := httptest.NewServer(fs)
			defer srv.Close()
			dir := t.TempDir()
			mod := testModel(c.sha)
			if c.partial != nil {
				if err := os.WriteFile(filepath.Join(dir, mod.File()+".part"), c.partial, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			ev := newEvents()
			m := New(Options{Dir: dir, BaseURL: srv.URL, Catalog: []Model{mod}, Publish: ev.publish,
				RetryDelay: time.Millisecond})
			defer m.Close()

			lm, err := m.Download(t.Context(), mod.ID)
			if err != nil || lm.Status != api.LocalModelStatusDownloading {
				t.Fatalf("Download = %+v, %v", lm, err)
			}
			got := ev.wait(t, mod.ID)
			if got.Status != c.wantStatus {
				t.Fatalf("final state %+v, want %s", got, c.wantStatus)
			}
			if c.wantCode != "" && (got.Error == nil || got.Error.Code != c.wantCode) {
				t.Fatalf("error = %+v, want %s", got.Error, c.wantCode)
			}
			if n := fs.requests.Load(); n != c.wantRequests {
				t.Errorf("%d requests, want %d", n, c.wantRequests)
			}
			fs.mu.Lock()
			if first := fs.ranges[0]; first != c.wantRange {
				t.Errorf("first Range = %q, want %q", first, c.wantRange)
			}
			fs.mu.Unlock()

			final := filepath.Join(dir, mod.File())
			b, err := os.ReadFile(final)
			if c.wantStatus == api.LocalModelStatusReady {
				if err != nil || !bytes.Equal(b, blob) {
					t.Fatalf("model file: %d bytes, %v", len(b), err)
				}
				if _, err := os.Stat(final + ".part"); !errors.Is(err, os.ErrNotExist) {
					t.Errorf(".part left behind: %v", err)
				}
			} else if err == nil {
				t.Errorf("a failed download left %s", final)
			}

			// The state stays in the list, and progress never goes back.
			list, err := m.List(t.Context())
			if err != nil || len(list) != 1 || list[0].Status != c.wantStatus {
				t.Fatalf("List = %+v, %v", list, err)
			}
			ev.mu.Lock()
			defer ev.mu.Unlock()
			var last float32 = -1
			for _, e := range ev.all {
				if e.Status == api.LocalModelStatusDownloading && e.Progress != nil {
					if *e.Progress < last && c.hook == nil {
						t.Errorf("progress went back from %v to %v", last, *e.Progress)
					}
					last = *e.Progress
				}
			}
		})
	}
}

func TestDownloadReadyAndUnknown(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer srv.Close()
	dir := t.TempDir()
	mod := testModel(blobSHA())
	if err := os.WriteFile(filepath.Join(dir, mod.File()), blob, 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Dir: dir, BaseURL: srv.URL, Catalog: []Model{mod}})
	defer m.Close()
	lm, err := m.Download(t.Context(), mod.ID)
	if err != nil || lm.Status != api.LocalModelStatusReady {
		t.Fatalf("Download of a present model = %+v, %v", lm, err)
	}
	if _, err := m.Download(t.Context(), "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown model: %v", err)
	}
	if requests.Load() != 0 {
		t.Errorf("%d requests for a present model", requests.Load())
	}
}

func TestDownloadIsIdempotentWhileRunning(t *testing.T) {
	release := make(chan struct{})
	fs := &fileServer{hook: func(_ int, _ http.ResponseWriter, _ *http.Request) bool {
		<-release
		return false
	}}
	srv := httptest.NewServer(fs)
	defer srv.Close()
	ev := newEvents()
	mod := testModel(blobSHA())
	m := New(Options{Dir: t.TempDir(), BaseURL: srv.URL, Catalog: []Model{mod}, Publish: ev.publish})
	defer m.Close()
	for range 3 {
		lm, err := m.Download(t.Context(), mod.ID)
		if err != nil || lm.Status != api.LocalModelStatusDownloading {
			t.Fatalf("Download = %+v, %v", lm, err)
		}
	}
	close(release)
	if got := ev.wait(t, mod.ID); got.Status != api.LocalModelStatusReady {
		t.Fatalf("final %+v", got)
	}
	if n := fs.requests.Load(); n != 1 {
		t.Errorf("%d downloads started, want 1", n)
	}
}

func TestListWhisper(t *testing.T) {
	dir := t.TempDir()
	ready := Model{ID: "whisper-a", Kind: api.Whisper, Name: "a", Size: 4}
	partial := Model{ID: "whisper-b", Kind: api.Whisper, Name: "b", Size: 10}
	wrongSize := Model{ID: "whisper-c", Kind: api.Whisper, Name: "c", Size: 10}
	missing := Model{ID: "whisper-d", Kind: api.Whisper, Name: "d", Size: 10}
	for name, data := range map[string]string{
		"ggml-a.bin": "abcd", "ggml-b.bin.part": "abcd", "ggml-c.bin": "abc",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(Options{Dir: dir, Catalog: []Model{ready, partial, wrongSize, missing},
		Recommended: func(context.Context) (string, string) { return "b", "gemma3:4b" }})
	defer m.Close()
	list, err := m.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		status      api.LocalModelStatus
		progress    float32 // -1: none
		recommended bool
	}{
		{api.LocalModelStatusReady, -1, false},
		{api.LocalModelStatusMissing, 0.4, true},
		{api.LocalModelStatusMissing, -1, false},
		{api.LocalModelStatusMissing, -1, false},
	} {
		got := list[i]
		if got.Status != want.status || *got.Recommended != want.recommended ||
			(want.progress < 0) != (got.Progress == nil) || (got.Progress != nil && *got.Progress != want.progress) {
			t.Errorf("%s = %+v (progress %v), want %+v", got.Id, got, got.Progress, want)
		}
	}
}

// fakeOllama serves /api/tags and /api/pull.
type fakeOllama struct {
	mu     sync.Mutex
	have   []string
	pull   []string // NDJSON lines of a pull
	pulled string   // the model of the last pull
}

func (o *fakeOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch r.URL.Path {
	case "/api/tags":
		var tags struct {
			Models []map[string]string `json:"models"`
		}
		for _, h := range o.have {
			tags.Models = append(tags.Models, map[string]string{"name": h, "model": h})
		}
		_ = json.NewEncoder(w).Encode(tags)
	case "/api/pull":
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		o.pulled = req.Model
		for _, l := range o.pull {
			_, _ = fmt.Fprintln(w, l)
		}
	default:
		http.NotFound(w, r)
	}
}

func TestGemma(t *testing.T) {
	progressLines := []string{
		`{"status":"pulling manifest"}`,
		`{"status":"pulling aaa","digest":"sha256:aaa","total":300,"completed":0}`,
		`{"status":"pulling aaa","digest":"sha256:aaa","total":300,"completed":150}`,
		`{"status":"pulling bbb","digest":"sha256:bbb","total":100,"completed":100}`,
		`{"status":"pulling aaa","digest":"sha256:aaa","total":300,"completed":300}`,
		`{"status":"verifying sha256 digest"}`,
		`{"status":"writing manifest"}`,
	}
	for _, c := range []struct {
		name       string
		have       []string
		pull       []string
		down       bool
		wantStart  api.LocalModelStatus // Download's answer
		wantErr    string               // Download's error code
		wantFinal  api.LocalModelStatus // "" when no download runs
		wantCode   string
		wantEvents []api.LocalModelStatus
	}{
		{name: "pull", have: []string{"gemma4:26b"}, pull: append(progressLines, `{"status":"success"}`),
			wantStart: api.LocalModelStatusDownloading, wantFinal: api.LocalModelStatusReady,
			wantEvents: []api.LocalModelStatus{api.LocalModelStatusVerifying, api.LocalModelStatusReady}},
		{name: "already pulled", have: []string{"gemma3:4b"}, wantStart: api.LocalModelStatusReady},
		{name: "pull error", pull: []string{`{"status":"pulling manifest"}`, `{"error":"pull model manifest: file does not exist"}`},
			wantStart: api.LocalModelStatusDownloading, wantFinal: api.LocalModelStatusError, wantCode: CodePullFailed},
		{name: "stream ends early", pull: progressLines[:3],
			wantStart: api.LocalModelStatusDownloading, wantFinal: api.LocalModelStatusError, wantCode: CodePullFailed},
		{name: "ollama down", down: true, wantErr: CodeOllamaUnreachable},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := &fakeOllama{have: c.have, pull: c.pull}
			srv := httptest.NewServer(o)
			url := srv.URL
			if c.down {
				srv.Close()
			} else {
				defer srv.Close()
			}
			settings := func(context.Context) (api.Settings, error) {
				var s api.Settings
				s.Providers.Local.OllamaUrl = url + "/"
				return s, nil
			}
			ev := newEvents()
			mod := Model{ID: "gemma3-4b", Kind: api.Gemma, Name: "gemma3:4b", Size: 400}
			m := New(Options{Dir: t.TempDir(), Settings: settings, Catalog: []Model{mod}, Publish: ev.publish})
			defer m.Close()

			lm, err := m.Download(t.Context(), mod.ID)
			if c.wantErr != "" {
				var ce *domain.CodedError
				if !errors.As(err, &ce) || ce.Code != c.wantErr {
					t.Fatalf("Download err = %v, want %s", err, c.wantErr)
				}
				list, _ := m.List(t.Context())
				if list[0].Status != api.LocalModelStatusError || list[0].Error.Code != CodeOllamaUnreachable {
					t.Errorf("List = %+v", list[0])
				}
				return
			}
			if err != nil || lm.Status != c.wantStart {
				t.Fatalf("Download = %+v, %v; want %s", lm, err, c.wantStart)
			}
			if c.wantFinal == "" {
				return
			}
			got := ev.wait(t, mod.ID)
			if got.Status != c.wantFinal || (c.wantCode != "" && got.Error.Code != c.wantCode) {
				t.Fatalf("final = %+v (error %+v)", got, got.Error)
			}
			if o.pulled != "gemma3:4b" {
				t.Errorf("pulled %q", o.pulled)
			}
			if c.wantFinal == api.LocalModelStatusReady {
				ev.mu.Lock()
				var statuses []api.LocalModelStatus
				var maxProgress float32
				for _, e := range ev.all {
					if len(statuses) == 0 || statuses[len(statuses)-1] != e.Status {
						statuses = append(statuses, e.Status)
					}
					if e.Status == api.LocalModelStatusDownloading && *e.Progress > maxProgress {
						maxProgress = *e.Progress
					}
				}
				ev.mu.Unlock()
				if maxProgress != 1 {
					t.Errorf("max progress %v, want 1 (400 of 400 bytes over two layers)", maxProgress)
				}
				tail := statuses[len(statuses)-2:]
				if tail[0] != c.wantEvents[0] || tail[1] != c.wantEvents[1] {
					t.Errorf("statuses %v, want to end with %v", statuses, c.wantEvents)
				}
			}
		})
	}
}

func TestRangeStart(t *testing.T) {
	for _, c := range []struct {
		h    string
		want int64
		ok   bool
	}{
		{"bytes 100-199/200", 100, true},
		{"bytes 0-0/1", 0, true},
		{"bytes */200", 0, false},
		{"", 0, false},
	} {
		if got, ok := rangeStart(c.h); got != c.want || ok != c.ok {
			t.Errorf("rangeStart(%q) = %d, %v", c.h, got, ok)
		}
	}
}

func TestCatalog(t *testing.T) {
	ids := map[string]bool{}
	for _, m := range Catalog {
		if ids[m.ID] {
			t.Errorf("duplicate id %s", m.ID)
		}
		ids[m.ID] = true
		if m.Kind == api.Whisper && (len(m.SHA256) != 64 || m.Size <= 0) {
			t.Errorf("%s: whisper models need a SHA-256 and a size", m.ID)
		}
	}
}
