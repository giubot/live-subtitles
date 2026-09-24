// SPDX-License-Identifier: Apache-2.0

//go:build keychain

// Run against the real OS keychain with: go test -tags keychain ./internal/secrets/

package secrets

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestSystemKeychain(t *testing.T) {
	if err := keyringAvailable(SystemKeyring{}); err != nil {
		t.Skipf("no OS keychain: %v", err)
	}
	s, err := New(Options{Getenv: envFunc(nil), Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("test:%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = SystemKeyring{}.Delete(KeychainService, name) })

	if err := s.SetSecret(ctx, name, "keychain-test-value"); err != nil {
		t.Fatal(err)
	}
	v, src, err := s.GetSecret(ctx, name)
	if err != nil || v != "keychain-test-value" || src != domain.SecretFromKeychain {
		t.Fatalf("got (%q, %q, %v)", v, src, err)
	}
	if err := s.DeleteSecret(ctx, name); err != nil {
		t.Fatal(err)
	}
}
