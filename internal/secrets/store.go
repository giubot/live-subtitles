// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

var (
	// ErrNotFound means no backend holds the secret.
	ErrNotFound = errors.New("secret not found")
	// ErrReadOnly means the secret comes from the environment, which
	// shadows any stored value; unset the variable to manage it here.
	ErrReadOnly = errors.New("secret is set by an environment variable and is read-only")
	// ErrInvalidName rejects names that don't match NamePattern.
	ErrInvalidName = errors.New("invalid secret name")
	// ErrNoBackend means there is nowhere to write: no OS keychain and no
	// master key for the encrypted file.
	ErrNoBackend = errors.New("no writable secret backend: no OS keychain available and " + MasterKeyEnv + " is not set")
	// ErrDecrypt means the encrypted file can't be opened: wrong master key
	// or a tampered file.
	ErrDecrypt = errors.New("cannot decrypt secrets file: wrong " + MasterKeyEnv + " or tampered file")
	// ErrCorrupt means the encrypted file is not in the expected format.
	ErrCorrupt = errors.New("secrets file is corrupted")
)

// NamePattern is what a secret name must match. Besides the well-known
// names (google_api_key…) it allows scoped names such as
// "session:main-stage:youtube_url".
var NamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

// Options configure New. The zero value reads the process environment and
// uses the OS keychain when one answers.
type Options struct {
	// DataDir holds the encrypted file (DataDir/secrets.enc).
	DataDir string
	// MasterKey is the file passphrase. Empty means Getenv(MasterKeyEnv);
	// if that is empty too, the encrypted file backend is disabled.
	MasterKey string
	// Getenv defaults to os.Getenv.
	Getenv func(string) string
	// Keyring defaults to SystemKeyring. It is probed once in New; if it
	// doesn't answer (headless Linux, CI) the store skips it.
	Keyring Keyring
	// DisableKeychain skips the keychain entirely.
	DisableKeychain bool
	// Redactor, if set, learns every secret value the store sees so a
	// RedactingHandler can mask it in logs.
	Redactor *Redactor
	// Logger defaults to slog.Default(). Values are never logged.
	Logger *slog.Logger
	// Now defaults to time.Now.
	Now func() time.Time
}

// Store implements domain.SecretStore. Lookups go env → OS keychain →
// encrypted file; writes go to the keychain when available, else the file.
type Store struct {
	getenv   func(string) string
	keyring  Keyring // nil when unavailable
	file     *fileBackend
	redactor *Redactor
	log      *slog.Logger
	now      func() time.Time
}

var _ domain.SecretStore = (*Store)(nil)

// New builds a store. It fails if the encrypted file exists but can't be
// decrypted with the master key, so a wrong key surfaces at startup.
func New(opts Options) (*Store, error) {
	s := &Store{getenv: opts.Getenv, redactor: opts.Redactor, log: opts.Logger, now: opts.Now}
	if s.getenv == nil {
		s.getenv = os.Getenv
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	for name := range envAliases {
		if v, ok := s.fromEnv(name); ok {
			s.redactor.Add(v)
		}
	}

	if !opts.DisableKeychain {
		kr := opts.Keyring
		if kr == nil {
			kr = SystemKeyring{}
		}
		if err := keyringAvailable(kr); err != nil {
			s.log.Info("OS keychain unavailable, using the encrypted secrets file", "reason", err.Error())
		} else {
			s.keyring = kr
		}
	}

	master := opts.MasterKey
	if master == "" {
		master = s.getenv(MasterKeyEnv)
	}
	if master != "" && opts.DataDir != "" {
		s.redactor.Add(master)
		s.file = newFileBackend(opts.DataDir, master)
		entries, err := s.file.all()
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			s.redactor.Add(e.Value)
		}
	} else if opts.DataDir != "" {
		path := newFileBackend(opts.DataDir, "").path
		if _, err := os.Stat(path); err == nil {
			s.log.Warn("encrypted secrets file present but "+MasterKeyEnv+" is not set; its secrets are unavailable", "path", path)
		}
	}
	return s, nil
}

// Backends reports which backends are active, for diagnostics.
func (s *Store) Backends() (keychain, file bool) { return s.keyring != nil, s.file != nil }

func checkName(name string) error {
	if !NamePattern.MatchString(name) {
		return fmt.Errorf("%w %q", ErrInvalidName, name)
	}
	return nil
}

type resolved struct {
	value     string
	source    domain.SecretSource
	updatedAt *time.Time
}

func (s *Store) fromEnv(name string) (string, bool) {
	for _, v := range EnvVars(name) {
		if val := s.getenv(v); val != "" {
			return val, true
		}
	}
	return "", false
}

