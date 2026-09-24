// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// FileName is the encrypted secrets file inside the data dir.
const FileName = "secrets.enc"

const (
	fileVersion = 1
	kdfArgon2id = "argon2id"
	keyLen      = 32 // AES-256
	saltLen     = 16
)

// kdfParams are stored in the file so they can change without breaking
// existing files.
type kdfParams struct {
	Alg       string `json:"alg"`
	Salt      []byte `json:"salt"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memoryKiB"`
	Threads   uint8  `json:"threads"`
}

// defaultKDF follows the RFC 9106 second recommended option (64 MiB, t=3).
// Tests lower it to keep the suite fast.
var defaultKDF = kdfParams{Alg: kdfArgon2id, Time: 3, MemoryKiB: 64 * 1024, Threads: 4}

func (p kdfParams) validate() error {
	switch {
	case p.Alg != kdfArgon2id:
		return fmt.Errorf("unsupported kdf %q", p.Alg)
	case len(p.Salt) < saltLen:
		return errors.New("salt too short")
	case p.Time < 1 || p.Time > 16:
		return errors.New("kdf time out of range")
	case p.MemoryKiB < 8 || p.MemoryKiB > 1024*1024:
		return errors.New("kdf memory out of range")
	case p.Threads < 1:
		return errors.New("kdf threads out of range")
	}
	return nil
}

// fileHeader is authenticated as GCM additional data, so tampering with the
// KDF parameters or version fails decryption.
type fileHeader struct {
	Version int       `json:"version"`
	KDF     kdfParams `json:"kdf"`
}

type fileEnvelope struct {
	fileHeader
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

type fileEntry struct {
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// fileBackend is an AES-256-GCM encrypted JSON map of secrets, keyed with
// Argon2id from a passphrase. Every write re-encrypts the whole file with a
// fresh nonce and replaces it atomically.
type fileBackend struct {
	path       string
	passphrase []byte

	mu      sync.Mutex
	keySalt []byte // salt the cached key was derived from
	key     []byte
}

func newFileBackend(dir, passphrase string) *fileBackend {
	return &fileBackend{path: filepath.Join(dir, FileName), passphrase: []byte(passphrase)}
}

func (f *fileBackend) deriveKey(p kdfParams) []byte {
	if f.key != nil && bytes.Equal(f.keySalt, p.Salt) {
		return f.key
	}
	f.key = argon2.IDKey(f.passphrase, p.Salt, p.Time, p.MemoryKiB, p.Threads, keyLen)
	f.keySalt = append([]byte(nil), p.Salt...)
	return f.key
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// load decrypts the file. A missing file is an empty store (kdf nil).
func (f *fileBackend) load() (map[string]fileEntry, *kdfParams, error) {
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]fileEntry{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read secrets file: %w", err)
	}
	var env fileEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrCorrupt, f.path, err)
	}
	if env.Version != fileVersion {
		return nil, nil, fmt.Errorf("%w: %s: unsupported version %d", ErrCorrupt, f.path, env.Version)
	}
	if err := env.KDF.validate(); err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrCorrupt, f.path, err)
	}
	aead, err := newGCM(f.deriveKey(env.KDF))
	if err != nil {
		return nil, nil, err
	}
	if len(env.Nonce) != aead.NonceSize() {
		return nil, nil, fmt.Errorf("%w: %s: bad nonce", ErrCorrupt, f.path)
	}
	aad, err := json.Marshal(env.fileHeader)
	if err != nil {
		return nil, nil, err
	}
	plain, err := aead.Open(nil, env.Nonce, env.Ciphertext, aad)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrDecrypt, f.path)
	}
	entries := map[string]fileEntry{}
	if err := json.Unmarshal(plain, &entries); err != nil {
		return nil, nil, fmt.Errorf("%w: %s: payload: %v", ErrCorrupt, f.path, err)
	}
	return entries, &env.KDF, nil
}

// save encrypts entries and atomically replaces the file (temp + rename,
// 0600). kdf is reused when the file already exists; otherwise a new salt
// is generated.
func (f *fileBackend) save(entries map[string]fileEntry, kdf *kdfParams) error {
	if kdf == nil {
		p := defaultKDF
		p.Salt = make([]byte, saltLen)
		if _, err := rand.Read(p.Salt); err != nil {
			return err
		}
		kdf = &p
	}
	aead, err := newGCM(f.deriveKey(*kdf))
	if err != nil {
		return err
	}
	env := fileEnvelope{fileHeader: fileHeader{Version: fileVersion, KDF: *kdf}}
	aad, err := json.Marshal(env.fileHeader)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	env.Nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(env.Nonce); err != nil {
		return err
	}
	env.Ciphertext = aead.Seal(nil, env.Nonce, plain, aad)
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(f.path, out)
}

func writeFileAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (f *fileBackend) get(name string) (fileEntry, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, _, err := f.load()
	if err != nil {
		return fileEntry{}, false, err
	}
	e, ok := entries[name]
	return e, ok, nil
}

func (f *fileBackend) set(name, value string, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, kdf, err := f.load()
	if err != nil {
		return err
	}
	entries[name] = fileEntry{Value: value, UpdatedAt: now.UTC()}
	return f.save(entries, kdf)
}

// delete reports whether name was present.
func (f *fileBackend) delete(name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, kdf, err := f.load()
	if err != nil {
		return false, err
	}
	if _, ok := entries[name]; !ok {
		return false, nil
	}
	delete(entries, name)
	return true, f.save(entries, kdf)
}

// all returns every stored entry.
func (f *fileBackend) all() (map[string]fileEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, _, err := f.load()
	return entries, err
}
