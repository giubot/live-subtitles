// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Redacted replaces masked values in log output.
const Redacted = "[REDACTED]"

// minSecretLen keeps very short values from masking common substrings.
const minSecretLen = 4

// Redactor masks known secret values and credentials embedded in URLs.
// A nil *Redactor is valid: it only masks URLs.
type Redactor struct {
	mu     sync.RWMutex
	values map[string]struct{}
	repl   *strings.Replacer
}

// NewRedactor returns an empty redactor. The Store feeds it every value it
// reads or writes; other packages call Add for secrets they hold directly.
func NewRedactor() *Redactor { return &Redactor{values: map[string]struct{}{}} }

// Add registers a value to mask. Values shorter than 4 characters are ignored.
func (r *Redactor) Add(value string) {
	if r == nil || len(value) < minSecretLen {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[value]; ok {
		return
	}
	r.values[value] = struct{}{}
	vals := make([]string, 0, len(r.values))
	for v := range r.values {
		vals = append(vals, v)
	}
	// Longest first, so a secret containing another is masked whole.
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	pairs := make([]string, 0, 2*len(vals))
	for _, v := range vals {
		pairs = append(pairs, v, Redacted)
	}
	r.repl = strings.NewReplacer(pairs...)
}

// Redact masks known values and URL credentials (userinfo, sensitive query
// parameters such as key, token, sig or YouTube's cid) in s.
func (r *Redactor) Redact(s string) string {
	if r != nil {
		r.mu.RLock()
		repl := r.repl
		r.mu.RUnlock()
		if repl != nil {
			s = repl.Replace(s)
		}
	}
	if strings.Contains(s, "://") {
		s = urlPattern.ReplaceAllStringFunc(s, redactURL)
	}
	return s
}

var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]+`)

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	changed := false
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "REDACTED")
		} else {
			u.User = url.User("REDACTED")
		}
		changed = true
	}
	if u.RawQuery != "" {
		q, err := url.ParseQuery(u.RawQuery)
		if err == nil {
			for k := range q {
				if sensitiveParam(k) {
					q[k] = []string{"REDACTED"}
					changed = true
				}
			}
			if changed {
				u.RawQuery = q.Encode()
			}
		}
	}
	if !changed {
		return raw
	}
	return u.String()
}

// sensitiveKey reports whether a log attribute key names a credential.
func sensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range []string{"password", "passwd", "passphrase", "secret", "token", "apikey", "api_key", "api-key", "authorization", "cookie", "credential", "private"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return strings.HasSuffix(k, "key")
}

func sensitiveParam(name string) bool {
	switch strings.ToLower(name) {
	case "sig", "signature", "cid", "auth", "code", "x-goog-api-key":
		return true
	}
	return sensitiveKey(name)
}

// RedactingHandler wraps a slog.Handler and masks secrets: attributes with
// sensitive keys (password, token, *key, secret…), known secret values from
// the Redactor anywhere in the message or string/error/any values, and
// credentials in URLs. Attributes bound with WithAttrs are masked when bound.
type RedactingHandler struct {
	next slog.Handler
	r    *Redactor
}

var _ slog.Handler = (*RedactingHandler)(nil)

// NewRedactingHandler wraps next. r may be nil (key- and URL-based masking only).
func NewRedactingHandler(next slog.Handler, r *Redactor) *RedactingHandler {
	return &RedactingHandler{next: next, r: r}
}

func (h *RedactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *RedactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.Redact(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.redactAttr(a)
	}
	return &RedactingHandler{next: h.next.WithAttrs(red), r: h.r}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name), r: h.r}
}

func (h *RedactingHandler) redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	if sensitiveKey(a.Key) {
		if v.Kind() == slog.KindString && v.String() == "" {
			return a
		}
		return slog.String(a.Key, Redacted)
	}
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.r.Redact(v.String()))
	case slog.KindGroup:
		group := v.Group()
		red := make([]any, len(group))
		for i, g := range group {
			red[i] = h.redactAttr(g)
		}
		return slog.Group(a.Key, red...)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, h.r.Redact(err.Error()))
		}
		s := fmt.Sprint(v.Any())
		if red := h.r.Redact(s); red != s {
			return slog.String(a.Key, red)
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}
