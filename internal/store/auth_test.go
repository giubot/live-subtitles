// SPDX-License-Identifier: Apache-2.0

package store

import (
	"testing"
	"time"
)

func TestAdminSessions(t *testing.T) {
	ctx := t.Context()
	s := openTest(t)
	for _, h := range []string{"a", "b"} {
		if err := s.CreateAdminSession(ctx, h, t0, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateAdminSession(ctx, "old", t0.Add(-2*time.Hour), t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	valid := func(h string, at time.Time) bool {
		t.Helper()
		ok, err := s.AdminSessionValid(ctx, h, at)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	tests := []struct {
		name string
		hash string
		at   time.Time
		want bool
	}{
		{"fresh", "a", t0, true},
		{"at expiry", "a", t0.Add(time.Hour), false},
		{"expired", "old", t0, false},
		{"unknown", "zzz", t0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valid(tt.hash, tt.at); got != tt.want {
				t.Errorf("valid(%q) = %v, want %v", tt.hash, got, tt.want)
			}
		})
	}

	if err := s.DeleteAdminSession(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAdminSession(ctx, "a"); err != nil {
		t.Errorf("deleting twice: %v", err)
	}
	if valid("a", t0) || !valid("b", t0) {
		t.Error("logout removed the wrong session")
	}

	if err := s.PurgeAdminSessions(ctx, t0); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d sessions after purge, want 1", n)
	}

	if err := s.DeleteAdminSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if valid("b", t0) {
		t.Error("session survived DeleteAdminSessions")
	}
}
