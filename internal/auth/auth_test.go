// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/store"
)

// cheap keeps tests fast; production uses DefaultPINParams.
var cheap = PINParams{Time: 1, MemKiB: 64, Threads: 1}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTest(t *testing.T, opts Options) (*Service, *store.Store, *fakeClock) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clock := &fakeClock{now: time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)}
	opts.PIN, opts.Clock = cheap, clock
	return New(st, opts), st, clock
}

func TestHashPIN(t *testing.T) {
	h, err := HashPIN("2468", cheap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("hash %q is not a PHC argon2id string", h)
	}
	if h2, _ := HashPIN("2468", cheap); h2 == h {
		t.Error("two hashes of the same PIN are equal: salt not random")
	}
	tests := []struct {
		name, hash, pin string
		want            bool
		wantErr         bool
	}{
		{"right", h, "2468", true, false},
		{"wrong", h, "2469", false, false},
		{"empty", h, "", false, false},
		{"not phc", "plain", "2468", false, true},
		{"other algorithm", strings.Replace(h, "argon2id", "argon2i", 1), "2468", false, true},
		{"bad params", strings.Replace(h, "t=1", "t=x", 1), "2468", false, true},
		{"zero time", strings.Replace(h, "t=1", "t=0", 1), "2468", false, true},
		{"bad salt", strings.Replace(h, "$m=64,t=1,p=1$", "$m=64,t=1,p=1$!!", 1), "2468", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VerifyPIN(tt.hash, tt.pin)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("VerifyPIN = %v, %v; want %v, err %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSetupAndLogin(t *testing.T) {
	ctx := t.Context()
	s, _, clock := newTest(t, Options{SessionTTL: time.Hour})

	if done, err := s.SetupDone(ctx); err != nil || done {
		t.Fatalf("SetupDone before setup = %v, %v", done, err)
	}
	if _, err := s.Login(ctx, "10.0.0.2", "2468"); !errors.Is(err, ErrSetupRequired) {
		t.Errorf("login before setup: %v, want ErrSetupRequired", err)
	}
	for _, pin := range []string{"", "123", strings.Repeat("9", MaxPINLength+1)} {
		if _, err := s.Setup(ctx, pin); !errors.Is(err, ErrInvalidPIN) {
			t.Errorf("Setup(%q): %v, want ErrInvalidPIN", pin, err)
		}
	}
	token, err := s.Setup(ctx, "2468")
	if err != nil {
		t.Fatal(err)
	}
	if done, _ := s.SetupDone(ctx); !done {
		t.Error("SetupDone false after setup")
	}
	if _, err := s.Setup(ctx, "1357"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("second setup: %v, want ErrSetupDone", err)
	}

	authed := func(r *http.Request) bool {
		t.Helper()
		ok, err := s.Authenticated(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	withCookie := func(v string) *http.Request {
		r := httptest.NewRequest("GET", "/api/sessions", nil)
		r.AddCookie(&http.Cookie{Name: CookieName, Value: v})
		return r
	}
	if !authed(withCookie(token)) {
		t.Error("setup cookie not accepted")
	}

	if _, err := s.Login(ctx, "10.0.0.2", "0000"); !errors.Is(err, ErrWrongPIN) {
		t.Errorf("wrong PIN: %v", err)
	}
	token2, err := s.Login(ctx, "10.0.0.2", "2468")
	if err != nil {
		t.Fatal(err)
	}
	if token2 == token || !authed(withCookie(token2)) {
		t.Error("login did not create a new valid session")
	}

	if err := s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if authed(withCookie(token)) || !authed(withCookie(token2)) {
		t.Error("logout ended the wrong session")
	}
	if authed(withCookie("forged")) || authed(httptest.NewRequest("GET", "/", nil)) {
		t.Error("request without a valid cookie accepted")
	}
	clock.Add(time.Hour)
	if authed(withCookie(token2)) {
		t.Error("expired session accepted")
	}
}

func TestBearerToken(t *testing.T) {
	const token = "0123456789abcdef-admin"
	tests := []struct {
		name, configured, header string
		want                     bool
	}{
		{"match", token, "Bearer " + token, true},
		{"wrong", token, "Bearer nope", false},
		{"no header", token, "", false},
		{"basic scheme", token, "Basic " + token, false},
		{"not configured", "", "Bearer ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := newTest(t, Options{AdminToken: tt.configured})
			r := httptest.NewRequest("GET", "/api/sessions", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			if got, err := s.Authenticated(t.Context(), r); err != nil || got != tt.want {
				t.Errorf("Authenticated = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestLoginRateLimit(t *testing.T) {
	ctx := t.Context()
	s, _, clock := newTest(t, Options{MaxFailures: 3, FailureWindow: time.Minute})
	if _, err := s.Setup(ctx, "2468"); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if _, err := s.Login(ctx, "10.0.0.9", "0000"); !errors.Is(err, ErrWrongPIN) {
			t.Fatalf("attempt %d: %v", i, err)
		}
		clock.Add(10 * time.Second)
	}
	var rl *RateLimitedError
	if _, err := s.Login(ctx, "10.0.0.9", "2468"); !errors.As(err, &rl) {
		t.Fatalf("4th attempt: %v, want RateLimitedError", err)
	}
	// The first failure was 30 s ago, so it leaves the window in 30 s.
	if rl.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter %v, want 30s", rl.RetryAfter)
	}
	if _, err := s.Login(ctx, "10.0.0.10", "2468"); err != nil {
		t.Errorf("another client is locked out too: %v", err)
	}
	clock.Add(30 * time.Second)
	if _, err := s.Login(ctx, "10.0.0.9", "2468"); err != nil {
		t.Errorf("still locked out after the window: %v", err)
	}
	// Success resets the count.
	for range 2 {
		_, _ = s.Login(ctx, "10.0.0.9", "0000")
	}
	if _, err := s.Login(ctx, "10.0.0.9", "0000"); !errors.Is(err, ErrWrongPIN) {
		t.Errorf("failures before the success still counted: %v", err)
	}
}

func TestCookie(t *testing.T) {
	s, _, _ := newTest(t, Options{SessionTTL: 2 * time.Hour})
	c := s.Cookie("tok", true)
	if c.Name != CookieName || c.Value != "tok" || !c.HttpOnly || !c.Secure ||
		c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 7200 {
		t.Errorf("cookie %+v", c)
	}
	if s.Cookie("tok", false).Secure {
		t.Error("Secure set on a plain-HTTP cookie")
	}
}

func TestIngestToken(t *testing.T) {
	ctx := t.Context()
	s, st, _ := newTest(t, Options{})
	now := time.Now()
	if err := st.CreateSession(ctx, domain.Session{Id: "main", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyIngestToken(ctx, "main", ""); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("session without a token accepted an empty one: %v", err)
	}
	old, err := s.RotateIngestToken(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(old) != 22 {
		t.Errorf("token %q: want 22 url-safe characters", old)
	}
	if hash, _ := st.IngestTokenHash(ctx, "main"); hash == old || hash == "" {
		t.Error("token stored in clear or not stored")
	}
	if err := s.VerifyIngestToken(ctx, "main", old); err != nil {
		t.Errorf("valid token rejected: %v", err)
	}
	tok, err := s.RotateIngestToken(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, session, token string
		want                 error
	}{
		{"current", "main", tok, nil},
		{"rotated out", "main", old, ErrInvalidToken},
		{"empty", "main", "", ErrInvalidToken},
		{"unknown session", "nope", tok, domain.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.VerifyIngestToken(ctx, tt.session, tt.token); !errors.Is(err, tt.want) {
				t.Errorf("VerifyIngestToken = %v, want %v", err, tt.want)
			}
		})
	}
	if _, err := s.RotateIngestToken(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rotate for unknown session: %v", err)
	}
}

func TestRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.30:51234"
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	got := RequestFrom(WithRequest(context.Background(), NewRequest(r, true)))
	want := Request{Client: "192.168.1.30", Admin: true, Cookie: "abc"}
	if got != want {
		t.Errorf("request %+v, want %+v", got, want)
	}
	if (RequestFrom(context.Background()) != Request{}) {
		t.Error("empty context has a request")
	}
}
