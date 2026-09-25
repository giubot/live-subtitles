// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Glossary limits. They keep a glossary small enough for the prompts: the
// translation prompt only carries the entries found in each caption, but
// the ASR vocabulary and whisper's initial prompt take the whole list.
const (
	maxGlossaryName  = 120
	maxGlossaryTerms = 1000
	maxGlossaryEntry = 200 // runes per term, translation, note or do-not-translate entry
)

// glossaryLanguage matches a translation's language code: `es`, `pt-BR`.
var glossaryLanguage = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)

var glossaryNotFound = api.NotFoundJSONResponse{Code: "glossary.not_found", Message: "glossary not found"}

func (s *Server) ListGlossaries(ctx context.Context, _ api.ListGlossariesRequestObject) (api.ListGlossariesResponseObject, error) {
	if s.Glossaries == nil {
		return nil, api.ErrNotImplemented
	}
	list, err := s.Glossaries.ListGlossaries(ctx)
	if err != nil {
		return nil, err
	}
	return api.ListGlossaries200JSONResponse(list), nil
}

func (s *Server) CreateGlossary(ctx context.Context, req api.CreateGlossaryRequestObject) (api.CreateGlossaryResponseObject, error) {
	if s.Glossaries == nil {
		return nil, api.ErrNotImplemented
	}
	in, bad := normalizeGlossary(req.Body)
	if len(bad) > 0 {
		return api.CreateGlossary400JSONResponse{BadRequestJSONResponse: invalidGlossary(bad)}, nil
	}
	g := domain.Glossary{Name: in.Name, Terms: in.Terms, DoNotTranslate: in.DoNotTranslate, UpdatedAt: time.Now().UTC()}
	for range 3 { // a random id collides only by bad luck; try again
		g.Id = newGlossaryID()
		err := s.Glossaries.CreateGlossary(ctx, g)
		if errors.Is(err, domain.ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return api.CreateGlossary201JSONResponse(g), nil
	}
	return nil, errors.New("glossary: no free id")
}

func (s *Server) GetGlossary(ctx context.Context, req api.GetGlossaryRequestObject) (api.GetGlossaryResponseObject, error) {
	if s.Glossaries == nil {
		return nil, api.ErrNotImplemented
	}
	g, err := s.Glossaries.GetGlossary(ctx, req.GlossaryId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.GetGlossary404JSONResponse{NotFoundJSONResponse: glossaryNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetGlossary200JSONResponse(g), nil
}

// UpdateGlossary replaces a glossary. Running sessions keep the copy they
// loaded at start; the change applies from their next start.
func (s *Server) UpdateGlossary(ctx context.Context, req api.UpdateGlossaryRequestObject) (api.UpdateGlossaryResponseObject, error) {
	if s.Glossaries == nil {
		return nil, api.ErrNotImplemented
	}
	in, bad := normalizeGlossary(req.Body)
	if len(bad) > 0 {
		return api.UpdateGlossary400JSONResponse{BadRequestJSONResponse: invalidGlossary(bad)}, nil
	}
	g := domain.Glossary{Id: req.GlossaryId, Name: in.Name, Terms: in.Terms, DoNotTranslate: in.DoNotTranslate, UpdatedAt: time.Now().UTC()}
	err := s.Glossaries.UpdateGlossary(ctx, g)
	if errors.Is(err, domain.ErrNotFound) {
		return api.UpdateGlossary404JSONResponse{NotFoundJSONResponse: glossaryNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.UpdateGlossary200JSONResponse(g), nil
}

// DeleteGlossary removes a glossary and detaches it from the sessions and
// the settings default that name it (the store does both at once). A
// running session keeps the copy it loaded until it stops. Admin
// dashboards get a sessionUpdated event for each detached session.
func (s *Server) DeleteGlossary(ctx context.Context, req api.DeleteGlossaryRequestObject) (api.DeleteGlossaryResponseObject, error) {
	if s.Glossaries == nil {
		return nil, api.ErrNotImplemented
	}
	var users []string
	if s.Sessions != nil {
		list, err := s.Sessions.ListSessions(ctx)
		if err != nil {
			return nil, err
		}
		for _, sess := range list {
			if sess.GlossaryId != nil && *sess.GlossaryId == req.GlossaryId {
				users = append(users, sess.Id)
			}
		}
	}
	err := s.Glossaries.DeleteGlossary(ctx, req.GlossaryId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.DeleteGlossary404JSONResponse{NotFoundJSONResponse: glossaryNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	for _, id := range users {
		sess, err := s.Sessions.GetSession(ctx, id)
		if err != nil {
			continue // deleted meanwhile
		}
		out := s.present(ctx, sess)
		s.sessionEvent(api.AdminEventTypeSessionUpdated, &out, id)
	}
	return api.DeleteGlossary204Response{}, nil
}

// normalizeGlossary trims the input, drops blank translations, notes and
// do-not-translate entries, removes duplicate do-not-translate entries,
// and validates what's left. It returns field → error code; field names
// follow the body (`terms.3.term`).
func normalizeGlossary(b *api.GlossaryInput) (api.GlossaryInput, map[string]string) {
	bad := map[string]string{}
	if b == nil {
		bad["name"] = "glossary.name_required"
		return api.GlossaryInput{}, bad
	}
	out := api.GlossaryInput{Name: strings.TrimSpace(b.Name), Terms: []api.GlossaryTerm{}, DoNotTranslate: []string{}}
	switch n := utf8.RuneCountInString(out.Name); {
	case n == 0:
		bad["name"] = "glossary.name_required"
	case n > maxGlossaryName:
		bad["name"] = "glossary.too_large"
	}
	if len(b.Terms) > maxGlossaryTerms {
		bad["terms"] = "glossary.too_large"
	}
	if len(b.DoNotTranslate) > maxGlossaryTerms {
		bad["doNotTranslate"] = "glossary.too_large"
	}
	if bad["terms"] != "" || bad["doNotTranslate"] != "" {
		return out, bad
	}
	long := func(s string) bool { return utf8.RuneCountInString(s) > maxGlossaryEntry }

	seen := map[string]int{}
	for i, t := range b.Terms {
		field := fmt.Sprintf("terms.%d", i)
		term := api.GlossaryTerm{Term: strings.TrimSpace(t.Term)}
		key := strings.ToLower(term.Term)
		switch _, dup := seen[key]; {
		case term.Term == "":
			bad[field+".term"] = "glossary.term_required"
		case long(term.Term):
			bad[field+".term"] = "glossary.too_large"
		case dup:
			bad[field+".term"] = "glossary.term_duplicate"
		default:
			seen[key] = i
		}
		if t.Translations != nil {
			tr := map[string]string{}
			for lang, text := range *t.Translations {
				text = strings.TrimSpace(text)
				switch {
				case !glossaryLanguage.MatchString(lang):
					bad[field+".translations."+lang] = "glossary.language_invalid"
				case long(text):
					bad[field+".translations."+lang] = "glossary.too_large"
				case text != "":
					tr[lang] = text
				}
			}
			if len(tr) > 0 {
				term.Translations = &tr
			}
		}
		if t.Note != nil {
			switch note := strings.TrimSpace(*t.Note); {
			case long(note):
				bad[field+".note"] = "glossary.too_large"
			case note != "":
				term.Note = &note
			}
		}
		out.Terms = append(out.Terms, term)
	}

	kept := map[string]bool{}
	for i, k := range b.DoNotTranslate {
		k = strings.TrimSpace(k)
		switch {
		case k == "" || kept[strings.ToLower(k)]:
		case long(k):
			bad[fmt.Sprintf("doNotTranslate.%d", i)] = "glossary.too_large"
		default:
			kept[strings.ToLower(k)] = true
			out.DoNotTranslate = append(out.DoNotTranslate, k)
		}
	}
	return out, bad
}

// invalidGlossary is the 400 body: the fields' shared code, or
// glossary.invalid when they differ.
func invalidGlossary(fields map[string]string) api.BadRequestJSONResponse {
	code := ""
	for _, c := range fields {
		if code != "" && c != code {
			code = "glossary.invalid"
			break
		}
		code = c
	}
	return api.BadRequestJSONResponse{Code: code, Message: "invalid glossary", Fields: &fields}
}

// newGlossaryID returns a random id like g-3fa9c1d2e4b0.
func newGlossaryID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return "g-" + hex.EncodeToString(b)
}

// checkGlossaryRef adds field → glossary.not_found to bad when id names a
// glossary that doesn't exist. A nil or empty id means no glossary, and
// without a glossary store nothing can be checked.
func (s *Server) checkGlossaryRef(ctx context.Context, id *string, field string, bad map[string]string) error {
	if id == nil || *id == "" || s.Glossaries == nil {
		return nil
	}
	_, err := s.Glossaries.GetGlossary(ctx, *id)
	if errors.Is(err, domain.ErrNotFound) {
		bad[field] = glossaryNotFound.Code
		return nil
	}
	return err
}
