// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

const knownSecret = "AIzaSyTopSecret3f9a"

func newTestLogger(r *Redactor) (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(NewRedactingHandler(slog.NewTextHandler(&buf, nil), r)), &buf
}

func TestRedactingHandler(t *testing.T) {
	type config struct{ Endpoint, Key string }
	tests := []struct {
		name     string
		log      func(*slog.Logger)
		mustNot  []string
		mustHave []string
	}{
		{"message", func(l *slog.Logger) { l.Info("using key " + knownSecret) }, []string{knownSecret}, []string{"using key [REDACTED]"}},
		{"string value", func(l *slog.Logger) { l.Info("x", "header", "Bearer "+knownSecret) }, []string{knownSecret}, []string{"Bearer [REDACTED]"}},
		{"error value", func(l *slog.Logger) { l.Info("x", "err", errors.New("bad key "+knownSecret)) }, []string{knownSecret}, []string{"bad key [REDACTED]"}},
		{"any value", func(l *slog.Logger) { l.Info("x", "cfg", config{"api", knownSecret}) }, []string{knownSecret}, []string{"[REDACTED]"}},
		{"group", func(l *slog.Logger) { l.Info("x", slog.Group("req", "note", knownSecret)) }, []string{knownSecret}, []string{"req.note=[REDACTED]"}},
		{"with attrs", func(l *slog.Logger) { l.With("note", knownSecret).WithGroup("g").Info("x", "n", 1) }, []string{knownSecret}, []string{"note=[REDACTED]", "g.n=1"}},
		{"sensitive keys", func(l *slog.Logger) {
			l.Info("x", "password", "pw-unknown", "api_key", "k-unknown", "Authorization", "tok-unknown", "master_key", "mk-unknown", "sessionToken", "st-unknown")
		}, []string{"unknown"}, []string{"password=[REDACTED]", "api_key=[REDACTED]", "Authorization=[REDACTED]", "sessionToken=[REDACTED]"}},
		{"sensitive group key", func(l *slog.Logger) { l.Info("x", slog.Group("secret", "a", "zzz-unknown")) }, []string{"unknown"}, []string{"secret=[REDACTED]"}},
		{"empty password kept", func(l *slog.Logger) { l.Info("x", "password", "") }, []string{"[REDACTED]"}, []string{"password=\"\""}},
		{"url query", func(l *slog.Logger) {
			l.Info("x", "url", "http://upload.youtube.com/closedcaption?cid=unknown-cid&seq=3")
		}, []string{"unknown-cid"}, []string{"cid=REDACTED", "seq=3"}},
		{"url key param in message", func(l *slog.Logger) {
			l.Info("GET https://generativelanguage.googleapis.com/v1/models?key=unknown-key failed")
		}, []string{"unknown-key"}, []string{"key=REDACTED", "failed"}},
		{"url userinfo", func(l *slog.Logger) { l.Info("x", "dsn", "srt://user:unknown-pw@host:9000") }, []string{"unknown-pw"}, []string{"user:REDACTED@host"}},
		{"harmless", func(l *slog.Logger) { l.Info("started", "addr", "https://example.com/x?lang=es", "count", 3) }, []string{"REDACTED"}, []string{"lang=es", "count=3"}},
		{"names are not values", func(l *slog.Logger) { l.Info("secret stored", "name", "google_api_key", "source", "keychain") }, []string{"REDACTED"}, []string{"name=google_api_key"}},
	}
	r := NewRedactor()
	r.Add(knownSecret)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, buf := newTestLogger(r)
			tt.log(l)
			out := buf.String()
			for _, s := range tt.mustNot {
				if strings.Contains(out, s) {
					t.Errorf("output contains %q: %s", s, out)
				}
			}
			for _, s := range tt.mustHave {
				if !strings.Contains(out, s) {
					t.Errorf("output lacks %q: %s", s, out)
				}
			}
		})
	}
}

func TestRedactor(t *testing.T) {
	var nilR *Redactor
	nilR.Add("ignored")
	if got := nilR.Redact("plain text"); got != "plain text" {
		t.Errorf("nil redactor changed text: %q", got)
	}

	r := NewRedactor()
	r.Add("abc") // too short, ignored
	r.Add("token-1")
	r.Add("token-1-extended") // contains the other: masked whole
	got := r.Redact("abc token-1 token-1-extended")
	if got != "abc [REDACTED] [REDACTED]" {
		t.Errorf("got %q", got)
	}
}
