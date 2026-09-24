// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// PINParams are the Argon2id cost parameters for new PIN hashes. Stored
// hashes carry their own parameters, so changing these doesn't lock anyone out.
type PINParams struct {
	Time    uint32
	MemKiB  uint32
	Threads uint8
}

// DefaultPINParams follow RFC 9106's second recommended option (64 MiB,
// 3 passes), which takes well under a second on a mini PC.
var DefaultPINParams = PINParams{Time: 3, MemKiB: 64 * 1024, Threads: 4}

const (
	saltLen = 16
	keyLen  = 32
)

var errBadHash = errors.New("auth: malformed PIN hash")

// HashPIN returns pin hashed with Argon2id in the PHC string format
// ($argon2id$v=19$m=…,t=…,p=…$salt$hash).
func HashPIN(pin string, p PINParams) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pin), salt, p.Time, p.MemKiB, p.Threads, keyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemKiB, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPIN reports whether pin matches hash, in constant time.
func VerifyPIN(hash, pin string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var p PINParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemKiB, &p.Time, &p.Threads); err != nil || p.Time == 0 || p.Threads == 0 {
		return false, errBadHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(pin), salt, p.Time, p.MemKiB, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
