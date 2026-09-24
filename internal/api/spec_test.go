// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"testing"
)

// The embedded spec must load and validate, including the example values
// that the Prism mock server serves.
func TestEmbeddedSpecIsValid(t *testing.T) {
	doc, err := GetSwagger()
	if err != nil {
		t.Fatalf("load embedded spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("validate embedded spec: %v", err)
	}
	if doc.Paths.Find("/api/sessions") == nil {
		t.Fatal("embedded spec has no /api/sessions path")
	}
}
