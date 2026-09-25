// SPDX-License-Identifier: Apache-2.0

// Package selector implements the default-provider rule (AI-11) and the
// Google API key check behind it (SEC-5): a session with `provider:
// default` runs on Gemini when a valid Google API key is saved, and on the
// local provider otherwise. The mock provider is never the default; it
// runs only when a session asks for it by name (development, demos, load
// tests).
package selector

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Translatable codes (UI-4) in ProviderInfo.reasonCode, SecretValidation.code
// and admin log events.
const (
	CodeNoAPIKey              = "provider.no_api_key"
	CodeKeyInvalid            = "provider.key_invalid"
	CodeKeyUnverified         = "provider.key_unverified"
	CodeWhisperUnreachable    = "provider.whisper_unreachable"
	CodeOllamaUnreachable     = "provider.ollama_unreachable"
	CodeFallbackKeyInvalid    = "provider.fallback_key_invalid"
	CodeFallbackKeyRemoved    = "provider.fallback_key_removed"
	CodeFallbackKeyUnverified = "provider.fallback_key_unverified"
	CodeValidationUnsupported = "secret.validation_unsupported"
)

// Errors returned by Validate.
var (
	// ErrUnsupported: the secret has no check.
	ErrUnsupported = errors.New("selector: secret has no check")
	// ErrNotSet: the secret isn't stored.
	ErrNotSet = errors.New("selector: secret not set")
)

// Defaults for Options.
const (
	DefaultValidTTL      = 10 * time.Minute
	DefaultUnverifiedTTL = 30 * time.Second
	DefaultMinRecheck    = 5 * time.Second
	DefaultKeyTTL        = 5 * time.Second
)

// KeyValidator checks a Google API key; gemini.KeyValidator in production.
type KeyValidator interface {
	// ValidateKey reports whether Google accepts key; an error means it
	// couldn't tell (network, timeout, server error).
	ValidateKey(ctx context.Context, key string) (bool, error)
}

// Options configure a Selector. APIKey and Validator are required.
type Options struct {
	// APIKey returns the Google API key, or "" with a nil error when none
	// is set.
	APIKey    func(ctx context.Context) (string, error)
	Validator KeyValidator
	// Local reports whether the local provider's sidecars answer, with a
	// translatable reason when they don't; nil: always available.
	Local func(ctx context.Context) (ok bool, reasonCode string)
	// Publish sends admin events (/ws/admin); nil drops them.
	Publish func(api.AdminEvent)
	Logger  *slog.Logger
	Now     func() time.Time

	// ValidTTL is how long an accepted or rejected key is trusted before
	// it is checked again (default 10 min). A stale result is still used
	// while the new check runs in the background.
	ValidTTL time.Duration
	// UnverifiedTTL is how soon a key that couldn't be checked is tried
	// again (default 30 s).
	UnverifiedTTL time.Duration
	// MinRecheck rate-limits explicit checks (Validate) of the same key:
	// within it the previous result is returned (default 5 s).
	MinRecheck time.Duration
	// KeyTTL is how long the key read from the secret store is reused
	// (default 5 s); Forget drops it early.
	KeyTTL time.Duration
}

// keyState is what the selector knows about the saved Google API key.
type keyState int

const (
	keyUnknown    keyState = iota // nothing observed yet
	keyNone                       // no key saved
	keyValid                      // Google accepted it
	keyInvalid                    // Google rejected it
	keyUnverified                 // it couldn't be checked (offline, Google down, store error)
)

func (k keyState) String() string {
	return [...]string{"unknown", "none", "valid", "invalid", "unverified"}[k]
}

// verdict is the last check of one key, identified by its hash so the
// value itself isn't kept twice.
type verdict struct {
	hash    [32]byte
	state   keyState
	checked time.Time
	expires time.Time
}

// flight is a key check in progress; waiters block on done.
type flight struct {
	hash         [32]byte
	done         chan struct{}
	fresh, state keyState
}

