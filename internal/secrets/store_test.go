// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestMain(m *testing.M) {
	// Cheap KDF so the suite stays fast; real files use defaultKDF's values.
	defaultKDF = kdfParams{Alg: kdfArgon2id, Time: 1, MemoryKiB: 64, Threads: 1}
	os.Exit(m.Run())
}

// fakeKeyring is an in-memory keychain.
type fakeKeyring struct {
	mu     sync.Mutex
	items  map[string]string
	getErr error // returned for every Get (e.g. no D-Bus)
	setErr error
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{items: map[string]string{}} }

func (k *fakeKeyring) Get(service, user string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.getErr != nil {
		return "", k.getErr
	}
	v, ok := k.items[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (k *fakeKeyring) Set(service, user, password string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.setErr != nil {
		return k.setErr
	}
	k.items[service+"/"+user] = password
	return nil
}

func (k *fakeKeyring) Delete(service, user string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.items[service+"/"+user]; !ok {
		return keyring.ErrNotFound
	}
	delete(k.items, service+"/"+user)
	return nil
}

func envFunc(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

var ctx = context.Background()

const masterKey = "correct horse battery staple"

// testStore builds a store over dir with the given keyring (nil disables it).
func testStore(t *testing.T, dir string, kr Keyring, env map[string]string) *Store {
	t.Helper()
	s, err := New(Options{
		DataDir:         dir,
		MasterKey:       masterKey,
		Getenv:          envFunc(env),
		Keyring:         kr,
		DisableKeychain: kr == nil,
		Logger:          slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestResolutionOrder(t *testing.T) {
	const name = "google_api_key"
	tests := []struct {
		name                string
		env, keychain, file string
		wantValue           string
		wantSource          domain.SecretSource
		wantErr             error
	}{
		{"env wins", "env-value-1", "kc-value-1", "file-value-1", "env-value-1", domain.SecretFromEnv, nil},
		{"keychain over file", "", "kc-value-1", "file-value-1", "kc-value-1", domain.SecretFromKeychain, nil},
		{"file last", "", "", "file-value-1", "file-value-1", domain.SecretFromFile, nil},
		{"nothing", "", "", "", "", "", ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.file != "" {
				if err := testStore(t, dir, nil, nil).SetSecret(ctx, name, tt.file); err != nil {
					t.Fatal(err)
				}
			}
			kr := newFakeKeyring()
			if tt.keychain != "" {
				_ = kr.Set(KeychainService, name, tt.keychain)
			}
			env := map[string]string{}
			if tt.env != "" {
				env["GEMINI_API_KEY"] = tt.env
			}
			v, src, err := testStore(t, dir, kr, env).GetSecret(ctx, name)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err %v, want %v", err, tt.wantErr)
			}
			if v != tt.wantValue || src != tt.wantSource {
				t.Errorf("got (%q, %q), want (%q, %q)", v, src, tt.wantValue, tt.wantSource)
			}
		})
	}
}

func TestEnvVars(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"google_api_key", []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "LIVESUBS_SECRET_GOOGLE_API_KEY"}},
		{"obs_websocket_password", []string{"OBS_WEBSOCKET_PASSWORD", "LIVESUBS_SECRET_OBS_WEBSOCKET_PASSWORD"}},
		{"session:main-stage:youtube_url", []string{"LIVESUBS_SECRET_SESSION_MAIN_STAGE_YOUTUBE_URL"}},
	}
	for _, tt := range tests {
		if got := EnvVars(tt.name); strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("EnvVars(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}

	// GEMINI_API_KEY is checked before GOOGLE_API_KEY.
	s := testStore(t, t.TempDir(), nil, map[string]string{"GOOGLE_API_KEY": "google-1", "GEMINI_API_KEY": "gemini-1"})
	if v, _, _ := s.GetSecret(ctx, "google_api_key"); v != "gemini-1" {
		t.Errorf("got %q, want GEMINI_API_KEY's value", v)
	}
}

func TestEnvIsReadOnly(t *testing.T) {
	s := testStore(t, t.TempDir(), newFakeKeyring(), map[string]string{"LIVESUBS_SECRET_OBS_WEBSOCKET_PASSWORD": "hunter2-long"})
	if err := s.SetSecret(ctx, "obs_websocket_password", "other"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Set: %v, want ErrReadOnly", err)
	}
	if err := s.DeleteSecret(ctx, "obs_websocket_password"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Delete: %v, want ErrReadOnly", err)
	}
	info, err := s.SecretInfo(ctx, "obs_websocket_password")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Set || *info.Source != api.Env || *info.Hint != "••••long" {
		t.Errorf("info %+v", info)
	}
}

func TestKeychainBackend(t *testing.T) {
	dir := t.TempDir()
	kr := newFakeKeyring()
	s := testStore(t, dir, kr, nil)
	if kc, file := s.Backends(); !kc || !file {
		t.Fatalf("backends keychain=%v file=%v", kc, file)
	}
	// An older file copy is removed when the keychain takes the secret.
	fileOnly := testStore(t, dir, nil, nil)
	if err := fileOnly.SetSecret(ctx, "google_api_key", "old-file-value"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret(ctx, "google_api_key", "AIzaSyExample3f9a"); err != nil {
		t.Fatal(err)
	}
	if kr.items[KeychainService+"/google_api_key"] != "AIzaSyExample3f9a" {
		t.Error("value not in keychain")
	}
	if _, _, err := fileOnly.GetSecret(ctx, "google_api_key"); !errors.Is(err, ErrNotFound) {
		t.Errorf("file copy still there: %v", err)
	}
	info, err := s.SecretInfo(ctx, "google_api_key")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Set || *info.Source != api.Keychain || *info.Hint != "••••3f9a" || info.UpdatedAt != nil {
		t.Errorf("info %+v", info)
	}
	if err := s.DeleteSecret(ctx, "google_api_key"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSecret(ctx, "google_api_key"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v, want ErrNotFound", err)
	}
}

func TestKeychainUnavailableFallsThrough(t *testing.T) {
	kr := newFakeKeyring()
	kr.getErr = errors.New("dbus: no session bus")
	s := testStore(t, t.TempDir(), kr, nil)
	if kc, _ := s.Backends(); kc {
		t.Fatal("unavailable keychain was kept")
	}
	if err := s.SetSecret(ctx, "google_api_key", "file-value-1234"); err != nil {
		t.Fatal(err)
	}
	if _, src, err := s.GetSecret(ctx, "google_api_key"); err != nil || src != domain.SecretFromFile {
		t.Errorf("got %q, %v", src, err)
	}
}

func TestKeychainWriteFailureFallsBackToFile(t *testing.T) {
	kr := newFakeKeyring()
	kr.setErr = keyring.ErrSetDataTooBig
	s := testStore(t, t.TempDir(), kr, nil)
	if err := s.SetSecret(ctx, "big", strings.Repeat("x", 4096)); err != nil {
		t.Fatal(err)
	}
	if _, src, err := s.GetSecret(ctx, "big"); err != nil || src != domain.SecretFromFile {
		t.Errorf("got %q, %v", src, err)
	}
}

func TestNoBackend(t *testing.T) {
	s, err := New(Options{DataDir: t.TempDir(), Getenv: envFunc(nil), DisableKeychain: true, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret(ctx, "google_api_key", "value-1234"); !errors.Is(err, ErrNoBackend) {
		t.Errorf("got %v, want ErrNoBackend", err)
	}
	info, err := s.SecretInfo(ctx, "google_api_key")
	if err != nil || info.Set {
		t.Errorf("info %+v, %v", info, err)
	}
}

func TestMasterKeyFromEnv(t *testing.T) {
	s, err := New(Options{DataDir: t.TempDir(), Getenv: envFunc(map[string]string{MasterKeyEnv: "from-env"}), DisableKeychain: true, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	if _, file := s.Backends(); !file {
		t.Error("file backend not enabled from " + MasterKeyEnv)
	}
}

func TestFileBackend(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 25, 13, 30, 0, 0, time.UTC)
	s, err := New(Options{DataDir: dir, MasterKey: masterKey, Getenv: envFunc(nil), DisableKeychain: true, Now: func() time.Time { return now }, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	const name, value = "session:main-stage:youtube_url", "http://upload.youtube.com/closedcaption?cid=abcd-efgh-1234"
	for _, v := range []string{"first-value-0000", value} { // overwrite
		if err := s.SetSecret(ctx, name, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetSecret(ctx, "other", "other-value"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, FileName)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("perm %v, want 0600", st.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "abcd-efgh") || strings.Contains(string(raw), "other-value") {
		t.Error("plaintext in secrets file")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}

	// A fresh store with the same key reads it back.
	s2 := testStore(t, dir, nil, nil)
	v, src, err := s2.GetSecret(ctx, name)
	if err != nil || v != value || src != domain.SecretFromFile {
		t.Errorf("got (%q, %q, %v)", v, src, err)
	}
	info, err := s2.SecretInfo(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if *info.Source != api.EncryptedFile || info.UpdatedAt == nil || !info.UpdatedAt.Equal(now) || *info.Hint != "••••1234" {
		t.Errorf("info %+v", info)
	}
	if err := s2.DeleteSecret(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetSecret(ctx, name); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if v, _, _ := s.GetSecret(ctx, "other"); v != "other-value" {
		t.Errorf("other secret lost: %q", v)
	}
}

func TestFileWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	if err := testStore(t, dir, nil, nil).SetSecret(ctx, "google_api_key", "value-1234"); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{DataDir: dir, MasterKey: "wrong", Getenv: envFunc(nil), DisableKeychain: true, Logger: slog.New(slog.DiscardHandler)})
	if !errors.Is(err, ErrDecrypt) {
		t.Errorf("got %v, want ErrDecrypt", err)
	}
}

func TestFileCorruption(t *testing.T) {
	tamper := func(f func(env map[string]any)) func([]byte) []byte {
		return func(raw []byte) []byte {
			var env map[string]any
			if err := json.Unmarshal(raw, &env); err != nil {
				panic(err)
			}
			f(env)
			out, _ := json.Marshal(env)
			return out
		}
	}
	tests := []struct {
		name   string
		modify func([]byte) []byte
		want   error
	}{
		{"not json", func([]byte) []byte { return []byte("garbage") }, ErrCorrupt},
		{"truncated", func(raw []byte) []byte { return raw[:len(raw)/2] }, ErrCorrupt},
		{"unknown version", tamper(func(env map[string]any) { env["version"] = 2 }), ErrCorrupt},
		{"bad nonce", tamper(func(env map[string]any) { env["nonce"] = "AAAA" }), ErrCorrupt},
		{"absurd kdf memory", tamper(func(env map[string]any) { env["kdf"].(map[string]any)["memoryKiB"] = 1 << 30 }), ErrCorrupt},
		{"tampered kdf (authenticated)", tamper(func(env map[string]any) { env["kdf"].(map[string]any)["time"] = 2 }), ErrDecrypt},
		{"flipped ciphertext", tamper(func(env map[string]any) {
			ct := []byte(env["ciphertext"].(string))
			if ct[0] == 'A' {
				ct[0] = 'B'
			} else {
				ct[0] = 'A'
			}
			env["ciphertext"] = string(ct)
		}), ErrDecrypt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := testStore(t, dir, nil, nil).SetSecret(ctx, "google_api_key", "value-1234"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, FileName)
			raw, _ := os.ReadFile(path)
			if err := os.WriteFile(path, tt.modify(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := New(Options{DataDir: dir, MasterKey: masterKey, Getenv: envFunc(nil), DisableKeychain: true, Logger: slog.New(slog.DiscardHandler)})
			if !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestInvalidName(t *testing.T) {
	s := testStore(t, t.TempDir(), newFakeKeyring(), nil)
	for _, name := range []string{"", "../etc", "has space", "-leading", strings.Repeat("a", 129)} {
		if _, _, err := s.GetSecret(ctx, name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Get(%q): %v", name, err)
		}
		if err := s.SetSecret(ctx, name, "value"); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Set(%q): %v", name, err)
		}
	}
}

func TestHint(t *testing.T) {
	tests := map[string]string{
		"AIzaSyExample3f9a": "••••3f9a",
		"12345678":          "••••5678",
		"short":             "••••",
		"":                  "••••",
		"ñandú-ñandú-ñandú": "••••andú",
	}
	for in, want := range tests {
		if got := Hint(in); got != want {
			t.Errorf("Hint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoreFeedsRedactor(t *testing.T) {
	r := NewRedactor()
	kr := newFakeKeyring()
	_ = kr.Set(KeychainService, "obs_websocket_password", "kc-secret-value")
	s, err := New(Options{DataDir: t.TempDir(), MasterKey: masterKey, Getenv: envFunc(map[string]string{"GEMINI_API_KEY": "env-secret-value"}), Keyring: kr, Redactor: r, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret(ctx, "custom", "set-secret-value"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetSecret(ctx, "obs_websocket_password"); err != nil {
		t.Fatal(err)
	}
	got := r.Redact("env-secret-value kc-secret-value set-secret-value " + masterKey)
	if strings.Contains(got, "value") || strings.Contains(got, "horse") {
		t.Errorf("not redacted: %q", got)
	}
}
