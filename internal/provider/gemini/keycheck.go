// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/genai"
)

// keyCheckTimeout bounds one key check, so an offline server decides fast.
const keyCheckTimeout = 5 * time.Second

// KeyValidator checks a Google API key with the cheapest Gemini call there
// is: listing one model. It costs no tokens and needs no model access.
type KeyValidator struct {
	// BaseURL overrides the Gemini API endpoint (tests).
	BaseURL string
	// HTTPClient is used for the call; nil uses the genai default.
	HTTPClient *http.Client
}

// ValidateKey reports whether Google accepts key. An error means it
// couldn't tell (network, timeout, server error); the error never contains
// the key.
func (v KeyValidator) ValidateKey(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      key,
		Backend:     genai.BackendGeminiAPI,
		HTTPClient:  v.HTTPClient,
		HTTPOptions: genai.HTTPOptions{BaseURL: v.BaseURL},
	})
	if err != nil {
		return false, errors.New("gemini key check: open client")
	}
	_, err = c.Models.List(ctx, &genai.ListModelsConfig{PageSize: 1})
	return keyVerdict(err)
}

// keyVerdict classifies the answer to the key check. Google answers 400
// API_KEY_INVALID for a malformed or unknown key and 401/403 for a revoked
// or restricted one; 429 means the key works but is over its quota.
func keyVerdict(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
			return false, nil
		case http.StatusTooManyRequests:
			return true, nil
		}
		// Only the status: the body is Google's and we don't log it.
		return false, fmt.Errorf("gemini key check: HTTP %d", apiErr.Code)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false, fmt.Errorf("gemini key check: %w", err)
	}
	// Transport errors may carry the request URL; keep only their kind.
	return false, errors.New("gemini key check: Google unreachable")
}
