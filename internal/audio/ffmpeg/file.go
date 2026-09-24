// SPDX-License-Identifier: Apache-2.0

package ffmpeg

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	// ErrNotAllowed: the URI is neither a file under an allowed directory
	// nor an http(s) URL.
	ErrNotAllowed = errors.New("input not allowed: use a file under the data or testdata directory, or an http(s) URL")
	// ErrNotFound: the local file doesn't exist.
	ErrNotFound = errors.New("file not found")
)

// urlProtocols may be used by an http(s) input, including HLS playlists.
// "file" is left out so a remote playlist can't read local files.
var urlProtocols = []string{"http", "https", "tcp", "tls", "crypto"}

// Files opens file and URL inputs as test sources (AUD-3).
type Files struct {
	// Binary is the ffmpeg executable (default DefaultBinary).
	Binary string
	// Roots are the directories local files may come from.
	Roots []string
}

// FileInput is what POST …/sources/file asks for.
type FileInput struct {
	URI     string
	Loop    bool
	StartAt time.Duration
}

// Open validates the input and returns a real-time source for it.
func (f Files) Open(in FileInput) (*Source, error) {
	src := &Source{Binary: f.Binary, Loop: in.Loop, StartAt: max(in.StartAt, 0), Realtime: true}
	uri := strings.TrimSpace(in.URI)
	if uri == "" {
		return nil, ErrNotAllowed
	}
	if u, err := url.Parse(uri); err == nil && len(u.Scheme) > 1 { // one letter: a Windows drive
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, ErrNotAllowed
		}
		src.Input, src.Protocols = uri, urlProtocols
		return src, nil
	}
	path, err := f.resolve(uri)
	if err != nil {
		return nil, err
	}
	// The file: prefix keeps a name like "a:b.wav" from being read as a protocol.
	src.Input, src.Protocols = "file:"+path, []string{"file"}
	return src, nil
}

// resolve returns the real path of a regular file inside one of the roots.
func (f Files) resolve(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, name)
	} else if err != nil {
		return "", err
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a file", ErrNotAllowed, name)
	}
	for _, root := range f.Roots {
		r, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if rr, err := filepath.EvalSymlinks(r); err == nil {
			r = rr
		}
		if rel, err := filepath.Rel(r, real); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return real, nil
		}
	}
	return "", fmt.Errorf("%w (%s)", ErrNotAllowed, name)
}
