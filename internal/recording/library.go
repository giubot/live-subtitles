// SPDX-License-Identifier: Apache-2.0

package recording

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// The recordings API (listRecordings, getRecording, deleteRecording,
// getRecordingAudio, getRecordingUsage).

// List returns the session's recordings (every recording for ""), newest
// first. expiresAt follows the current retention setting.
func (r *Recorder) List(ctx context.Context, sessionID string) ([]domain.Recording, error) {
	list, err := r.opts.Store.ListRecordings(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	keep := r.retention(ctx)
	for i := range list {
		list[i].ExpiresAt = expiry(list[i], keep)
	}
	return list, nil
}

// Get returns one recording, or domain.ErrNotFound.
func (r *Recorder) Get(ctx context.Context, id string) (domain.Recording, error) {
	rec, err := r.opts.Store.GetRecording(ctx, id)
	if err != nil {
		return rec, err
	}
	rec.ExpiresAt = expiry(rec, r.retention(ctx))
	return rec, nil
}

// Delete removes a recording and its file, or returns domain.ErrNotFound.
// A recording still being written stops first; its session keeps running
// without recording.
func (r *Recorder) Delete(ctx context.Context, id string) error {
	rec, err := r.opts.Store.GetRecording(ctx, id)
	if err != nil {
		return err
	}
	r.mu.Lock()
	s := r.active[id]
	r.mu.Unlock()
	if s != nil {
		s.abort(id)
	}
	if err := os.Remove(r.path(rec)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("recording: delete file: %w", err)
	}
	_ = os.Remove(filepath.Dir(r.path(rec))) // the session's directory, once empty
	return r.opts.Store.DeleteRecording(ctx, id)
}

// Open returns the recording's audio file (the caller closes it), or
// domain.ErrNotFound when the recording or its file is missing.
func (r *Recorder) Open(ctx context.Context, id string) (*os.File, domain.Recording, error) {
	rec, err := r.Get(ctx, id)
	if err != nil {
		return nil, rec, err
	}
	f, err := os.Open(r.path(rec))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, rec, domain.ErrNotFound
	}
	return f, rec, err
}

// Usage reports the disk space the recordings take and the space left on
// their disk (REC-5).
func (r *Recorder) Usage(ctx context.Context) (api.StorageUsage, error) {
	list, err := r.opts.Store.ListRecordings(ctx, "")
	if err != nil {
		return api.StorageUsage{}, err
	}
	u := api.StorageUsage{Recordings: len(list)}
	err = filepath.WalkDir(r.opts.Dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				u.UsedBytes += fi.Size()
			}
		}
		return nil
	})
	if err != nil {
		return u, fmt.Errorf("recording: disk usage: %w", err)
	}
	// The directory may not exist before the first recording: ask about
	// the closest one that does.
	for dir := r.opts.Dir; ; dir = filepath.Dir(dir) {
		if free, ok := freeBytes(dir); ok {
			u.FreeBytes = &free
			break
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return u, nil
}

// playable reports whether an MP4 file has its header and at least one
// complete fragment (moof + mdat). A fragmented file cut short by a crash
// plays up to its last complete fragment.
func playable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	size := fi.Size()
	var moov, moof, frag bool
	hdr := make([]byte, 16)
	for off := int64(0); off+8 <= size; {
		if _, err := f.ReadAt(hdr[:8], off); err != nil {
			break
		}
		n, head := int64(binary.BigEndian.Uint32(hdr)), int64(8)
		switch n {
		case 0: // to the end of the file
			n = size - off
		case 1: // 64-bit size
			if _, err := f.ReadAt(hdr[8:16], off+8); err != nil && !errors.Is(err, io.EOF) {
				return false
			}
			n, head = int64(binary.BigEndian.Uint64(hdr[8:16])), 16
		}
		if n < head || off+n > size {
			break // a box cut short
		}
		switch string(hdr[4:8]) {
		case "moov":
			moov = true
		case "moof":
			moof = true
		case "mdat":
			frag = frag || moof
			moof = false
		}
		off += n
	}
	return moov && frag
}