// Selector resolves `provider: default` and checks the Google API key.
// It is safe for concurrent use.
type Selector struct {
	opts Options
	log  *slog.Logger

	mu       sync.Mutex
	key      string // cached read of the secret store
	keyErr   error
	keyUntil time.Time
	v        verdict
	inflight *flight
	last     keyState // the state the last decision was made on, for warnings
}

// New returns a Selector.
func New(opts Options) *Selector {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ValidTTL <= 0 {
		opts.ValidTTL = DefaultValidTTL
	}
	if opts.UnverifiedTTL <= 0 {
		opts.UnverifiedTTL = DefaultUnverifiedTTL
	}
	if opts.MinRecheck <= 0 {
		opts.MinRecheck = DefaultMinRecheck
	}
	if opts.KeyTTL <= 0 {
		opts.KeyTTL = DefaultKeyTTL
	}
	return &Selector{opts: opts, log: opts.Logger}
}

// Decision is the resolved default provider and why.
type Decision struct {
	Kind domain.ProviderKind
	// Reason is nil when a saved key couldn't be checked yet.
	Reason *api.ProvidersResponseDefaultReason
	key    keyState
}

// rule is the default-provider rule on what is known about the key.
func rule(k keyState) Decision {
	reason := func(r api.ProvidersResponseDefaultReason) *api.ProvidersResponseDefaultReason { return &r }
	switch k {
	case keyValid:
		return Decision{Kind: api.ProviderKindGemini, Reason: reason(api.GoogleApiKeyValid), key: k}
	case keyInvalid:
		return Decision{Kind: api.ProviderKindLocal, Reason: reason(api.GoogleApiKeyInvalid), key: k}
	case keyUnverified:
		return Decision{Kind: api.ProviderKindLocal, key: k}
	default:
		return Decision{Kind: api.ProviderKindLocal, Reason: reason(api.NoGoogleApiKey), key: k}
	}
}

// fallbackWarning is the admin warning for a change of the key's state
// from prev to cur, or "" when there is nothing to warn about: the default
// falls back to local because the key is invalid, removed or unverifiable.
func fallbackWarning(prev, cur keyState) string {
	if prev == cur {
		return ""
	}
	switch cur {
	case keyInvalid:
		return CodeFallbackKeyInvalid
	case keyUnverified:
		return CodeFallbackKeyUnverified
	case keyNone:
		if prev == keyValid {
			return CodeFallbackKeyRemoved
		}
	}
	return ""
}

// DefaultProvider resolves `provider: default`; it is
// session.Options.DefaultProvider.
func (s *Selector) DefaultProvider(ctx context.Context) domain.ProviderKind {
	return s.Resolve(ctx).Kind
}

// Resolve applies the default-provider rule. It checks the key with Google
// only when nothing is known about it yet; a stale result is used while a
// background check refreshes it.
func (s *Selector) Resolve(ctx context.Context) Decision {
	return rule(s.keyState(ctx))
}

// Providers reports the availability of every provider and the resolved
// default (GET /api/providers).
func (s *Selector) Providers(ctx context.Context) api.ProvidersResponse {
	type local struct {
		ok     bool
		reason string
	}
	lc := make(chan local, 1)
	go func() {
		if s.opts.Local == nil {
			lc <- local{ok: true}
			return
		}
		ok, reason := s.opts.Local(ctx)
		lc <- local{ok, reason}
	}()
	d := s.Resolve(ctx)
	l := <-lc

	str := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	gemini := api.ProviderInfo{Kind: api.ProviderKindGemini, Available: d.key == keyValid}
	switch d.key {
	case keyInvalid:
		gemini.ReasonCode = str(CodeKeyInvalid)
	case keyUnverified:
		gemini.ReasonCode = str(CodeKeyUnverified)
	case keyNone, keyUnknown:
		gemini.ReasonCode = str(CodeNoAPIKey)
	}
	localInfo := api.ProviderInfo{Kind: api.ProviderKindLocal, Available: l.ok}
	if !l.ok {
		localInfo.ReasonCode = str(l.reason)
	}
	return api.ProvidersResponse{
		DefaultProvider: d.Kind,
		DefaultReason:   d.Reason,
		Providers: []api.ProviderInfo{gemini, localInfo,
			{Kind: api.ProviderKindMock, Available: true}},
	}
}

