// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/config"
)

const acceptToken = "acceptance-admin-token-0123"

// The backend half of the requirements §8 demo checklist (P4-06) over real
// HTTP and WebSockets, with ffmpeg: two sessions run at once from two audio
// files on the mock provider; viewers get the source, ES and EN tracks; the
// VTT and SRT exports parse; each run leaves a recording served with HTTP
// Range; and after a restart on the same data directory the sessions,
// captions and recordings are still there and a new run continues the
// session clock past the earlier recording.
func TestAcceptanceTwoSessionsExportsReplayRestart(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	dir := t.TempDir()
	for _, f := range []string{"en.wav", "es.wav"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", "fixtures", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{Addr: "127.0.0.1:0", DataDir: dir, NoKeychain: true, AdminToken: acceptToken, FFmpeg: ffmpeg}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	a, srv := startAcceptApp(t, ctx, cfg)
	c := acceptClient{t: t, ctx: ctx, base: srv.URL}
	sessions := map[string]string{"room-a": "en.wav", "room-b": "es.wav"}
	for id := range sessions {
		c.call("POST", "/api/sessions", fmt.Sprintf(`{"slug":%q,"name":%q,"provider":"mock"}`, id, id), 201, nil)
	}

	// Viewers on every track of both sessions, then both files at once.
	type got struct{ id, lang string }
	finals := make(chan got, 64)
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	for id := range sessions {
		v, _, err := websocket.Dial(ctx, ws+"/ws/captions/"+id+"?lang=source&lang=es&lang=en", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = v.CloseNow() }()
		v.SetReadLimit(1 << 20)
		go func() {
			for {
				_, b, err := v.Read(ctx)
				if err != nil {
					return
				}
				var m api.CaptionsServerMessage
				if json.Unmarshal(b, &m) == nil && m.Type == api.CaptionsServerMessageTypeCaption && m.Caption.Final {
					finals <- got{id, m.Caption.Lang}
				}
			}
		}()
	}
	for id, file := range sessions {
		c.call("POST", "/api/sessions/"+id+"/sources/file", fmt.Sprintf(`{"uri":%q}`, filepath.Join(dir, file)), 202, nil)
	}
	want := map[got]bool{}
	for id := range sessions {
		for _, l := range []string{"source", "es", "en"} {
			want[got{id, l}] = true
		}
	}
	for len(want) > 0 {
		select {
		case g := <-finals:
			delete(want, g)
		case <-ctx.Done():
			t.Fatalf("no final captions on %v", want)
		}
	}
	for id := range sessions {
		var st api.SessionStatus
		c.call("POST", "/api/sessions/"+id+"/stop", "", 200, &st)
		if st.State != api.SessionStateIdle {
			t.Errorf("%s after stop: %+v", id, st)
		}
	}

	// Exports: every track in VTT and SRT, with well-formed, ordered cues.
	for id := range sessions {
		for _, l := range []string{"source", "es", "en"} {
			checkCues(t, id+" "+l+".vtt", c.get("/api/public/sessions/"+id+"/subtitles?lang="+l+"&format=vtt", 200, "text/vtt"), true)
			checkCues(t, id+" "+l+".srt", c.get("/api/public/sessions/"+id+"/subtitles?lang="+l+"&format=srt", 200, "application/x-subrip"), false)
		}
	}

	// Replay: one complete recording per session, served with Range.
	recs := map[string]api.Recording{}
	for id := range sessions {
		var list []api.Recording
		c.call("GET", "/api/recordings?sessionId="+id, "", 200, &list)
		if len(list) != 1 || list[0].Status != api.RecordingStatusComplete || list[0].DurationSec == nil || *list[0].DurationSec <= 0 {
			t.Fatalf("%s recordings %+v", id, list)
		}
		recs[id] = list[0]
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/recordings/"+list[0].Id+"/audio", nil)
		req.Header.Set("Range", "bytes=0-99")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 206 || len(body) != 100 || res.Header.Get("Content-Type") != "audio/mp4" ||
			!bytes.Contains(body, []byte("ftyp")) {
			t.Errorf("%s audio range: %d %q, %d bytes", id, res.StatusCode, res.Header.Get("Content-Type"), len(body))
		}
		vtt := c.get("/api/public/sessions/"+id+"/subtitles?lang=es&format=vtt&recordingId="+list[0].Id, 200, "text/vtt")
		checkCues(t, id+" replay es.vtt", vtt, true)
	}

	// Restart on the same data directory.
	srv.Close()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	_, srv = startAcceptApp(t, ctx, cfg)
	c.base = srv.URL
	var list []api.Session
	c.call("GET", "/api/sessions", "", 200, &list)
	if len(list) != 2 {
		t.Fatalf("sessions after restart: %+v", list)
	}
	if vtt := c.get("/api/public/sessions/room-b/subtitles?lang=en&format=vtt", 200, "text/vtt"); !strings.Contains(vtt, "-->") {
		t.Errorf("captions lost in the restart:\n%s", vtt)
	}
	c.call("POST", "/api/sessions/room-a/sources/file", fmt.Sprintf(`{"uri":%q}`, filepath.Join(dir, "en.wav")), 202, nil)
	var second api.Recording
	for second.Id == "" || second.Id == recs["room-a"].Id {
		var l []api.Recording
		c.call("GET", "/api/recordings?sessionId=room-a", "", 200, &l)
		if len(l) > 0 {
			second = l[0]
		}
		if ctx.Err() != nil {
			t.Fatalf("no second recording: %+v", l)
		}
		time.Sleep(50 * time.Millisecond)
	}
	c.call("POST", "/api/sessions/room-a/stop", "", 200, nil)
	first := recs["room-a"]
	if end := *first.OffsetSec + *first.DurationSec; second.OffsetSec == nil || *second.OffsetSec < end {
		t.Errorf("run after the restart starts at %v s, inside the earlier recording (ends at %.2f s)", second.OffsetSec, end)
	}
}

func startAcceptApp(t *testing.T, ctx context.Context, cfg config.Config) (*App, *httptest.Server) {
	t.Helper()
	a, err := New(ctx, cfg, slog.New(slog.DiscardHandler), testDist, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.out = io.Discard
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(func() { srv.Close(); _ = a.Close() })
	return a, srv
}

type acceptClient struct {
	t    *testing.T
	ctx  context.Context
	base string
}

func (c acceptClient) call(method, path, body string, wantStatus int, out any) {
	c.t.Helper()
	req, _ := http.NewRequestWithContext(c.ctx, method, c.base+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+acceptToken)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		c.t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			c.t.Fatalf("%s %s: %v: %s", method, path, err, b)
		}
	}
}

