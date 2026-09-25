// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/store"
)

func glossaryServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New()
	s.Glossaries, s.Sessions = st, st
	return st, s.Handler(http.NewServeMux(), slog.New(slog.DiscardHandler))
}

func TestGlossaryValidation(t *testing.T) {
	_, h := glossaryServer(t)
	long := strings.Repeat("x", maxGlossaryEntry+1)
	for _, tc := range []struct {
		name, body, want string
	}{
		{"no name", `{"name":"  ","terms":[],"doNotTranslate":[]}`, `"fields":{"name":"glossary.name_required"}`},
		{"blank term", `{"name":"a","terms":[{"term":" "}],"doNotTranslate":[]}`, `"fields":{"terms.0.term":"glossary.term_required"}`},
		{"duplicate term", `{"name":"a","terms":[{"term":"React"},{"term":"react "}],"doNotTranslate":[]}`, `"terms.1.term":"glossary.term_duplicate"`},
		{"bad language", `{"name":"a","terms":[{"term":"x","translations":{"Spanish":"y"}}],"doNotTranslate":[]}`, `"code":"glossary.language_invalid"`},
		{"long entry", `{"name":"a","terms":[],"doNotTranslate":["` + long + `"]}`, `"doNotTranslate.0":"glossary.too_large"`},
		{"mixed", `{"name":"","terms":[{"term":""}],"doNotTranslate":[]}`, `"code":"glossary.invalid"`},
		// Missing lists are read as empty ones.
		{"missing lists", `{"name":"a"}`, `"terms":[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := 400
			if !strings.Contains(tc.want, "glossary.") {
				status = 201
			}
			call{"POST", "/api/glossaries", tc.body, "", "", status, tc.want}.do(t, h)
		})
	}
}

func TestGlossaryCRUD(t *testing.T) {
	st, h := glossaryServer(t)
	ctx := t.Context()

	res := call{"POST", "/api/glossaries", `{"name":" Talks ","terms":[
		{"term":" clúster ","translations":{"en":"cluster","pt":" "},"note":" "},
		{"term":"deploy","note":"verb"}],
		"doNotTranslate":["Kubernetes"," kubernetes","","React"]}`, "", "", 201, `"name":"Talks"`}.do(t, h)
	var g api.Glossary
	if err := json.NewDecoder(res.Body).Decode(&g); err != nil {
		t.Fatal(err)
	}
	want := []api.GlossaryTerm{
		{Term: "clúster", Translations: &map[string]string{"en": "cluster"}},
		{Term: "deploy", Note: ptrTo("verb")},
	}
	if !strings.HasPrefix(g.Id, "g-") || !reflect.DeepEqual(g.Terms, want) ||
		!reflect.DeepEqual(g.DoNotTranslate, []string{"Kubernetes", "React"}) || g.UpdatedAt.IsZero() {
		t.Errorf("created %+v", g)
	}
	stored, err := st.GetGlossary(ctx, g.Id)
	if err != nil || !reflect.DeepEqual(stored.Terms, want) {
		t.Errorf("stored %+v, %v", stored, err)
	}

	now := time.Now()
	sess := domain.Session{Id: "main", Name: "Main", CreatedAt: now, UpdatedAt: now, GlossaryId: &g.Id,
		SourceLanguage: api.Auto, TargetLanguages: []string{"es"}, Provider: api.ProviderChoiceDefault}
	if err := st.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	path := "/api/glossaries/" + g.Id
	for _, c := range []call{
		{"GET", "/api/glossaries", "", "", "", 200, `"id":"` + g.Id + `"`},
		{"GET", "/api/glossaries", "", "", "", 200, `"id":"` + store.SeedGlossaryID + `"`},
		{"GET", path, "", "", "", 200, `"doNotTranslate":["Kubernetes","React"]`},
		{"GET", "/api/glossaries/nope", "", "", "", 404, `"code":"glossary.not_found"`},
		{"PUT", path, `{"name":"Renamed","terms":[],"doNotTranslate":["Go"]}`, "", "", 200, `"name":"Renamed"`},
		{"PUT", "/api/glossaries/nope", `{"name":"x","terms":[],"doNotTranslate":[]}`, "", "", 404, `"code":"glossary.not_found"`},
		{"PUT", path, `{"name":"","terms":[],"doNotTranslate":[]}`, "", "", 400, `"code":"glossary.name_required"`},
		{"GET", path, "", "", "", 200, `"terms":[]`},
		{"DELETE", path, "", "", "", 204, ""},
		{"DELETE", path, "", "", "", 404, `"code":"glossary.not_found"`},
		{"GET", path, "", "", "", 404, `"code":"glossary.not_found"`},
	} {
		c.do(t, h)
	}
	got, err := st.GetSession(ctx, "main")
	if err != nil || got.GlossaryId != nil {
		t.Errorf("session after glossary delete: %+v, %v", got.GlossaryId, err)
	}
}

func ptrTo[T any](v T) *T { return &v }