// Available reports whether a running session may switch to kind (the
// provider fallback, AI-8), with a translatable reason when it can't:
// Gemini needs a key Google accepted, local needs its sidecars to answer.
// Mock is never a fallback. It is session.Options.FallbackAvailable.
func (s *Selector) Available(ctx context.Context, kind domain.ProviderKind) (bool, string) {
	switch kind {
	case api.ProviderKindGemini:
		switch s.keyState(ctx) {
		case keyValid:
			return true, ""
		case keyInvalid:
			return false, CodeKeyInvalid
		case keyUnverified:
			return false, CodeKeyUnverified
		default:
			return false, CodeNoAPIKey
		}
	case api.ProviderKindLocal:
		if s.opts.Local == nil {
			return true, ""
		}
		return s.opts.Local(ctx)
	}
	return false, ""
}

// Validate checks the named secret now (POST /api/secrets/{name}/validate
// and saving a key). A check of the same key within MinRecheck returns the
// previous result. ErrNotSet if the secret isn't stored, ErrUnsupported if
// it has no check.
func (s *Selector) Validate(ctx context.Context, name string) (api.SecretValidation, error) {
	if name != string(api.GoogleApiKey) {
		return api.SecretValidation{}, ErrUnsupported
	}
	s.Forget() // read the value just saved
	key, err := s.readKey(ctx)
	if err != nil {
		s.observe(keyUnverified)
		return api.SecretValidation{}, err
	}
	if key == "" {
		s.observe(keyNone)
		return api.SecretValidation{}, ErrNotSet
	}
	h := sha256.Sum256([]byte(key))
	s.mu.Lock()
	var fresh keyState
	if s.v.hash == h && s.v.state != keyUnknown && s.opts.Now().Sub(s.v.checked) < s.opts.MinRecheck {
		fresh = s.v.state
		s.mu.Unlock()
	} else {
		f := s.startLocked(key, h)
		s.mu.Unlock()
		select {
		case <-f.done:
			fresh = f.fresh
		case <-ctx.Done():
			return api.SecretValidation{}, ctx.Err()
		}
	}
	out := api.SecretValidation{Valid: fresh == keyValid}
	switch fresh {
	case keyInvalid:
		c := CodeKeyInvalid
		out.Code = &c
	case keyUnverified:
		c := CodeKeyUnverified
		out.Code = &c
	}
	return out, nil
}

// LastValid is the result of the last check of the named secret's current
// value, without calling Google: nil when it hasn't been checked or
// couldn't be (SecretInfo.valid).
func (s *Selector) LastValid(ctx context.Context, name string) *bool {
	if name != string(api.GoogleApiKey) {
		return nil
	}
	key, err := s.readKey(ctx)
	if err != nil || key == "" {
		return nil
	}
	h := sha256.Sum256([]byte(key))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v.hash != h || (s.v.state != keyValid && s.v.state != keyInvalid) {
		return nil
	}
	ok := s.v.state == keyValid
	return &ok
}

// Forget drops the cached key, so a key saved or deleted a moment ago is
// seen by the next decision.
func (s *Selector) Forget() {
	s.mu.Lock()
	s.key, s.keyErr, s.keyUntil = "", nil, time.Time{}
	s.mu.Unlock()
}

// Warm resolves the default once in the background, so the first session
// list after startup doesn't wait for Google.
func (s *Selector) Warm(ctx context.Context) {
	go s.Resolve(context.WithoutCancel(ctx))
}

// readKey reads the key from the store, reusing it for KeyTTL.
func (s *Selector) readKey(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.opts.Now().Before(s.keyUntil) {
		k, err := s.key, s.keyErr
		s.mu.Unlock()
		return k, err
	}
	s.mu.Unlock()
	key, err := s.opts.APIKey(ctx)
	if err != nil {
		// Only the error: the store never puts values in it.
		s.log.Warn("read the Google API key", "secret", api.GoogleApiKey, "err", err)
	}
	s.mu.Lock()
	s.key, s.keyErr, s.keyUntil = key, err, s.opts.Now().Add(s.opts.KeyTTL)
	s.mu.Unlock()
	return key, err
}

