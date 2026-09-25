// SPDX-License-Identifier: Apache-2.0

package streamcc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fakeOBS is an obs-websocket v5 server that records SendStreamCaption.
type fakeOBS struct {
	t        *testing.T
	srv      *httptest.Server
	password string // "": no authentication
	reject   bool   // answer result=false (OBS not streaming)

	mu          sync.Mutex
	captions    []obsCaption
	requests    int
	connections int
	dropOn      map[int]bool // n-th request (1-based) closes the connection unanswered
}

type obsCaption struct {
	text string
	at   time.Time
}

const obsSalt, obsChallenge = "c2FsdA==", "Y2hhbGxlbmdl"

func newFakeOBS(t *testing.T, password string) *fakeOBS {
	f := &fakeOBS{t: t, password: password, dropOn: map[int]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOBS) url() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") }

func (f *fakeOBS) serve(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{obsSubprotocol}})
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	if c.Subprotocol() != obsSubprotocol {
		f.t.Errorf("subprotocol %q, want %s", c.Subprotocol(), obsSubprotocol)
	}
	f.mu.Lock()
	f.connections++
	f.mu.Unlock()
	ctx := r.Context()
	hello := map[string]any{"obsWebSocketVersion": "5.5.0", "rpcVersion": 1}
	if f.password != "" {
		hello["authentication"] = map[string]string{"challenge": obsChallenge, "salt": obsSalt}
	}
	if f.write(ctx, c, opHello, hello) != nil {
		return
	}
	var m obsMessage
	if wsjson.Read(ctx, c, &m) != nil || m.Op != opIdentify {
		return
	}
	var id struct {
		RPCVersion     int    `json:"rpcVersion"`
		Authentication string `json:"authentication"`
	}
	_ = json.Unmarshal(m.D, &id)
	if f.password != "" && id.Authentication != obsAuth(f.password, obsSalt, obsChallenge) {
		_ = c.Close(obsAuthFailed, "Authentication failed.")
		return
	}
	if f.write(ctx, c, opIdentified, map[string]int{"negotiatedRpcVersion": 1}) != nil {
		return
	}
	for {
		if wsjson.Read(ctx, c, &m) != nil {
			return
		}
		var req struct {
			RequestType string `json:"requestType"`
			RequestID   string `json:"requestId"`
			RequestData struct {
				CaptionText string `json:"captionText"`
			} `json:"requestData"`
		}
		_ = json.Unmarshal(m.D, &req)
		f.mu.Lock()
		f.requests++
		drop := f.dropOn[f.requests]
		if !drop && !f.reject {
			f.captions = append(f.captions, obsCaption{req.RequestData.CaptionText, time.Now()})
		}
		f.mu.Unlock()
		if req.RequestType != "SendStreamCaption" {
			f.t.Errorf("request %q, want SendStreamCaption", req.RequestType)
		}
		if drop {
			return // OBS quits
		}
		status := map[string]any{"result": true, "code": 100}
		if f.reject {
			status = map[string]any{"result": false, "code": 501, "comment": "Output not running"}
		}
		if f.write(ctx, c, opRequestResp, map[string]any{"requestType": req.RequestType, "requestId": req.RequestID, "requestStatus": status}) != nil {
			return
		}
	}
}

func (f *fakeOBS) write(ctx context.Context, c *websocket.Conn, op int, d any) error {
	b, _ := json.Marshal(d)
	return wsjson.Write(ctx, c, obsMessage{Op: op, D: b})
}

func (f *fakeOBS) got() ([]obsCaption, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]obsCaption(nil), f.captions...), f.connections
}

// obsSettings gives obs.websocketUrl.
type obsSettings struct{ url string }

func (s obsSettings) Settings(context.Context) (api.Settings, error) {
	return api.Settings{Obs: &struct {
		WebsocketUrl *string `json:"websocketUrl,omitempty"`
	}{WebsocketUrl: &s.url}}, nil
}
func (obsSettings) PutSettings(context.Context, api.Settings) error { return nil }

func newOBSHarness(t *testing.T, obsPassword, secret string) (*harness, *fakeOBS) {
	f := newFakeOBS(t, obsPassword)
	h := newHarness(t, false, Options{Settings: obsSettings{f.url()}, OBSMinGap: 30 * time.Millisecond, OBSMaxGap: 60 * time.Millisecond})
	if secret != "" {
		h.sec.values[string(api.ObsWebsocketPassword)] = secret
	}
	return h, f
}

func obsSession(maxChars int) domain.Session {
	s := enabledSession("es")
	target := api.ObsWebsocket
	s.StreamCaptions.Target, s.StreamCaptions.MaxCharsPerLine = &target, &maxChars
	return s
}

