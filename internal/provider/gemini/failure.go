// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"errors"
	"net/http"
	"strings"

	"google.golang.org/genai"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// failureCode is domain.CodeProviderQuotaExhausted or
// domain.CodeProviderAuthFailed when err says Google refused the call for
// its quota or its key, and "" otherwise. REST calls carry a
// genai.APIError; the Live API reports the same through the WebSocket
// close reason, so its text is matched too.
func failureCode(err error) string {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Code == http.StatusTooManyRequests || apiErr.Status == "RESOURCE_EXHAUSTED":
			return domain.CodeProviderQuotaExhausted
		case apiErr.Code == http.StatusUnauthorized || apiErr.Code == http.StatusForbidden,
			apiErr.Status == "UNAUTHENTICATED" || apiErr.Status == "PERMISSION_DENIED",
			strings.Contains(apiErr.Message, "API_KEY_INVALID") || strings.Contains(apiErr.Message, "API key not valid"):
			return domain.CodeProviderAuthFailed
		}
		return ""
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"resource_exhausted", "resource exhausted", "quota", "rate limit"} {
		if strings.Contains(msg, s) {
			return domain.CodeProviderQuotaExhausted
		}
	}
	for _, s := range []string{"api_key_invalid", "api key not valid", "permission_denied", "permission denied", "unauthenticated"} {
		if strings.Contains(msg, s) {
			return domain.CodeProviderAuthFailed
		}
	}
	return ""
}

// coded returns err as a *domain.CodedError when it is a quota or key
// failure (failureCode), so a session can fall back to the local provider
// at once (AI-8); any other error is returned as is.
func coded(err error) error {
	if err == nil {
		return nil
	}
	if ce := (*domain.CodedError)(nil); errors.As(err, &ce) {
		return err
	}
	code := failureCode(err)
	if code == "" {
		return err
	}
	return &domain.CodedError{Code: code, Message: err.Error(),
		Params: map[string]any{"provider": string(api.ProviderKindGemini)}}
}
