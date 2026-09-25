// SPDX-License-Identifier: Apache-2.0

package streamcc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/secrets"
)

// OBS defaults (Route B, CC-4).
const (
	// DefaultOBSURL is obs-websocket's default address.
	DefaultOBSURL = "ws://127.0.0.1:4455"
	// OBSMaxChars is the CEA-608 line limit OBS encodes captions into.
	OBSMaxChars = 32
	// DefaultOBSMinGap and DefaultOBSMaxGap bound how long one OBS caption
	// stays up before the next is sent.
	DefaultOBSMinGap = 1500 * time.Millisecond
	DefaultOBSMaxGap = 4 * time.Second
	// obsSubprotocol is obs-websocket v5's JSON encoding.
	obsSubprotocol = "obswebsocket.json"
	// obsAuthFailed is the close code obs-websocket sends for a bad password.
	obsAuthFailed websocket.StatusCode = 4009
)

// obs sends captions to OBS with obs-websocket v5's SendStreamCaption; OBS
// encodes them as CEA-608 into its stream (Route B, CC-4).
//
// It's a small hand-rolled client of the three messages it needs (Hello,
// Identify, Request) over coder/websocket, which the server already uses.
// The connection is opened on the first caption and reopened on the next
// one after it drops, so OBS can restart while a session runs.
type obs struct {
	url      func(ctx context.Context) string
	password func(ctx context.Context) (string, error)
	timeout  time.Duration
	minGap   time.Duration
	maxGap   time.Duration

	mu     sync.Mutex // one caption at a time; guards the fields below
	conn   *obsConn
	nextID int
	next   time.Time // when the next caption may replace the one on screen
}

// obsMessage is the obs-websocket v5 envelope.
type obsMessage struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
}

const (
	opHello       = 0
	opIdentify    = 1
	opIdentified  = 2
	opRequest     = 6
	opRequestResp = 7
)

type obsHello struct {
	RPCVersion     int `json:"rpcVersion"`
	Authentication *struct {
		Challenge string `json:"challenge"`
		Salt      string `json:"salt"`
	} `json:"authentication"`
}

type obsResponse struct {
	RequestID     string `json:"requestId"`
	RequestStatus struct {
		Result  bool   `json:"result"`
		Code    int    `json:"code"`
		Comment string `json:"comment"`
	} `json:"requestStatus"`
}

// obsConn is an identified connection with a reader that hands request
// responses over and answers pings.
type obsConn struct {
	c         *websocket.Conn
	cancel    context.CancelFunc
	responses chan obsResponse
	done      chan struct{}
	err       error // why the reader stopped; read after done
}

func (o *obs) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.drop()
}

// drop closes the connection. Called with mu held.
func (o *obs) drop() {
	if o.conn != nil {
		o.conn.cancel()
		_ = o.conn.c.CloseNow()
		o.conn = nil
	}
}

// send shows the cues of it one after the other, each for at least minGap
// and following the speech up to maxGap. Cues already shown (it.done) are
// skipped, so a retry doesn't repeat them. The error is a *deliveryError.
func (o *obs) send(ctx context.Context, it *item) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for it.done < len(it.cues) {
		c := it.cues[it.done]
		if wait := time.Until(o.next); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return &deliveryError{code: CodeOBSUnreachable, message: "cancelled", retryable: true}
			}
		}
		if err := o.caption(ctx, strings.Join(c.lines, "\n")); err != nil {
			return err
		}
		gap := o.minGap
		if it.done+1 < len(it.cues) {
			gap = min(max(it.cues[it.done+1].at.Sub(c.at), o.minGap), o.maxGap)
		}
		o.next = time.Now().Add(gap)
		it.done++
	}
	return nil
}

// caption sends one SendStreamCaption request. Called with mu held.
func (o *obs) caption(ctx context.Context, text string) error {
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	if o.conn == nil {
		conn, err := o.connect(ctx)
		if err != nil {
			return err
		}
		o.conn = conn
	}
	o.nextID++
	id := strconv.Itoa(o.nextID)
	d, _ := json.Marshal(map[string]any{
		"requestType": "SendStreamCaption",
		"requestId":   id,
		"requestData": map[string]string{"captionText": text},
	})
	if err := wsjson.Write(ctx, o.conn.c, obsMessage{Op: opRequest, D: d}); err != nil {
		o.drop()
		return unreachable(err)
	}
	for {
		select {
		case r := <-o.conn.responses:
			if r.RequestID != id {
				continue // a late answer to an earlier, timed-out request
			}
			if !r.RequestStatus.Result {
				return &deliveryError{code: CodeOBSRejected,
					message: fmt.Sprintf("OBS refused the caption: %d %s", r.RequestStatus.Code, r.RequestStatus.Comment),
					params:  map[string]any{"status": r.RequestStatus.Code, "comment": r.RequestStatus.Comment}}
			}
			return nil
		case <-o.conn.done:
			err := o.conn.err
			o.drop()
			return unreachable(err)
		case <-ctx.Done():
			o.drop()
			return unreachable(ctx.Err())
		}
	}
}

