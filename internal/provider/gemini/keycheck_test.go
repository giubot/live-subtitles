// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKeyValidator(t *testing.T) {
	const key = "AIzaSyTestKeyValue1234"
	tests := []struct {
		name      string
		key       string
		status    int
		body      string
		wantValid bool
		wantErr   bool
	}{
		{"accepted", key, 200, `{"models":[{"name":"models/gemini-3.5-flash-lite"}]}`, true, false},
		{"invalid key", key, 400, `{"error":{"code":400,"message":"API key not valid.","status":"INVALID_ARGUMENT"}}`, false, false},
		{"unauthorized", key, 401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, false, false},
		{"forbidden", key, 403, `{"error":{"code":403,"status":"PERMISSION_DENIED"}}`, false, false},
		{"over quota still works", key, 429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`, true, false},
		{"server error can't tell", key, 503, `{"error":{"code":503,"status":"UNAVAILABLE"}}`, false, true},
		{"empty key", "", 200, `{}`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if !strings.HasSuffix(r.URL.Path, "/models") {
					t.Errorf("path %s, want a models list", r.URL.Path)
				}
				if strings.Contains(r.URL.RawQuery, tt.key) {
					t.Errorf("key in the query string")
				}
				if got := r.Header.Get("x-goog-api-key"); got != tt.key {
					t.Errorf("x-goog-api-key %q", got)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			valid, err := KeyValidator{BaseURL: srv.URL}.ValidateKey(t.Context(), tt.key)
			if valid != tt.wantValid || (err != nil) != tt.wantErr {
				t.Fatalf("ValidateKey = %v, %v; want %v, err %v", valid, err, tt.wantValid, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), tt.key) {
				t.Errorf("error leaks the key: %v", err)
			}
			if tt.key == "" && calls != 0 {
				t.Errorf("empty key called Google")
			}
		})
	}
}

func TestKeyValidatorUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens there any more
	valid, err := KeyValidator{BaseURL: url}.ValidateKey(context.Background(), "AIzaSyUnreachable0000")
	if valid || err == nil {
		t.Fatalf("ValidateKey = %v, %v; want an error", valid, err)
	}
	if strings.Contains(err.Error(), "AIzaSyUnreachable0000") || strings.Contains(err.Error(), url) {
		t.Errorf("error leaks details: %v", err)
	}
}
