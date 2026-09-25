// SPDX-License-Identifier: Apache-2.0

package loadtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Server is a livesubs process started for one load test run, on a free
// loopback port with a throwaway data directory.
type Server struct {
	URL   string
	Token string
	PID   int
	// Fixture is the audio file copied into the data directory, as the
	// absolute path the file source takes.
	Fixture string

	cmd     *exec.Cmd
	dataDir string
	exited  chan error
}

// StartServer runs binary with a fresh data directory, TLS off, metrics on
// and a random admin token, copies fixture into the data directory and
// waits for /healthz. Its log goes to logw (warnings and errors only).
func StartServer(ctx context.Context, binary, fixture string, logw io.Writer) (*Server, error) {
	dir, err := os.MkdirTemp("", "livesubs-loadtest-")
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Server, error) {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	dst := filepath.Join(dir, filepath.Base(fixture))
	if err := copyFile(fixture, dst); err != nil {
		return fail(fmt.Errorf("copy fixture: %w", err))
	}
	port, err := freePort()
	if err != nil {
		return fail(err)
	}
	if binary, err = filepath.Abs(binary); err != nil { // it runs in the data directory
		return fail(err)
	}
	tok := make([]byte, 24)
	_, _ = rand.Read(tok)
	s := &Server{
		URL:     fmt.Sprintf("http://127.0.0.1:%d", port),
		Token:   hex.EncodeToString(tok),
		Fixture: dst,
		dataDir: dir,
		exited:  make(chan error, 1),
	}
	s.cmd = exec.Command(binary,
		"--addr", fmt.Sprintf("127.0.0.1:%d", port),
		"--data-dir", dir,
		"--models-dir", filepath.Join(dir, "models"),
		"--tls", "disabled",
		"--no-keychain",
		"--metrics",
		"--log-level", "warn")
	s.cmd.Dir = dir
	s.cmd.Env = append(os.Environ(), "LIVESUBS_ADMIN_TOKEN="+s.Token)
	s.cmd.Stdout, s.cmd.Stderr = logw, logw
	if err := s.cmd.Start(); err != nil {
		return fail(fmt.Errorf("start %s: %w", binary, err))
	}
	s.PID = s.cmd.Process.Pid
	go func() { s.exited <- s.cmd.Wait() }()
	if err := s.waitHealthy(ctx, 30*time.Second); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Server) waitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-s.exited:
			s.exited <- err
			return fmt.Errorf("server exited during startup: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		res, err := http.Get(s.URL + "/healthz")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
	return errors.New("server did not become healthy in time")
}

// Close stops the server (SIGINT, then kill after 15 s) and deletes its
// data directory.
func (s *Server) Close() error {
	defer func() { _ = os.RemoveAll(s.dataDir) }()
	if s.cmd.Process == nil {
		return nil
	}
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil { // Windows has no SIGINT
		_ = s.cmd.Process.Kill()
	}
	select {
	case <-s.exited:
	case <-time.After(15 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.exited
	}
	return nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