func unreachable(err error) *deliveryError {
	return &deliveryError{code: CodeOBSUnreachable, message: "OBS websocket unreachable: " + err.Error(), retryable: true}
}

// connect dials OBS and identifies: Hello → Identify (with the password
// when OBS asks for one) → Identified.
func (o *obs) connect(ctx context.Context) (*obsConn, error) {
	url := o.url(ctx)
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{obsSubprotocol}})
	if err != nil {
		return nil, unreachable(err)
	}
	c.SetReadLimit(1 << 20)
	fail := func(err error) (*obsConn, error) {
		_ = c.CloseNow()
		if websocket.CloseStatus(err) == obsAuthFailed {
			return nil, &deliveryError{code: CodeOBSAuthFailed, message: "OBS rejected the websocket password"}
		}
		var de *deliveryError
		if errors.As(err, &de) {
			return nil, de
		}
		return nil, unreachable(err)
	}

	var m obsMessage
	if err := wsjson.Read(ctx, c, &m); err != nil {
		return fail(err)
	}
	var hello obsHello
	if m.Op != opHello || json.Unmarshal(m.D, &hello) != nil {
		return fail(errors.New("unexpected first message"))
	}
	identify := map[string]any{"rpcVersion": 1, "eventSubscriptions": 0}
	if a := hello.Authentication; a != nil {
		pw, err := o.password(ctx)
		if err != nil {
			return fail(err)
		}
		identify["authentication"] = obsAuth(pw, a.Salt, a.Challenge)
	}
	d, _ := json.Marshal(identify)
	if err := wsjson.Write(ctx, c, obsMessage{Op: opIdentify, D: d}); err != nil {
		return fail(err)
	}
	if err := wsjson.Read(ctx, c, &m); err != nil {
		return fail(err)
	}
	if m.Op != opIdentified {
		return fail(fmt.Errorf("unexpected message op %d while identifying", m.Op))
	}

	rctx, cancel := context.WithCancel(context.Background())
	conn := &obsConn{c: c, cancel: cancel, responses: make(chan obsResponse, 8), done: make(chan struct{})}
	go conn.read(rctx)
	return conn, nil
}

// read hands request responses over until the connection ends. Reading
// also answers OBS's pings.
func (c *obsConn) read(ctx context.Context) {
	defer close(c.done)
	for {
		var m obsMessage
		if err := wsjson.Read(ctx, c.c, &m); err != nil {
			c.err = err
			return
		}
		if m.Op != opRequestResp {
			continue // events: none are subscribed
		}
		var r obsResponse
		if json.Unmarshal(m.D, &r) != nil {
			continue
		}
		select {
		case c.responses <- r:
		default: // nobody waits for it
		}
	}
}

// obsAuth is obs-websocket v5's authentication string:
// base64(sha256(base64(sha256(password + salt)) + challenge)).
func obsAuth(password, salt, challenge string) string {
	h := sha256.Sum256([]byte(password + salt))
	secret := base64.StdEncoding.EncodeToString(h[:])
	h = sha256.Sum256([]byte(secret + challenge))
	return base64.StdEncoding.EncodeToString(h[:])
}

// newOBS builds the OBS target from the settings (obs.websocketUrl) and
// the obs_websocket_password secret, both read at each connection.
func newOBS(opts Options) *obs {
	return &obs{
		url: func(ctx context.Context) string {
			if opts.Settings != nil {
				if s, err := opts.Settings.Settings(ctx); err == nil && s.Obs != nil && s.Obs.WebsocketUrl != nil && *s.Obs.WebsocketUrl != "" {
					return *s.Obs.WebsocketUrl
				}
			}
			return DefaultOBSURL
		},
		password: func(ctx context.Context) (string, error) {
			v, _, err := opts.Secrets.GetSecret(ctx, string(api.ObsWebsocketPassword))
			if errors.Is(err, secrets.ErrNotFound) {
				return "", &deliveryError{code: CodeOBSAuthFailed, message: "OBS asks for a password and obs_websocket_password is not set"}
			}
			return v, err
		},
		timeout: DefaultTimeout,
		minGap:  opts.OBSMinGap,
		maxGap:  opts.OBSMaxGap,
	}
}
