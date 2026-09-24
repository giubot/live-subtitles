// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/auth"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/store"
)

const testAdminToken = "test-admin-token-0123456789"

// authServer is a Server with a real store and auth service.
func authServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New()
	s.Auth = auth.New(st, auth.Options{
		AdminToken:  testAdminToken,
		PIN:         auth.PINParams{Time: 1, MemKiB: 64, Threads: 1},
		MaxFailures: 2,
	})
	s.Secrets = &fakeSecrets{values: map[string]string{}}
	return s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler)), st
}

type call struct {
	method, path, body string
	cookie             string
	bearer             string
	wantStatus         int
	wantBody           string
}

func (c call) do(t *testing.T, h http.Handler) *http.Response {
	t.Helper()
	req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	req.RemoteAddr = "192.168.1.40:40000"
	if c.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: c.cookie})
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != c.wantStatus {
		t.Errorf("%s %s: status %d, want %d: %s", c.method, c.path, res.StatusCode, c.wantStatus, body)
	}
	if !strings.Contains(string(body), c.wantBody) {
		t.Errorf("%s %s: body %s lacks %s", c.method, c.path, body, c.wantBody)
	}
	return res
}

func adminCookie(t *testing.T, res *http.Response) string {
	t.Helper()
	for _, c := range res.Cookies() {
		if c.Name == auth.CookieName {
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Errorf("cookie flags %+v", c)
			}
			return c.Value
		}
	}
	t.Fatal("no admin cookie set")
	return ""
}

func TestSetupAndLoginFlow(t *testing.T) {
	h, _ := authServer(t)
	call{"GET", "/api/setup", "", "", "", 200, `"adminPinSet":false`}.do(t, h)
	call{"GET", "/api/auth/me", "", "", "", 200, `{"authenticated":false}`}.do(t, h)
	call{"POST", "/api/auth/login", `{"pin":"2468"}`, "", "", 401, `"code":"auth.setup_required"`}.do(t, h)
	call{"POST", "/api/setup", `{"pin":"12"}`, "", "", 400, `"code":"setup.pin_invalid"`}.do(t, h)

	setupCookie := adminCookie(t, call{"POST", "/api/setup", `{"pin":"2468"}`, "", "", 204, ""}.do(t, h))
	call{"GET", "/api/setup", "", "", "", 200, `"completed":true`}.do(t, h)
	call{"POST", "/api/setup", `{"pin":"1357"}`, "", "", 409, `"code":"setup.already_done"`}.do(t, h)
	call{"GET", "/api/auth/me", "", setupCookie, "", 200, `{"authenticated":true}`}.do(t, h)

	call{"POST", "/api/auth/login", `{"pin":"0000"}`, "", "", 401, `"code":"auth.invalid_pin"`}.do(t, h)
	call{"POST", "/api/auth/login", `{"pin":"0001"}`, "", "", 401, `"code":"auth.invalid_pin"`}.do(t, h)
	call{"POST", "/api/auth/login", `{"pin":"2468"}`, "", "", 429, `"code":"auth.rate_limited"`}.do(t, h)
}

func TestLoginLogout(t *testing.T) {
	h, _ := authServer(t)
	call{"POST", "/api/setup", `{"pin":"2468"}`, "", "", 204, ""}.do(t, h)
	cookie := adminCookie(t, call{"POST", "/api/auth/login", `{"pin":"2468"}`, "", "", 204, ""}.do(t, h))
	call{"GET", "/api/secrets", "", cookie, "", 200, `"google_api_key"`}.do(t, h)
	call{"POST", "/api/auth/logout", "", cookie, "", 204, ""}.do(t, h)
	call{"GET", "/api/secrets", "", cookie, "", 401, `"code":"auth.required"`}.do(t, h)
	call{"GET", "/api/auth/me", "", cookie, "", 200, `{"authenticated":false}`}.do(t, h)
	call{"POST", "/api/auth/logout", "", "", "", 401, `"code":"auth.required"`}.do(t, h)
}

func TestAdminRoutesNeedCredentials(t *testing.T) {
	h, st := authServer(t)
	now := time.Now()
	if err := st.CreateSession(t.Context(), domain.Session{Id: "main", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	tests := []call{
		// Admin operations (the spec's global security).
		{"GET", "/api/secrets", "", "", "", 401, `"code":"auth.required"`},
		{"GET", "/api/secrets", "", "forged", "", 401, `"code":"auth.required"`},
		{"GET", "/api/secrets", "", "", "wrong-token", 401, `"code":"auth.required"`},
		{"GET", "/api/secrets", "", "", testAdminToken, 200, `"google_api_key"`},
		{"GET", "/api/sessions", "", "", "", 401, `"code":"auth.required"`},
		{"GET", "/ws/admin", "", "", "", 401, `"code":"auth.required"`},
		{"POST", "/api/sessions/main/ingest-token", "", "", "", 401, `"code":"auth.required"`},
		{"POST", "/api/sessions/main/ingest-token", "", "", testAdminToken, 200, `"ingestToken":"`},
		{"POST", "/api/sessions/nope/ingest-token", "", "", testAdminToken, 404, `"code":"session.not_found"`},
		// Public operations (`security: []`) pass without credentials.
		{"GET", "/healthz", "", "", "", 200, `"status":"ok"`},
		{"GET", "/api/public/sessions", "", "", "", 501, `"code":"not_implemented"`},
		{"GET", "/api/network", "", "", "", 501, `"code":"not_implemented"`},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) { tt.do(t, h) })
	}
}

func TestSecurityPolicy(t *testing.T) {
	policy, err := securityPolicy()
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]access{
		"GET /healthz":                                   public,
		"POST /api/setup":                                public,
		"POST /api/auth/login":                           public,
		"POST /api/auth/logout":                          adminOnly,
		"GET /api/sessions":                              adminOnly,
		"GET /api/public/sessions":                       public,
		"GET /ws/ingest/{sessionId}":                     ingestTokenOnly,
		"GET /ws/captions/{sessionId}":                   public,
		"GET /ws/admin":                                  adminOnly,
		"PUT /api/secrets/{name}":                        adminOnly,
		"GET /api/tls/ca.crt":                            public,
		"GET /api/public/sessions/{sessionId}/subtitles": public,
	}
	for pattern, want := range tests {
		got, ok := policy[pattern]
		if !ok {
			t.Errorf("%s missing from the policy", pattern)
			continue
		}
		if got != want {
			t.Errorf("%s: access %d, want %d", pattern, got, want)
		}
	}
}
