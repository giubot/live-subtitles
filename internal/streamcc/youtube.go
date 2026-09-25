// SPDX-License-Identifier: Apache-2.0

package streamcc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// youtubeTimeLayout is the timestamp format of YouTube's caption ingestion:
// UTC with milliseconds, no zone suffix.
const youtubeTimeLayout = "2006-01-02T15:04:05.000"

// ValidateURL checks that raw can be a caption ingestion URL: absolute
// http or https with a host. It returns ErrInvalidURL otherwise.
func ValidateURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidURL
	}
	return nil
}

// youtube posts cues to a YouTube caption ingestion URL (Route A, CC-1).
//
// Each POST carries the cues of one item: a timestamp line
// (YYYY-MM-DDTHH:MM:SS.mmm, UTC, on YouTube's clock) followed by the cue
// text, lines joined with <br>. The URL gets an increasing `seq`. YouTube
// answers with its own time, from which the clock offset is updated.
type youtube struct {
	client *http.Client
	clock  domain.Clock

	mu     sync.Mutex
	offset time.Duration // local clock minus YouTube's
	known  bool          // offset has been measured
}

// deliveryError is a failed delivery, with a translatable code and
// whether trying again may help. Its message never contains the URL.
type deliveryError struct {
	code      string
	message   string
	params    map[string]any
	retryable bool
}

func (e *deliveryError) Error() string { return e.message }

// post sends it to rawURL with seq. The error is a *deliveryError.
func (y *youtube) post(ctx context.Context, rawURL string, seq int64, it item) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return &deliveryError{code: CodeInvalidURL, message: "the caption ingestion URL is invalid"}
	}
	q := u.Query()
	q.Set("seq", strconv.FormatInt(seq, 10))
	u.RawQuery = q.Encode()

	body := y.body(it)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return &deliveryError{code: CodeInvalidURL, message: "the caption ingestion URL is invalid"}
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	sent := y.clock.Now()
	resp, err := y.client.Do(req)
	if err != nil {
		// *url.Error repeats the URL, which is a secret: keep only the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return &deliveryError{code: CodeUnreachable, message: "YouTube caption ingestion unreachable: " + err.Error(), retryable: true}
	}
	defer func() { _ = resp.Body.Close() }()
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	got := y.clock.Now()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		y.measure(strings.TrimSpace(string(reply)), sent, got)
		return nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500:
		return &deliveryError{code: CodeServerError, message: fmt.Sprintf("YouTube answered HTTP %d", resp.StatusCode),
			params: map[string]any{"status": resp.StatusCode}, retryable: true}
	default:
		return &deliveryError{code: CodeRejected, message: fmt.Sprintf("YouTube rejected the caption: HTTP %d", resp.StatusCode),
			params: map[string]any{"status": resp.StatusCode}}
	}
}

// body renders the cues of it, their times moved onto YouTube's clock.
func (y *youtube) body(it item) []byte {
	off := y.clockOffset()
	var b bytes.Buffer
	for _, c := range it.cues {
		b.WriteString(c.at.Add(-off).UTC().Format(youtubeTimeLayout))
		b.WriteByte('\n')
		b.WriteString(strings.Join(c.lines, "<br>"))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// measure updates the clock offset from YouTube's reply, a timestamp in
// youtubeTimeLayout, taken as the midpoint of the request.
func (y *youtube) measure(reply string, sent, got time.Time) {
	server, err := time.Parse(youtubeTimeLayout, reply)
	if err != nil {
		if server, err = time.Parse(time.RFC3339Nano, reply); err != nil {
			return // an empty or unexpected reply: keep the last offset
		}
	}
	mid := sent.Add(got.Sub(sent) / 2)
	y.mu.Lock()
	y.offset, y.known = mid.Sub(server), true
	y.mu.Unlock()
}

func (y *youtube) clockOffset() time.Duration {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.offset
}

// measuredOffset is the clock offset when one has been measured.
func (y *youtube) measuredOffset() (time.Duration, bool) {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.offset, y.known
}
