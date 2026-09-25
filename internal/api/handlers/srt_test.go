// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/audio/srt"
	"github.com/iencodev/live-subtitles/internal/bus"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
	"github.com/iencodev/live-subtitles/internal/session"
	"github.com/iencodev/live-subtitles/internal/store"
)

// srtServer serves one mock-provider session, "main", with SRT ingest on
// port (settings.srt.port) through ffmpeg binary.
func srtServer(t *testing.T, binary string, port int) (http.Handler, *Server) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now()
	if err := st.CreateSession(t.Context(), domain.Session{Id: "main", CreatedAt: now, UpdatedAt: now,
		Provider: api.ProviderChoiceMock, SourceLanguage: api.En, TargetLanguages: []string{"es"}}); err != nil {
		t.Fatal(err)
	}
	var settings api.Settings
	if err := json.Unmarshal([]byte(`{"srt":{"enabled":true,"port":`+strconv.Itoa(port)+`,"latencyMs":120}}`), &settings); err != nil {
		t.Fatal(err)
	}
	if err := st.PutSettings(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
	m := session.New(session.Options{
		Sessions: st, Captions: st, Bus: bus.New(), Logger: slog.New(slog.DiscardHandler),
		Providers: map[domain.ProviderKind]session.Provider{
			api.ProviderKindMock: {ASR: &mock.ASR{}, Translator: &mock.Translator{}},
		},
		IngestSource:   func(string) domain.AudioSource { return &fake.Source{Realtime: true} },
		StatusInterval: 50 * time.Millisecond,
	})
	t.Cleanup(m.Close)
	s := New()
	s.Manager, s.Sessions, s.Settings = m, st, st
	s.Network = func() api.NetworkInfo { return api.NetworkInfo{ViewerBaseUrl: "http://192.168.1.20:8080"} }
	s.SRT = srt.New(srt.Options{Binary: binary, Settings: st, Logger: slog.New(slog.DiscardHandler)})
	return s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler)), s
}

func TestSRTUnavailable(t *testing.T) {
	h, _ := srtServer(t, filepath.Join(t.TempDir(), "no-ffmpeg"), 9000)
	res := call{"GET", "/api/sessions/main", "", "", "", 200, `"id":"main"`}.do(t, h)
	var sess api.Session
	if err := json.NewDecoder(res.Body).Decode(&sess); err != nil || sess.Urls.SrtIngest != nil {
		t.Errorf("srtIngest %v without libsrt (%v)", sess.Urls.SrtIngest, err)
	}
	for _, c := range []call{
		{"POST", "/api/sessions/nope/start", `{"source":"srt"}`, "", "", 404, `"code":"session.not_found"`},
		{"POST", "/api/sessions/main/start", `{"source":"srt"}`, "", "", 422, `"code":"source.srt_unavailable"`},
		{"GET", "/api/sessions/main/status", "", "", "", 200, `"state":"idle"`},
	} {
		c.do(t, h)
	}
}

// TestSRTSession pushes the fixture to a session over SRT twice, as an
// encoder that reconnects, and needs ffmpeg with libsrt.
func TestSRTSession(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	_ = c.Close()
	h, s := srtServer(t, "ffmpeg", port)
	if !s.SRT.Available(t.Context()) {
		t.Skip("ffmpeg without libsrt")
	}
	want := "srt://192.168.1.20:" + strconv.Itoa(port) + "?streamid=main"
	call{"GET", "/api/sessions/main", "", "", "", 200, `"srtIngest":"` + want + `"`}.do(t, h)
	call{"POST", "/api/sessions/main/start", `{"source":"srt"}`, "", "", 200, `"srt":{"connected":false}`}.do(t, h)
	call{"POST", "/api/sessions/main/start", `{"source":"srt"}`, "", "", 409, `"code":"session.state_conflict"`}.do(t, h)

	fixture, _ := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "audio", "fixtures", "en.wav"))
	for round := range 2 {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		sent := make(chan error, 1)
		go func() {
			sent <- exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-re",
				"-i", fixture, "-t", "3", "-c:a", "aac", "-f", "mpegts", "srt://127.0.0.1:"+strconv.Itoa(port)+"?streamid=main").Run()
		}()
		connected := false
		for !connected {
			select {
			case err := <-sent:
				cancel()
				t.Fatalf("round %d: sender ended (%v) before the session saw it connected", round, err)
			case <-time.After(100 * time.Millisecond):
			}
			st := s.Manager.StatusOf("main")
			connected = st.Srt != nil && st.Srt.Connected != nil && *st.Srt.Connected &&
				st.Audio != nil && st.Audio.Connected && *st.Audio.Source == api.AudioSourceKindSrt
		}
		if err := <-sent; err != nil {
			t.Fatalf("round %d: send: %v", round, err)
		}
		cancel()
		// The session stays live while the listener waits for the next sender.
		deadline := time.Now().Add(3 * time.Second)
		for st := s.Manager.StatusOf("main"); *st.Srt.Connected; st = s.Manager.StatusOf("main") {
			if time.Now().After(deadline) {
				t.Fatalf("round %d: still connected after the sender left", round)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if st := s.Manager.StatusOf("main"); st.State != api.SessionStateLive {
			t.Fatalf("round %d: state %s after the sender left: %+v", round, st.State, st.Error)
		}
	}
	res := call{"POST", "/api/sessions/main/stop", "", "", "", 200, `"state":"idle"`}.do(t, h)
	var st api.SessionStatus
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Usage == nil || st.Usage.AudioSeconds == nil || *st.Usage.AudioSeconds < 3 {
		t.Errorf("usage %+v: the audio of both senders didn't reach the provider", st.Usage)
	}
	if strings.Contains(want, "passphrase") {
		t.Error("the ingest URL carries the passphrase")
	}
}
