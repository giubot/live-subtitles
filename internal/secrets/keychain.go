// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"errors"
	"time"

	"github.com/zalando/go-keyring"
)

// KeychainService is the service name under which secrets are stored in the
// OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service).
const KeychainService = "live-subtitles"

// Keyring is the subset of an OS keychain the store uses. Get and Delete
// return ErrNotFound (or keyring.ErrNotFound) for missing entries.
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

// SystemKeyring is the OS keychain through github.com/zalando/go-keyring.
type SystemKeyring struct{}

func (SystemKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (SystemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (SystemKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

const probeUser = "__livesubs_probe__"

// probeTimeout bounds the availability check: on a headless host a D-Bus
// call without a Secret Service can hang instead of failing.
const probeTimeout = 3 * time.Second

// keyringAvailable reports whether kr answers a lookup. A missing entry
// counts as available; any other error (no D-Bus, no Secret Service,
// locked keychain…) means the store falls through to the encrypted file.
func keyringAvailable(kr Keyring) error {
	done := make(chan error, 1)
	go func() {
		_, err := kr.Get(KeychainService, probeUser)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || isNotFound(err) {
			return nil
		}
		return err
	case <-time.After(probeTimeout):
		return errors.New("keychain probe timed out")
	}
}

func isNotFound(err error) bool {
	return errors.Is(err, keyring.ErrNotFound) || errors.Is(err, ErrNotFound)
}