func (s *Store) resolve(name string) (resolved, error) {
	if err := checkName(name); err != nil {
		return resolved{}, err
	}
	if v, ok := s.fromEnv(name); ok {
		return resolved{value: v, source: domain.SecretFromEnv}, nil
	}
	if s.keyring != nil {
		v, err := s.keyring.Get(KeychainService, name)
		switch {
		case err == nil:
			return resolved{value: v, source: domain.SecretFromKeychain}, nil
		case !isNotFound(err):
			s.log.Warn("keychain lookup failed, trying the encrypted file", "name", name, "err", err.Error())
		}
	}
	if s.file != nil {
		e, ok, err := s.file.get(name)
		if err != nil {
			return resolved{}, err
		}
		if ok {
			t := e.UpdatedAt
			return resolved{value: e.Value, source: domain.SecretFromFile, updatedAt: &t}, nil
		}
	}
	return resolved{}, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// GetSecret returns the value and where it came from, or ErrNotFound.
func (s *Store) GetSecret(_ context.Context, name string) (string, domain.SecretSource, error) {
	r, err := s.resolve(name)
	if err != nil {
		return "", "", err
	}
	s.redactor.Add(r.value)
	return r.value, r.source, nil
}

// SetSecret stores value in the keychain, or the encrypted file when the
// keychain is unavailable or refuses it. It returns ErrReadOnly if the
// environment provides the secret.
func (s *Store) SetSecret(_ context.Context, name, value string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if value == "" {
		return errors.New("secret value is empty")
	}
	if _, ok := s.fromEnv(name); ok {
		return fmt.Errorf("%w: %s", ErrReadOnly, name)
	}
	s.redactor.Add(value)
	if s.keyring != nil {
		err := s.keyring.Set(KeychainService, name, value)
		if err == nil {
			// Drop any older copy in the file so it can't resurface.
			if s.file != nil {
				if _, err := s.file.delete(name); err != nil {
					s.log.Warn("could not remove the old file copy of a secret", "name", name, "err", err.Error())
				}
			}
			s.log.Info("secret stored", "name", name, "source", domain.SecretFromKeychain)
			return nil
		}
		if s.file == nil {
			return fmt.Errorf("keychain: %w", err)
		}
		s.log.Warn("keychain write failed, using the encrypted file", "name", name, "err", err.Error())
	}
	if s.file == nil {
		return ErrNoBackend
	}
	if err := s.file.set(name, value, s.now()); err != nil {
		return err
	}
	s.log.Info("secret stored", "name", name, "source", domain.SecretFromFile)
	return nil
}

// DeleteSecret removes the secret from the keychain and the encrypted file.
// It returns ErrReadOnly if the environment provides it and ErrNotFound if
// no backend held it.
func (s *Store) DeleteSecret(_ context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if _, ok := s.fromEnv(name); ok {
		return fmt.Errorf("%w: %s", ErrReadOnly, name)
	}
	found := false
	if s.keyring != nil {
		switch err := s.keyring.Delete(KeychainService, name); {
		case err == nil:
			found = true
		case !isNotFound(err):
			return fmt.Errorf("keychain: %w", err)
		}
	}
	if s.file != nil {
		ok, err := s.file.delete(name)
		if err != nil {
			return err
		}
		found = found || ok
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	s.log.Info("secret deleted", "name", name)
	return nil
}

// SecretInfo describes the secret without its value. An unset secret is
// not an error: it returns Set=false.
func (s *Store) SecretInfo(_ context.Context, name string) (domain.SecretInfo, error) {
	info := domain.SecretInfo{Name: api.SecretName(name)}
	r, err := s.resolve(name)
	if errors.Is(err, ErrNotFound) {
		return info, nil
	}
	if err != nil {
		return info, err
	}
	s.redactor.Add(r.value)
	hint := Hint(r.value)
	src := APISource(r.source)
	info.Set = true
	info.Hint = &hint
	info.Source = &src
	info.UpdatedAt = r.updatedAt
	return info, nil
}

// APISource maps a domain source onto the contract enum.
func APISource(src domain.SecretSource) api.SecretInfoSource {
	switch src {
	case domain.SecretFromEnv:
		return api.Env
	case domain.SecretFromKeychain:
		return api.Keychain
	default:
		return api.EncryptedFile
	}
}

// Hint masks value, keeping its last 4 characters ("••••3f9a"). Values
// shorter than 8 characters show no characters at all, so short passwords
// aren't half revealed.
func Hint(value string) string {
	const mask = "••••"
	r := []rune(value)
	if len(r) < 8 {
		return mask
	}
	return mask + string(r[len(r)-4:])
}
