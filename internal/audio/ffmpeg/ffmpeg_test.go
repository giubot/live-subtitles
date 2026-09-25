// SPDX-License-Identifier: Apache-2.0

package ffmpeg

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/audio"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// fixture is the committed 7.85 s English clip (16 kHz mono).
var fixture = filepath.Join("..", "..", "..", "testdata", "audio", "fixtures", "en.wav")

const fixtureFrames = 393 // 7.85 s / 20 ms, the last one padded

func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(DefaultBinary); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

func TestFilesOpen(t *testing.T) {
	// Resolve symlinks (macOS temp dirs live under /var → /private/var) so
	// the expected inputs match the resolved paths Open returns.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for _, p := range []string{filepath.Join(root, "talk.wav"), filepath.Join(outside, "secret.wav")} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.wav"), filepath.Join(root, "link.wav")); err != nil {
		t.Fatal(err)
	}
	f := Files{Roots: []string{root}}
	tests := []struct {
		name, uri string
		wantInput string
		wantProto string
		wantErr   error
	}{
		{"file in root", filepath.Join(root, "talk.wav"), "file:" + filepath.Join(root, "talk.wav"), "file", nil},
		{"dot-dot back into root", filepath.Join(root, "dir", "..", "talk.wav"), "file:" + filepath.Join(root, "talk.wav"), "file", nil},
		{"https URL", "https://example.com/talk.m4a", "https://example.com/talk.m4a", "http", nil},
		{"file outside the roots", filepath.Join(outside, "secret.wav"), "", "", ErrNotAllowed},
		{"symlink out of the root", filepath.Join(root, "link.wav"), "", "", ErrNotAllowed},
		{"missing file", filepath.Join(root, "nope.wav"), "", "", ErrNotFound},
		{"directory", filepath.Join(root, "dir"), "", "", ErrNotAllowed},
		{"file URL", "file://" + filepath.Join(root, "talk.wav"), "", "", ErrNotAllowed},
		{"other scheme", "ftp://example.com/a.wav", "", "", ErrNotAllowed},
		{"URL without host", "https:///a.wav", "", "", ErrNotAllowed},
		{"empty", "  ", "", "", ErrNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := f.Open(FileInput{URI: tt.uri})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if src.Input != tt.wantInput || src.Protocols[0] != tt.wantProto || !src.Realtime {
				t.Errorf("source %+v", src)
			}
			for _, p := range src.Protocols {
				if tt.wantProto == "http" && p == "file" {
					t.Error("URL input may read local files")
				}
			}
		})
	}
}

func collect(t *testing.T, s *Source, ctx context.Context) []domain.AudioFrame {
	t.Helper()
	ch, err := s.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frames []domain.AudioFrame
	for f := range ch {
		frames = append(frames, f)
	}
	return frames
}

func TestDecode(t *testing.T) {
	needFFmpeg(t)
	abs, _ := filepath.Abs(fixture)
	tests := []struct {
		name       string
		src        *Source
		wantFrames int
	}{
		{"whole file", &Source{Input: "file:" + abs, Protocols: []string{"file"}}, fixtureFrames},
		{"from 5 s", &Source{Input: "file:" + abs, Protocols: []string{"file"}, StartAt: 5 * time.Second}, fixtureFrames - 250},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames := collect(t, tt.src, t.Context())
			if d := len(frames) - tt.wantFrames; d < -2 || d > 2 {
				t.Errorf("%d frames, want about %d", len(frames), tt.wantFrames)
			}
			if err := tt.src.Err(); err != nil {
				t.Errorf("Err() = %v at the end of the file", err)
			}
			var loud int
			for i, f := range frames {
				if len(f.PCM) != domain.FrameSamples || f.T != time.Duration(i)*domain.FrameDuration {
					t.Fatalf("frame %d: %d samples at %v", i, len(f.PCM), f.T)
				}
				if audio.RMSDBFS(f.PCM) > -50 {
					loud++
				}
			}
			if loud < len(frames)/4 {
				t.Errorf("only %d of %d frames have speech: decoding is wrong", loud, len(frames))
			}
		})
	}
}

func TestLoopAndCancel(t *testing.T) {
	needFFmpeg(t)
	abs, _ := filepath.Abs(fixture)
	s := &Source{Input: "file:" + abs, Protocols: []string{"file"}, Loop: true}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := s.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for range ch {
		n++
		if n == 2*fixtureFrames+10 { // past the second loop
			cancel()
			break
		}
	}
	for range ch { // drains and closes once ffmpeg is killed
	}
	if n < 2*fixtureFrames+10 {
		t.Errorf("looped input ended after %d frames", n)
	}
	if err := s.Err(); err != nil {
		t.Errorf("cancel reported %v", err)
	}
}

func TestRealtime(t *testing.T) {
	needFFmpeg(t)
	abs, _ := filepath.Abs(fixture)
	s := &Source{Input: "file:" + abs, Protocols: []string{"file"}, Realtime: true}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	frames := collect(t, s, ctx)
	// -re delivers about 50 frames per second (ffmpeg may send a short burst first).
	if len(frames) < 20 || len(frames) > 150 {
		t.Errorf("%d frames in 1 s of real-time reading", len(frames))
	}
}

func TestErrors(t *testing.T) {
	needFFmpeg(t)
	s := &Source{Input: "file:" + filepath.Join(t.TempDir(), "missing.wav"), Protocols: []string{"file"}}
	if frames := collect(t, s, t.Context()); len(frames) != 0 {
		t.Errorf("%d frames from a missing file", len(frames))
	}
	if s.Err() == nil {
		t.Error("no error for a missing input")
	}

	// Protocols outside the whitelist are refused by ffmpeg itself.
	abs, _ := filepath.Abs(fixture)
	s = &Source{Input: "file:" + abs, Protocols: []string{"http"}}
	if frames := collect(t, s, t.Context()); len(frames) != 0 || s.Err() == nil {
		t.Errorf("whitelist ignored: %d frames, err %v", len(frames), s.Err())
	}

	s = &Source{Binary: filepath.Join(t.TempDir(), "no-ffmpeg"), Input: "x"}
	if _, err := s.Start(t.Context()); err == nil {
		t.Error("Start succeeded without an ffmpeg binary")
	}
}