// keyState is what is known about the current key, checking it with
// Google when nothing is.
func (s *Selector) keyState(ctx context.Context) keyState {
	key, err := s.readKey(ctx)
	switch {
	case err != nil:
		s.observe(keyUnverified)
		return keyUnverified
	case key == "":
		s.observe(keyNone)
		return keyNone
	}
	h := sha256.Sum256([]byte(key))
	now := s.opts.Now()
	s.mu.Lock()
	if s.v.hash == h && s.v.state != keyUnknown {
		st := s.v.state
		if !now.Before(s.v.expires) && s.inflight == nil {
			s.startLocked(key, h) // refresh in the background, use the stale result meanwhile
		}
		s.mu.Unlock()
		s.observe(st)
		return st
	}
	f := s.startLocked(key, h)
	s.mu.Unlock()
	select {
	case <-f.done:
		return f.state // check already observed it
	case <-ctx.Done():
		return keyUnverified
	}
}

// startLocked starts a check of key, or joins the one in progress for the
// same key. s.mu is held.
func (s *Selector) startLocked(key string, h [32]byte) *flight {
	if f := s.inflight; f != nil && f.hash == h {
		return f
	}
	f := &flight{hash: h, done: make(chan struct{})}
	s.inflight = f
	go s.check(key, f)
	return f
}

// check asks Google about the key and records the verdict. A key that
// can't be checked keeps its previous definite verdict (so a blip doesn't
// flip the default), and is tried again after UnverifiedTTL.
func (s *Selector) check(key string, f *flight) {
	defer close(f.done)
	// Not tied to the request that started it: waiters may give up, the
	// result is still worth keeping. The validator bounds the call.
	valid, err := s.opts.Validator.ValidateKey(context.Background(), key)
	fresh := keyInvalid
	switch {
	case err != nil:
		fresh = keyUnverified
		s.log.Warn("Google API key couldn't be checked", "secret", api.GoogleApiKey, "err", err)
	case valid:
		fresh = keyValid
	}
	now := s.opts.Now()
	s.mu.Lock()
	state := fresh
	if fresh == keyUnverified && s.v.hash == f.hash && (s.v.state == keyValid || s.v.state == keyInvalid) {
		state = s.v.state
	}
	ttl := s.opts.ValidTTL
	if fresh == keyUnverified {
		ttl = s.opts.UnverifiedTTL
	}
	s.v = verdict{hash: f.hash, state: state, checked: now, expires: now.Add(ttl)}
	if s.inflight == f {
		s.inflight = nil
	}
	f.fresh, f.state = fresh, state
	s.mu.Unlock()
	if fresh != keyUnverified {
		s.log.Info("Google API key checked", "secret", api.GoogleApiKey, "valid", fresh == keyValid)
	}
	// A newer key may have been saved meanwhile; only a check of the
	// current key moves the default.
	if cur, err := s.readKey(context.Background()); err == nil && sha256.Sum256([]byte(cur)) == f.hash {
		s.observe(state)
	}
}

// observe records the key state a decision was made on and warns admins
// when the default falls back to local (AI-11).
func (s *Selector) observe(cur keyState) {
	s.mu.Lock()
	prev := s.last
	s.last = cur
	s.mu.Unlock()
	code := fallbackWarning(prev, cur)
	if code == "" {
		return
	}
	s.log.Warn("default provider falls back to local", "code", code, "was", prev.String(), "now", cur.String())
	if s.opts.Publish == nil {
		return
	}
	ev := api.AdminEvent{Type: api.AdminEventTypeLog, At: s.opts.Now()}
	params := map[string]any{"provider": string(api.ProviderKindLocal)}
	ev.Log = &struct {
		Code      string                  `json:"code"`
		Level     api.AdminEventLogLevel  `json:"level"`
		Params    *map[string]interface{} `json:"params,omitempty"`
		SessionId *api.Slug               `json:"sessionId,omitempty"`
	}{Code: code, Level: api.AdminEventLogLevelWarn, Params: &params}
	s.opts.Publish(ev)
}
