// SPDX-License-Identifier: Apache-2.0

//go:build gemini

package gemini

import (
	"os"
	"testing"
)

// TestKeyValidatorLive checks a real key and a made-up one against Google:
//
//	GEMINI_API_KEY=… go test -tags gemini -run TestKeyValidatorLive ./internal/provider/gemini/
func TestKeyValidatorLive(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		t.Skip("set GEMINI_API_KEY or GOOGLE_API_KEY")
	}
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"real key", key, true},
		{"made-up key", "AIzaSyThisKeyDoesNotExist000000000000000", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := KeyValidator{}.ValidateKey(t.Context(), tt.key)
			if err != nil {
				t.Fatalf("ValidateKey: %v", err)
			}
			if valid != tt.want {
				t.Errorf("valid %v, want %v", valid, tt.want)
			}
		})
	}
}
