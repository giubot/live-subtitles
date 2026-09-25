// SPDX-License-Identifier: Apache-2.0

package streamcc_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/fake"
	"github.com/iencodev/live-subtitles/internal/bus"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/mock"
	"github.com/iencodev/live-subtitles/internal/secrets"
	"github.com/iencodev/live-subtitles/internal/session"
	"github.com/iencodev/live-subtitles/internal/store"
	"github.com/iencodev/live-subtitles/internal/streamcc"
)

// TestSessionRunPostsCaptions runs a real session (mock provider, real bus
// and store) with stream captions on the Spanish track, against a fake
// YouTube ingestion endpoint.
func TestSessionRunPostsCaptions(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		_, _ = io.WriteString(w, time.Now().UTC().Format("2006-01-02T15:04:05.000"))
	}))
	defer yt.Close()
	posted := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}

	ctx := t.Context()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	on, track := true, "es"
	now := time.Now()
	if err := st.CreateSession(ctx, domain.Session{Id: "main", Name: "Main", Provider: api.ProviderChoiceMock,
		SourceLanguage: api.Auto, TargetLanguages: []string{"es"}, CreatedAt: now, UpdatedAt: now,
		StreamCaptions: &api.StreamCaptionsConfig{Enabled: &on, Track: &track}}); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	cc := streamcc.New(streamcc.Options{Secrets: memStore{"session:main:youtube_url": yt.URL + "/closedcaption?cid=x"}, Logger: log})
	b := bus.New()
	m := session.New(session.Options{
		Sessions: st, Captions: st, Bus: cc.Tap(b), Logger: log,
		Providers:      map[domain.ProviderKind]session.Provider{api.ProviderKindMock: {ASR: &mock.ASR{}, Translator: &mock.Translator{}}},
		IngestSource:   func(string) domain.AudioSource { return &fake.Source{} },
		StreamCaptions: cc,
	})
	cc.Bind(m.Events().Publish, m.StatusOf)
	defer cc.Close()
	defer m.Close()

	if _, err := m.Start(ctx, "main", &fake.Source{Duration: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for m.State("main") != api.SessionStateIdle {
		if time.Now().After(deadline) {
			t.Fatal("the session didn't end")
		}
		time.Sleep(5 * time.Millisecond)
	}
	finals, _, err := st.ListCaptions(ctx, domain.CaptionQuery{SessionID: "main", Track: "es", Limit: 100})
	if err != nil || len(finals) == 0 {
		t.Fatalf("no Spanish finals: %v", err)
	}
	for len(posted()) < len(finals) {
		if time.Now().After(deadline) {
			t.Fatalf("%d posts, want one per Spanish final (%d)", len(posted()), len(finals))
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i, body := range posted() {
		if want := strings.ReplaceAll(finals[i].Text, " ", ""); !strings.Contains(strings.ReplaceAll(strings.ReplaceAll(body, "<br>", ""), " ", ""), want) {
			t.Errorf("post %d = %q, want the Spanish final %q", i, body, finals[i].Text)
		}
	}
	status := m.StatusOf("main")
	if status.StreamCaptions == nil || status.StreamCaptions.LastSeq == nil || *status.StreamCaptions.LastSeq != int64(len(finals)) {
		t.Errorf("session status streamCaptions = %+v, want last seq %d", status.StreamCaptions, len(finals))
	}
}

// memStore is a read-only domain.SecretStore.
type memStore map[string]string

func (m memStore) GetSecret(_ context.Context, name string) (string, domain.SecretSource, error) {
	if v, ok := m[name]; ok {
		return v, domain.SecretFromKeychain, nil
	}
	return "", "", secrets.ErrNotFound
}
func (m memStore) SetSecret(context.Context, string, string) error { return secrets.ErrNoBackend }
func (m memStore) DeleteSecret(context.Context, string) error      { return secrets.ErrNotFound }
func (m memStore) SecretInfo(_ context.Context, name string) (domain.SecretInfo, error) {
	_, ok := m[name]
	return domain.SecretInfo{Name: api.SecretName(name), Set: ok}, nil
}
