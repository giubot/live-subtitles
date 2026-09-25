// SPDX-License-Identifier: Apache-2.0

package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// errRestart means the partial file can't be resumed and was emptied.
var errRestart = errors.New("restart the download from the beginning")

// fetchWhisper downloads mod into <Dir>/ggml-<name>.bin.part, resuming
// what is already there with an HTTP Range request, checks its SHA-256
// and renames it into place. Network failures are retried.
func (m *Manager) fetchWhisper(ctx context.Context, mod Model) error {
	if err := os.MkdirAll(m.opts.Dir, 0o755); err != nil {
		return err
	}
	part := m.partPath(mod)
	var err error
	for attempt := 1; attempt <= m.opts.Attempts; attempt++ {
		if attempt > 1 {
			m.opts.Logger.Warn("model download interrupted; resuming", "model", mod.ID, "attempt", attempt, "err", err)
			select {
			case <-time.After(m.opts.RetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err = m.fetchOnce(ctx, mod, part)
		var ce *domain.CodedError
		if err == nil || ctx.Err() != nil || errors.As(err, &ce) {
			break
		}
	}
	if err != nil {
		return err
	}

	m.update(mod, api.LocalModelStatusVerifying, 1, mod.Size)
	sum, err := fileSHA256(part)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, mod.SHA256) {
		_ = os.Remove(part)
		return &domain.CodedError{Code: CodeChecksumMismatch, Params: map[string]any{"model": mod.Name},
			Message: fmt.Sprintf("%s: SHA-256 %s, want %s; the file was deleted", mod.File(), sum, mod.SHA256)}
	}
	return os.Rename(part, filepath.Join(m.opts.Dir, mod.File()))
}

// fetchOnce makes one request, appending to part from its current size.
func (m *Manager) fetchOnce(ctx context.Context, mod Model, part string) error {
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	offset := st.Size()
	if offset > mod.Size {
		if err := f.Truncate(0); err != nil {
			return err
		}
		offset = 0
	}
	if offset == mod.Size {
		return nil // complete; verify
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.opts.BaseURL+"/"+mod.File(), nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := m.opts.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusPartialContent:
		if start, ok := rangeStart(resp.Header.Get("Content-Range")); !ok || start != offset {
			return restart(f, "unexpected Content-Range "+resp.Header.Get("Content-Range"))
		}
	case http.StatusOK:
		if err := f.Truncate(0); err != nil { // the server ignored the Range
			return err
		}
		offset = 0
	case http.StatusRequestedRangeNotSatisfiable:
		return restart(f, "range not satisfiable")
	default:
		err := &domain.CodedError{Code: CodeDownloadFailed, Params: map[string]any{"status": resp.StatusCode},
			Message: fmt.Sprintf("download %s: %s", mod.File(), resp.Status)}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return errors.New(err.Message) // retryable
		}
		return err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	done := offset
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if done+int64(n) > mod.Size {
				return restart(f, "the server sent more than the model's size")
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			m.update(mod, api.LocalModelStatusDownloading, float32(float64(done)/float64(mod.Size)), mod.Size)
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if done != mod.Size {
		return fmt.Errorf("download %s: got %d of %d bytes", mod.File(), done, mod.Size)
	}
	return nil
}

// restart empties the partial file so the next attempt starts over.
func restart(f *os.File, why string) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", errRestart, why)
}

// rangeStart reads the first byte position of "bytes 100-199/200".
func rangeStart(h string) (int64, bool) {
	rest, ok := strings.CutPrefix(h, "bytes ")
	if !ok {
		return 0, false
	}
	start, _, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(start), 10, 64)
	return n, err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
