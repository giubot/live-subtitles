// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// CookieName is the admin session cookie (the spec's adminSession scheme).
const CookieName = "ls_admin"

// PIN length limits, as in the spec's SetupRequest.
const (
	MinPINLength = 4
	MaxPINLength = 64
)

// Defaults for Options.
const (
	DefaultSessionTTL    = 7 * 24 * time.Hour
	DefaultMaxFailures   = 5
	DefaultFailureWindow = 15 * time.Minute
	// MinAdminTokenLength keeps a guessable bearer token out of config.
	MinAdminTokenLength = 16
)

var (
	// ErrSetupDone: the admin PIN is already set.
	ErrSetupDone = errors.New("auth: setup already completed")
	// ErrSetupRequired: no admin PIN yet, so nobody can log in.
	ErrSetupRequired = errors.New("auth: setup required")
	// ErrInvalidPIN: the PIN is too short or too long.
	ErrInvalidPIN = fmt.Errorf("auth: PIN must be %d to %d characters", MinPINLength, MaxPINLength)
	// ErrWrongPIN: the PIN doesn't match.
	ErrWrongPIN = errors.New("auth: wrong PIN")
	// ErrInvalidToken: a missing or wrong ingest token.
	ErrInvalidToken = errors.New("auth: invalid ingest token")
)

// RateLimitedError is returned by Login while a client is locked out.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("auth: too many failed logins, retry in %s", e.RetryAfter.Round(time.Second))
}

// Store is what the service persists: the PIN hash, admin logins and
// per-session ingest token hashes. *store.Store implements it.
type Store interface {
	AdminPINHash(ctx context.Context) (string, error)
	InitAdminPINHash(ctx context.Context, hash string) error
	CreateAdminSession(ctx context.Context, tokenHash string, created, expires time.Time) error
	AdminSessionValid(ctx context.Context, tokenHash string, now time.Time) (bool, error)
	DeleteAdminSession(ctx context.Context, tokenHash string) error
	PurgeAdminSessions(ctx context.Context, now time.Time) error
	IngestTokenHash(ctx context.Context, sessionID string) (string, error)
	SetIngestTokenHash(ctx context.Context, sessionID, hash string) error
}

// Options configure New. Zero fields take the defaults.
type Options struct {
	// AdminToken, if set, is accepted as `Authorization: Bearer <token>` on
	// admin endpoints (LIVESUBS_ADMIN_TOKEN).
	AdminToken string
	// SessionTTL is how long a login lasts.
	SessionTTL time.Duration
	// PIN sets the Argon2id cost of new PIN hashes.
	PIN PINParams
	// MaxFailures failed logins from one client within FailureWindow lock it
	// out until the oldest one leaves the window.
	MaxFailures   int
	FailureWindow time.Duration
	Clock         domain.Clock
}

// Service authenticates the admin (PIN → cookie, or bearer token) and
// audio ingest clients (per-session token) (ADM-3).
type Service struct {
	store   Store
	opts    Options
	limiter *limiter
}

// New returns a Service backed by store.
func New(store Store, opts Options) *Service {
	if opts.SessionTTL <= 0 {
		opts.SessionTTL = DefaultSessionTTL
	}
	if opts.PIN == (PINParams{}) {
		opts.PIN = DefaultPINParams
	}
	if opts.MaxFailures <= 0 {
		opts.MaxFailures = DefaultMaxFailures
	}
	if opts.FailureWindow <= 0 {
		opts.FailureWindow = DefaultFailureWindow
	}
	if opts.Clock == nil {
		opts.Clock = domain.SystemClock{}
	}
	return &Service{store: store, opts: opts, limiter: newLimiter(opts.MaxFailures, opts.FailureWindow)}
}