func (c acceptClient) get(path string, wantStatus int, wantType string) string {
	c.t.Helper()
	res, err := http.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus || !strings.HasPrefix(res.Header.Get("Content-Type"), wantType) {
		c.t.Fatalf("GET %s: %d %q %s", path, res.StatusCode, res.Header.Get("Content-Type"), b)
	}
	return string(b)
}

var (
	vttTiming = regexp.MustCompile(`^(\d{2}):(\d{2}):(\d{2})\.(\d{3}) --> (\d{2}):(\d{2}):(\d{2})\.(\d{3})$`)
	srtTiming = regexp.MustCompile(`^(\d{2}):(\d{2}):(\d{2}),(\d{3}) --> (\d{2}):(\d{2}):(\d{2}),(\d{3})$`)
)

// checkCues parses a WebVTT or SubRip file: the header, numbered cues (SRT),
// timings that end after they start and don't go back, and text on each cue.
func checkCues(t *testing.T, name, file string, vtt bool) {
	t.Helper()
	sc := bufio.NewScanner(strings.NewReader(file))
	if vtt {
		if !sc.Scan() || sc.Text() != "WEBVTT" {
			t.Fatalf("%s: no WEBVTT header:\n%s", name, file)
		}
		sc.Scan() // blank line
	}
	timing := srtTiming
	if vtt {
		timing = vttTiming
	}
	ms := func(m []string, i int) int {
		var h, mi, s, f int
		_, _ = fmt.Sscan(m[i], &h)
		_, _ = fmt.Sscan(m[i+1], &mi)
		_, _ = fmt.Sscan(m[i+2], &s)
		_, _ = fmt.Sscan(m[i+3], &f)
		return ((h*60+mi)*60+s)*1000 + f
	}
	cues, last := 0, -1
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if !vtt {
			if line != fmt.Sprint(cues+1) {
				t.Fatalf("%s: cue %d numbered %q", name, cues+1, line)
			}
			sc.Scan()
			line = sc.Text()
		}
		m := timing.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("%s: bad timing line %q", name, line)
		}
		start, end := ms(m, 1), ms(m, 5)
		if end <= start || start < last {
			t.Errorf("%s: cue %d %q ends before it starts or goes back", name, cues+1, line)
		}
		last = start
		text := 0
		for sc.Scan() && sc.Text() != "" {
			text++
		}
		if text == 0 || text > 2 {
			t.Errorf("%s: cue %d has %d lines", name, cues+1, text)
		}
		cues++
	}
	if cues == 0 {
		t.Errorf("%s: no cues:\n%s", name, file)
	}
}