func TestOBSAuthentication(t *testing.T) {
	tests := []struct {
		name        string
		obsPassword string
		secret      string
		wantState   api.StreamCaptionStatusState
		wantCode    string
	}{
		{"no password", "", "", api.StreamCaptionStatusStateOk, ""},
		{"password", "hunter22", "hunter22", api.StreamCaptionStatusStateOk, ""},
		{"wrong password", "hunter22", "hunter23", api.StreamCaptionStatusStateError, CodeOBSAuthFailed},
		{"password not stored", "hunter22", "", api.StreamCaptionStatusStateError, CodeOBSAuthFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, f := newOBSHarness(t, tt.obsPassword, tt.secret)
			st, err := h.svc.SendTest(t.Context(), obsSession(32), "Probando")
			if err != nil {
				t.Fatal(err)
			}
			if st.State != tt.wantState || (tt.wantCode != "" && (st.Error == nil || st.Error.Code != tt.wantCode)) {
				t.Fatalf("status %+v (error %+v), want %s %s", st, st.Error, tt.wantState, tt.wantCode)
			}
			if st.Target == nil || *st.Target != api.ObsWebsocket {
				t.Errorf("target = %v, want obs_websocket", st.Target)
			}
			caps, _ := f.got()
			if tt.wantCode == "" && (len(caps) != 1 || caps[0].text != "Probando") {
				t.Errorf("OBS got %+v, want the test caption", caps)
			}
			if tt.secret != "" && strings.Contains(h.logText(), tt.secret) {
				t.Errorf("the log reveals the OBS password")
			}
		})
	}
}

func TestOBSSplitsAndPacesCaptions(t *testing.T) {
	h, f := newOBSHarness(t, "", "")
	h.svc.RunStarted(obsSession(42)) // more than 608 allows: 32 is used
	text := "¿Cómo están? Hoy vamos a hablar de observabilidad en Kubernetes, de métricas y de señales útiles."
	h.final("es", "s-1", text)
	waitFor(t, "every cue shown", func() bool {
		caps, _ := f.got()
		return len(caps) == 2
	})
	caps, _ := f.got()
	var words []string
	for i, c := range caps {
		lines := strings.Split(c.text, "\n")
		if len(lines) > LinesPerCue {
			t.Errorf("caption %d has %d lines", i, len(lines))
		}
		for _, l := range lines {
			if n := utf8.RuneCountInString(l); n > OBSMaxChars {
				t.Errorf("line %q has %d chars, more than %d", l, n, OBSMaxChars)
			}
			words = append(words, strings.Fields(l)...)
		}
		if i > 0 && c.at.Sub(caps[i-1].at) < 25*time.Millisecond {
			t.Errorf("caption %d came %v after the previous one, want at least the minimum gap", i, c.at.Sub(caps[i-1].at))
		}
	}
	if got := strings.Join(words, " "); got != text {
		t.Errorf("OBS showed %q, want %q", got, text)
	}
	st := h.waitState(t, api.StreamCaptionStatusStateOk)
	if st.LastSeq == nil || *st.LastSeq != 1 {
		t.Errorf("last seq = %v, want 1", st.LastSeq)
	}
}

func TestOBSReconnectsAfterRestart(t *testing.T) {
	h, f := newOBSHarness(t, "pw-123456", "pw-123456")
	f.mu.Lock()
	f.dropOn[2] = true // OBS quits while the second caption is sent
	f.mu.Unlock()
	h.svc.RunStarted(obsSession(32))
	for i, text := range []string{"primero", "segundo", "tercero"} {
		h.final("es", "s-"+string(rune('1'+i)), text)
	}
	waitFor(t, "all captions after the restart", func() bool {
		caps, _ := f.got()
		return len(caps) == 3
	})
	caps, conns := f.got()
	for i, want := range []string{"primero", "segundo", "tercero"} {
		if caps[i].text != want {
			t.Errorf("caption %d = %q, want %q", i, caps[i].text, want)
		}
	}
	if conns != 2 {
		t.Errorf("%d connections, want 2 (one reconnect)", conns)
	}
	h.waitState(t, api.StreamCaptionStatusStateOk)
	var retried bool
	for _, s := range h.events.states() {
		retried = retried || s == api.StreamCaptionStatusStateRetrying
	}
	if !retried {
		t.Errorf("states %v never showed retrying", h.events.states())
	}
}

func TestOBSRejectsWhenNotStreaming(t *testing.T) {
	h, f := newOBSHarness(t, "", "")
	f.reject = true
	st, err := h.svc.SendTest(t.Context(), obsSession(32), "")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != api.StreamCaptionStatusStateError || st.Error == nil || st.Error.Code != CodeOBSRejected {
		t.Fatalf("status %+v (error %+v), want error %s", st, st.Error, CodeOBSRejected)
	}
}

func TestOBSUnreachable(t *testing.T) {
	h, f := newOBSHarness(t, "", "")
	f.srv.Close()
	st, err := h.svc.SendTest(t.Context(), obsSession(32), "x")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != api.StreamCaptionStatusStateError || st.Error == nil || st.Error.Code != CodeOBSUnreachable {
		t.Fatalf("status %+v (error %+v), want error %s", st, st.Error, CodeOBSUnreachable)
	}
}

func TestOBSAuthString(t *testing.T) {
	// The example from the obs-websocket v5 protocol docs.
	got := obsAuth("supersecretpassword", "lM1GncleQOaCu9lT1yeUZhFYnqhsLLP1G5lAGo3ixaI=", "+IxH4CnCiqpX1rM9scsNynZzbOe4KhDeYcTNS3PDaeY=")
	if want := "1Ct943GAT+6YQUUX47Ia/ncufilbe6+oD6lY+5kaCu4="; got != want {
		t.Fatalf("obsAuth = %s, want %s", got, want)
	}
}