// SetupDone reports whether the admin PIN is set.
func (s *Service) SetupDone(ctx context.Context) (bool, error) {
	_, err := s.store.AdminPINHash(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// Setup sets the first admin PIN and logs the caller in, returning the
// cookie value. It fails with ErrSetupDone once a PIN exists.
func (s *Service) Setup(ctx context.Context, pin string) (string, error) {
	if n := utf8.RuneCountInString(pin); n < MinPINLength || n > MaxPINLength {
		return "", ErrInvalidPIN
	}
	if done, err := s.SetupDone(ctx); err != nil {
		return "", err
	} else if done {
		return "", ErrSetupDone // skip the expensive hash
	}
	hash, err := HashPIN(pin, s.opts.PIN)
	if err != nil {
		return "", err
	}
	if err := s.store.InitAdminPINHash(ctx, hash); errors.Is(err, domain.ErrConflict) {
		return "", ErrSetupDone
	} else if err != nil {
		return "", err
	}
	return s.newSession(ctx)
}

// Login checks pin for client (its IP address) and returns a new cookie
// value. Failures count towards client's lockout (*RateLimitedError).
func (s *Service) Login(ctx context.Context, client, pin string) (string, error) {
	now := s.opts.Clock.Now()
	if wait := s.limiter.retryAfter(client, now); wait > 0 {
		return "", &RateLimitedError{RetryAfter: wait}
	}
	hash, err := s.store.AdminPINHash(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return "", ErrSetupRequired
	} else if err != nil {
		return "", err
	}
	ok, err := VerifyPIN(hash, pin)
	if err != nil {
		return "", err
	}
	if !ok {
		s.limiter.fail(client, now)
		return "", ErrWrongPIN
	}
	s.limiter.reset(client)
	return s.newSession(ctx)
}

func (s *Service) newSession(ctx context.Context) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	now := s.opts.Clock.Now()
	_ = s.store.PurgeAdminSessions(ctx, now) // housekeeping; a failure only leaves stale rows
	if err := s.store.CreateAdminSession(ctx, hashToken(token), now, now.Add(s.opts.SessionTTL)); err != nil {
		return "", err
	}
	return token, nil
}

// Logout ends the login whose cookie value is token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteAdminSession(ctx, hashToken(token))
}

// Authenticated reports whether r carries a valid admin cookie or bearer token.
func (s *Service) Authenticated(ctx context.Context, r *http.Request) (bool, error) {
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if s.opts.AdminToken != "" && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(bearer)), []byte(s.opts.AdminToken)) == 1 {
			return true, nil
		}
	}
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return false, nil
	}
	return s.store.AdminSessionValid(ctx, hashToken(c.Value), s.opts.Clock.Now())
}

// Cookie returns the admin cookie for token. Secure is set when the
// request came over TLS; on plain-HTTP LAN setups it can't be.
func (s *Service) Cookie(token string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.opts.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   secure,
		// Strict: the admin API is only called by the app itself, so no
		// cross-site request ever needs the cookie (CSRF protection).
		SameSite: http.SameSiteStrictMode,
	}
}

// RotateIngestToken gives the session a new ingest token, invalidating the
// previous one, and returns it. The token itself is never stored.
func (s *Service) RotateIngestToken(ctx context.Context, sessionID string) (string, error) {
	token, hash, err := NewIngestToken()
	if err != nil {
		return "", err
	}
	if err := s.store.SetIngestTokenHash(ctx, sessionID, hash); err != nil {
		return "", err
	}
	return token, nil
}

// VerifyIngestToken checks token against the session's ingest token. It
// returns an error wrapping domain.ErrNotFound for an unknown session and
// ErrInvalidToken for a bad token (ingest.TokenVerifier).
func (s *Service) VerifyIngestToken(ctx context.Context, sessionID, token string) error {
	want, err := s.store.IngestTokenHash(ctx, sessionID)
	if err != nil {
		return err
	}
	if want == "" || token == "" || subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(want)) != 1 {
		return ErrInvalidToken
	}
	return nil
}

// NewIngestToken returns a random ingest token and the hash to store.
func NewIngestToken() (token, hash string, err error) {
	token, err = randomToken(16)
	if err != nil {
		return "", "", err
	}
	return token, hashToken(token), nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken hashes a random token for storage. The tokens carry 128+ bits
// of entropy, so a fast hash is enough (unlike the PIN).
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
