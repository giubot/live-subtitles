// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"

	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestCoded(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string // "" keeps err as is
	}{
		{"429", fmt.Errorf("gemini translate: %w", genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED"}), domain.CodeProviderQuotaExhausted},
		{"resource exhausted status", genai.APIError{Code: 400, Status: "RESOURCE_EXHAUSTED"}, domain.CodeProviderQuotaExhausted},
		{"401", genai.APIError{Code: 401, Status: "UNAUTHENTICATED"}, domain.CodeProviderAuthFailed},
		{"403", genai.APIError{Code: 403, Status: "PERMISSION_DENIED"}, domain.CodeProviderAuthFailed},
		{"invalid key", genai.APIError{Code: 400, Status: "INVALID_ARGUMENT", Message: "API key not valid. Please pass a valid API key."}, domain.CodeProviderAuthFailed},
		{"other API error", genai.APIError{Code: 500, Status: "INTERNAL"}, ""},
		{"live close: quota", errors.New("websocket: close 1011 (internal server error): You exceeded your current quota"), domain.CodeProviderQuotaExhausted},
		{"live close: key", errors.New("websocket: close 1008 (policy violation): API key not valid"), domain.CodeProviderAuthFailed},
		{"network", errors.New("dial tcp: connection refused"), ""},
		{"cancelled", context.Canceled, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coded(tt.err)
			var ce *domain.CodedError
			switch {
			case tt.want == "":
				if errors.As(got, &ce) || got.Error() != tt.err.Error() {
					t.Errorf("coded(%v) = %#v, want it unchanged", tt.err, got)
				}
			case !errors.As(got, &ce) || ce.Code != tt.want:
				t.Errorf("coded(%v) = %#v, want code %s", tt.err, got, tt.want)
			case ce.Message != tt.err.Error():
				t.Errorf("message %q, want %q", ce.Message, tt.err.Error())
			}
		})
	}
	if coded(nil) != nil {
		t.Error("coded(nil) != nil")
	}
}

// Google's answers to a translation over quota or with a bad key come back
// as coded errors, through the real SDK.
func TestTranslatorCodedErrors(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		want       string
	}{
		{"quota", `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`, http.StatusTooManyRequests, domain.CodeProviderQuotaExhausted},
		{"key", `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT"}}`, http.StatusBadRequest, domain.CodeProviderAuthFailed},
		{"server error", `{"error":{"code":500,"message":"internal","status":"INTERNAL"}}`, http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			tr := &Translator{APIKey: func(context.Context) (string, error) { return "k", nil }, BaseURL: srv.URL,
				HTTPClient: &http.Client{Transport: &http.Transport{}}}
			for _, stream := range []bool{false, true} {
				var err error
				if stream {
					_, err = tr.TranslateStream(t.Context(), enToEs, nil)
				} else {
					_, err = tr.Translate(t.Context(), enToEs)
				}
				var ce *domain.CodedError
				got := ""
				if errors.As(err, &ce) {
					got = ce.Code
				}
				if err == nil || got != tt.want {
					t.Errorf("stream=%v: err %v (code %q), want code %q", stream, err, got, tt.want)
				}
			}
		})
	}
}
