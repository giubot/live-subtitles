// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "nested", "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func ptr[T any](v T) *T { return &v }

var t0 = time.Date(2026, 9, 25, 13, 30, 0, 0, time.UTC)

func session(id string, created time.Time) domain.Session {
	return domain.Session{
		Id: id, Name: "Session " + id, CreatedAt: created, UpdatedAt: created,
		Provider: "default", EffectiveProvider: "mock", SourceLanguage: "auto",
		State: api.SessionStateIdle, TargetLanguages: []string{"es", "en"}, Room: ptr("Sala Konex"),
	}
}

func TestOpen(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "a", "b", "live.db")
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	// Reopening must not re-apply anything, and data must survive.
	for i := range 3 {
		s, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if i == 0 {
			if err := s.CreateSession(ctx, session("main", t0)); err != nil {
				t.Fatal(err)
			}
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != len(ms) {
			t.Errorf("open %d: %d migrations recorded, want %d", i, n, len(ms))
		}
		if _, err := s.GetSession(ctx, "main"); err != nil {
			t.Errorf("open %d: %v", i, err)
		}
		for pragma, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000"} {
			var got string
			if err := s.db.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s = %s, want %s", pragma, got, want)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessions(t *testing.T) {
	ctx := t.Context()
	s := openTest(t)
	// Ids sort opposite to creation time, which is the list order.
	a, b := session("b-first", t0), session("a-second", t0.Add(time.Minute))
	for _, sess := range []domain.Session{b, a} {
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}

	updated := a
	updated.Name, updated.State = "Renamed", api.SessionStateLive

	tests := []struct {
		name string
		op   func() error
		want error
	}{
		{"create duplicate", func() error { return s.CreateSession(ctx, a) }, domain.ErrConflict},
		{"get missing", func() error { _, err := s.GetSession(ctx, "nope"); return err }, domain.ErrNotFound},
		{"update", func() error { return s.UpdateSession(ctx, updated) }, nil},
		{"update missing", func() error { return s.UpdateSession(ctx, session("nope", t0)) }, domain.ErrNotFound},
		{"delete missing", func() error { return s.DeleteSession(ctx, "nope") }, domain.ErrNotFound},
		{"token hash of missing", func() error { _, err := s.IngestTokenHash(ctx, "nope"); return err }, domain.ErrNotFound},
		{"set token hash of missing", func() error { return s.SetIngestTokenHash(ctx, "nope", "h") }, domain.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.op(); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}

	got, err := s.GetSession(ctx, a.Id)
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Errorf("GetSession = %+v, %v; want %+v", got, err, updated)
	}
	list, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Id != a.Id || list[1].Id != b.Id {
		t.Errorf("ListSessions order = %v, want [%s %s]", list, a.Id, b.Id)
	}

	// The ingest token hash is independent of the session document.
	if h, err := s.IngestTokenHash(ctx, a.Id); err != nil || h != "" {
		t.Errorf("initial hash = %q, %v", h, err)
	}
	if err := s.SetIngestTokenHash(ctx, a.Id, "argon2id$x"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSession(ctx, a); err != nil {
		t.Fatal(err)
	}
	if h, err := s.IngestTokenHash(ctx, a.Id); err != nil || h != "argon2id$x" {
		t.Errorf("hash after update = %q, %v", h, err)
	}

	if err := s.DeleteSession(ctx, a.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, a.Id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
}

func caption(session, track, seg string, start float32) domain.CaptionEvent {
	return domain.CaptionEvent{
		SessionId: session, Lang: track, SegmentId: seg, Start: start, End: start + 1.5,
		Text: fmt.Sprintf("%s %s", track, seg), SourceLang: "es", Final: true,
	}
}

func TestSaveCaption(t *testing.T) {
	ctx := t.Context()
	s := openTest(t)
	if err := s.CreateSession(ctx, session("main", t0)); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveCaption(ctx, caption("nope", "es", "s-1", 0)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing session: err = %v", err)
	}

	first := caption("main", "es", "s-1", 1.25)
	first.LatencyMs = ptr(900)
	edited := first
	edited.Text, edited.Edited, edited.Hidden, edited.LatencyMs = "corrected", ptr(true), ptr(true), nil
	other := caption("main", "en", "s-1", 1.25)
	for _, c := range []domain.CaptionEvent{first, other, edited} {
		if err := s.SaveCaption(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	got, next, err := s.ListCaptions(ctx, domain.CaptionQuery{SessionID: "main", Track: "es"})
	if err != nil || next != "" {
		t.Fatalf("ListCaptions: next %q, err %v", next, err)
	}
	if want := []domain.CaptionEvent{edited}; !reflect.DeepEqual(got, want) {
		t.Errorf("upserted = %+v, want %+v", got, want)
	}

	// Deleting the session removes its captions.
	if err := s.DeleteSession(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captions`).Scan(&n); err != nil || n != 0 {
		t.Errorf("captions after session delete = %d, %v", n, err)
	}
}

func TestEditCaption(t *testing.T) {
	ctx := t.Context()
	s := openTest(t)
	if err := s.CreateSession(ctx, session("main", t0)); err != nil {
		t.Fatal(err)
	}
	for _, track := range []string{"es", "en"} {
		if err := s.SaveCaption(ctx, caption("main", track, "s-1", 1)); err != nil {
			t.Fatal(err)
		}
	}

	// The cases run in order: each edits the result of the previous one.
	tests := []struct {
		name                 string
		session, track, seg  string
		edit                 domain.CaptionEdit
		wantErr              error
		wantText             string
		wantHidden, wantEdit *bool
	}{
		{"missing session", "nope", "es", "s-1", domain.CaptionEdit{Text: ptr("x")}, domain.ErrNotFound, "", nil, nil},
		{"missing track", "main", "fr", "s-1", domain.CaptionEdit{Text: ptr("x")}, domain.ErrNotFound, "", nil, nil},
		{"missing segment", "main", "es", "s-9", domain.CaptionEdit{Text: ptr("x")}, domain.ErrNotFound, "", nil, nil},
		{"text", "main", "es", "s-1", domain.CaptionEdit{Text: ptr("corregido")}, nil, "corregido", nil, ptr(true)},
		{"hide keeps text", "main", "es", "s-1", domain.CaptionEdit{Hidden: ptr(true)}, nil, "corregido", ptr(true), ptr(true)},
		{"unhide", "main", "es", "s-1", domain.CaptionEdit{Hidden: ptr(false)}, nil, "corregido", nil, ptr(true)},
		{"both", "main", "es", "s-1", domain.CaptionEdit{Text: ptr("otra"), Hidden: ptr(true)}, nil, "otra", ptr(true), ptr(true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.EditCaption(ctx, tt.session, tt.track, tt.seg, tt.edit)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Text != tt.wantText || !reflect.DeepEqual(got.Hidden, tt.wantHidden) || !reflect.DeepEqual(got.Edited, tt.wantEdit) {
				t.Errorf("got text %q hidden %v edited %v", got.Text, got.Hidden, got.Edited)
			}
			items, _, err := s.ListCaptions(ctx, domain.CaptionQuery{SessionID: "main", Track: "es"})
			if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0], got) {
				t.Errorf("stored = %+v, %v; want %+v", items, err, got)
			}
		})
	}

	// A provider re-final of the segment keeps the operator's correction
	// but takes the new timing; the other track is untouched.
	if err := s.SaveCaption(ctx, caption("main", "es", "s-1", 2)); err != nil {
		t.Fatal(err)
	}
	items, _, err := s.ListCaptions(ctx, domain.CaptionQuery{SessionID: "main"})
	if err != nil || len(items) != 2 {
		t.Fatalf("ListCaptions = %+v, %v", items, err)
	}
	en, es := items[0], items[1]
	if es.Text != "otra" || !isTrue(es.Hidden) || !isTrue(es.Edited) || es.Start != 2 {
		t.Errorf("after re-final: %+v", es)
	}
	if en.Text != "en s-1" || en.Edited != nil {
		t.Errorf("other track changed: %+v", en)
	}
}

func TestListCaptions(t *testing.T) {
	ctx := t.Context()
	s := openTest(t)
	for _, id := range []string{"main", "other"} {
		if err := s.CreateSession(ctx, session(id, t0)); err != nil {
			t.Fatal(err)
		}
	}
	// Saved out of order; s-3 and s-3b share a start time.
	starts := map[string]float32{"s-4": 30.5, "s-1": 0.1, "s-3": 20, "s-3b": 20, "s-2": 10.7}
	for seg, start := range starts {
		for _, track := range []string{"es", "en"} {
			if err := s.SaveCaption(ctx, caption("main", track, seg, start)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.SaveCaption(ctx, caption("other", "es", "s-0", 0)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		track    string
		limit    int
		from, to time.Duration
		want     []string // track/segment in order
	}{
		{"one track, one per page", "es", 1, 0, 0, []string{"es/s-1", "es/s-2", "es/s-3", "es/s-3b", "es/s-4"}},
		{"one track, two per page", "es", 2, 0, 0, []string{"es/s-1", "es/s-2", "es/s-3", "es/s-3b", "es/s-4"}},
		{"exact fit", "en", 5, 0, 0, []string{"en/s-1", "en/s-2", "en/s-3", "en/s-3b", "en/s-4"}},
		{"default limit", "en", 0, 0, 0, []string{"en/s-1", "en/s-2", "en/s-3", "en/s-3b", "en/s-4"}},
		{"all tracks", "", 3, 0, 0, []string{
			"en/s-1", "es/s-1", "en/s-2", "es/s-2", "en/s-3", "en/s-3b", "es/s-3", "es/s-3b", "en/s-4", "es/s-4",
		}},
		{"unknown track", "fr", 2, 0, 0, nil},
		{"window", "es", 1, 10 * time.Second, 30 * time.Second, []string{"es/s-2", "es/s-3", "es/s-3b"}},
		{"window from a start", "es", 0, 20 * time.Second, time.Hour, []string{"es/s-3", "es/s-3b", "es/s-4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			cursor, pages := "", 0
			for {
				items, next, err := s.ListCaptions(ctx, domain.CaptionQuery{SessionID: "main", Track: tt.track, Cursor: cursor, Limit: tt.limit, From: tt.from, To: tt.to})
				if err != nil {
					t.Fatal(err)
				}
				if tt.limit > 0 && len(items) > tt.limit {
					t.Fatalf("page of %d items, limit %d", len(items), tt.limit)
				}
				for _, c := range items {
					got = append(got, c.Lang+"/"+c.SegmentId)
				}
				if pages++; next == "" || pages > 20 {
					break
				}
				cursor = next
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	if _, _, err := s.ListCaptions(ctx, domain.CaptionQuery{SessionID: "main", Cursor: "!!"}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("bad cursor: err = %v", err)
	}
}

func TestConcurrentWrites(t *testing.T) {
	ctx := t.Context()
	s := openTest(t)
	if err := s.CreateSession(ctx, session("main", t0)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for w := range 8 {
		wg.Go(func() {
			for i := range 25 {
				c := caption("main", "es", fmt.Sprintf("w%d-%d", w, i), float32(i))
				if err := s.SaveCaption(ctx, c); err != nil {
					errs <- err
				}
				if _, _, err := s.ListCaptions(ctx, domain.CaptionQuery{SessionID: "main", Limit: 10}); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	items, _, err := s.ListCaptions(ctx, domain.CaptionQuery{SessionID: "main", Limit: MaxCaptionLimit})
	if err != nil || len(items) != 200 {
		t.Errorf("stored %d captions, err %v; want 200", len(items), err)
	}
}

func TestSettingsAndAdmin(t *testing.T) {
	ctx := t.Context()
	s := openTest(t)

	if _, err := s.Settings(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unset settings: err = %v", err)
	}
	var v api.Settings
	v.DefaultSourceLanguage = "auto"
	v.DefaultTargetLanguages = []string{"es", "en"}
	v.Recording.EnabledByDefault = true
	v.Providers.Local.OllamaUrl = "http://localhost:11434"
	for i := range 2 { // second put replaces
		v.Recording.RetentionDays = i + 7
		if err := s.PutSettings(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.Settings(ctx); err != nil || !reflect.DeepEqual(got, v) {
		t.Errorf("Settings = %+v, %v; want %+v", got, err, v)
	}

	if _, err := s.AdminPINHash(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unset PIN: err = %v", err)
	}
	if err := s.InitAdminPINHash(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if err := s.InitAdminPINHash(ctx, "h2"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("second init: err = %v", err)
	}
	if err := s.SetAdminPINHash(ctx, "h3"); err != nil {
		t.Fatal(err)
	}
	if h, err := s.AdminPINHash(ctx); err != nil || h != "h3" {
		t.Errorf("AdminPINHash = %q, %v", h, err)
	}
}

// crud runs the shared create/get/list/update/delete contract over one of
// the document tables.
type crud[T any] struct {
	create, update func(context.Context, T) error
	get            func(context.Context, string) (T, error)
	list           func(context.Context) ([]T, error)
	del            func(context.Context, string) error
}

func (c crud[T]) run(t *testing.T, a, b, aChanged, missing T, idA, idB string) {
	t.Helper()
	ctx := t.Context()
	for _, v := range []T{a, b} {
		if err := c.create(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.create(ctx, a); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate create: err = %v", err)
	}
	if err := c.update(ctx, aChanged); err != nil {
		t.Fatal(err)
	}
	if err := c.update(ctx, missing); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("update missing: err = %v", err)
	}
	if got, err := c.get(ctx, idA); err != nil || !reflect.DeepEqual(got, aChanged) {
		t.Errorf("get = %+v, %v; want %+v", got, err, aChanged)
	}
	if list, err := c.list(ctx); err != nil || !reflect.DeepEqual(list, []T{aChanged, b}) {
		t.Errorf("list = %+v, %v", list, err)
	}
	if err := c.del(ctx, idB); err != nil {
		t.Fatal(err)
	}
	if err := c.del(ctx, idB); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("delete missing: err = %v", err)
	}
	if _, err := c.get(ctx, idB); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get deleted: err = %v", err)
	}
}

func TestGlossaries(t *testing.T) {
	s := openTest(t)
	// The seed glossary would sit in the listing; the CRUD contract runs
	// on an otherwise empty table.
	if err := s.DeleteGlossary(t.Context(), SeedGlossaryID); err != nil {
		t.Fatal(err)
	}
	a := domain.Glossary{Id: "g1", Name: "Alpha", UpdatedAt: t0, DoNotTranslate: []string{"Kubernetes"},
		Terms: []api.GlossaryTerm{{Term: "clúster", Note: ptr("infra"), Translations: &map[string]string{"en": "cluster"}}}}
	b := domain.Glossary{Id: "g0", Name: "Beta", UpdatedAt: t0, DoNotTranslate: []string{}, Terms: []api.GlossaryTerm{}}
	changed := a
	changed.DoNotTranslate = []string{"Kubernetes", "React"}
	crud[domain.Glossary]{s.CreateGlossary, s.UpdateGlossary, s.GetGlossary, s.ListGlossaries, s.DeleteGlossary}.
		run(t, a, b, changed, domain.Glossary{Id: "nope"}, "g1", "g0")
}

func TestOverlayPresets(t *testing.T) {
	s := openTest(t)
	a := api.OverlayPreset{Id: "p1", Name: "Lower third", Style: api.OverlayStyle{FontSizePx: ptr(42), ShowInterim: ptr(true)}}
	b := api.OverlayPreset{Id: "p0", Name: "Top bar"}
	changed := a
	changed.Style.Background = ptr("transparent")
	crud[api.OverlayPreset]{s.CreateOverlayPreset, s.UpdateOverlayPreset, s.GetOverlayPreset, s.ListOverlayPresets, s.DeleteOverlayPreset}.
		run(t, a, b, changed, api.OverlayPreset{Id: "nope"}, "p1", "p0")
}

func TestRecordings(t *testing.T) {
	s := openTest(t)
	a := domain.Recording{Id: "r1", SessionId: "main", StartedAt: t0.Add(time.Hour), Status: api.RecordingStatusRecording, Languages: []string{"es"}}
	b := domain.Recording{Id: "r0", SessionId: "main", StartedAt: t0, Status: api.RecordingStatusComplete, Languages: []string{"en"}}
	changed := a
	changed.Status, changed.EndedAt, changed.SizeBytes = api.RecordingStatusComplete, ptr(t0.Add(2*time.Hour)), ptr(int64(1<<20))
	listAll := func(ctx context.Context) ([]domain.Recording, error) { return s.ListRecordings(ctx, "") }
	crud[domain.Recording]{s.CreateRecording, s.UpdateRecording, s.GetRecording, listAll, s.DeleteRecording}.
		run(t, a, b, changed, domain.Recording{Id: "nope"}, "r1", "r0")

	ctx := t.Context()
	if err := s.CreateRecording(ctx, domain.Recording{Id: "r2", SessionId: "other", StartedAt: t0, Languages: []string{}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListRecordings(ctx, "other")
	if err != nil || len(got) != 1 || got[0].Id != "r2" {
		t.Errorf("ListRecordings(other) = %+v, %v", got, err)
	}
}

func TestSeedGlossary(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "live.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.GetGlossary(ctx, SeedGlossaryID)
	if err != nil {
		t.Fatal(err)
	}
	if g.Id != SeedGlossaryID || g.Name == "" || g.UpdatedAt.IsZero() {
		t.Errorf("seed = %+v", g)
	}
	has := func(lang string) bool {
		for _, term := range g.Terms {
			if term.Translations != nil && (*term.Translations)[lang] != "" {
				return true
			}
		}
		return false
	}
	if len(g.Terms) < 10 || !has("es") || !has("en") || !slices.Contains(g.DoNotTranslate, "Kubernetes") {
		t.Errorf("seed lacks EN and ES terms or do-not-translate entries: %+v", g)
	}
	// A deleted seed stays deleted: the migration ran once.
	if err := s.DeleteGlossary(ctx, SeedGlossaryID); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.GetGlossary(ctx, SeedGlossaryID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("seed after delete and reopen: err = %v", err)
	}
}

func TestDeleteGlossaryDetaches(t *testing.T) {
	ctx := t.Context()
	s := openTest(t)
	past := t0.AddDate(-5, 0, 0) // before the wall clock the delete stamps
	g := domain.Glossary{Id: "g1", Name: "Talks", UpdatedAt: t0, Terms: []api.GlossaryTerm{}, DoNotTranslate: []string{}}
	if err := s.CreateGlossary(ctx, g); err != nil {
		t.Fatal(err)
	}
	for id, glossary := range map[string]*string{"uses": ptr("g1"), "other": ptr(SeedGlossaryID), "none": nil} {
		sess := session(id, past)
		sess.GlossaryId = glossary
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PutSettings(ctx, api.Settings{DefaultGlossaryId: ptr("g1"), DefaultTargetLanguages: []string{"es"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGlossary(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id       string
		glossary *string
		touched  bool
	}{
		{"uses", nil, true},
		{"other", ptr(SeedGlossaryID), false},
		{"none", nil, false},
	}
	for _, tc := range tests {
		got, err := s.GetSession(ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.GlossaryId, tc.glossary) {
			t.Errorf("%s: glossaryId = %v, want %v", tc.id, got.GlossaryId, tc.glossary)
		}
		if touched := got.UpdatedAt.After(past); touched != tc.touched {
			t.Errorf("%s: updatedAt %v, touched = %v, want %v", tc.id, got.UpdatedAt, touched, tc.touched)
		}
		if got.Name != "Session "+tc.id {
			t.Errorf("%s: other fields changed: %+v", tc.id, got)
		}
	}
	st, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.DefaultGlossaryId != nil || len(st.DefaultTargetLanguages) != 1 {
		t.Errorf("settings = %+v", st)
	}
	if err := s.DeleteGlossary(ctx, "g1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("delete again: err = %v", err)
	}
}
