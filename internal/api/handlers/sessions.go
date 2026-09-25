// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/audio/ffmpeg"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/session"
)

var sessionNotFound = api.NotFoundJSONResponse{Code: "session.not_found", Message: "session not found"}

func (s *Server) RotateIngestToken(ctx context.Context, req api.RotateIngestTokenRequestObject) (api.RotateIngestTokenResponseObject, error) {
	if s.Auth == nil {
		return nil, api.ErrNotImplemented
	}
	token, err := s.Auth.RotateIngestToken(ctx, req.SessionId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.RotateIngestToken404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.RotateIngestToken200JSONResponse{IngestToken: token}, nil
}

// stateConflict answers an operation the session's state doesn't allow.
func (s *Server) stateConflict(id string) api.ConflictJSONResponse {
	return api.ConflictJSONResponse{Code: "session.state_conflict", Message: "not allowed in the session's current state",
		Params: &map[string]any{"state": s.Manager.State(id)}}
}

func (s *Server) StartSession(ctx context.Context, req api.StartSessionRequestObject) (api.StartSessionResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	if req.Body != nil && req.Body.Source != nil && *req.Body.Source != api.AudioSourceKindBrowser {
		// File audio has its own endpoint (sources/file); SRT and device
		// capture come later (P3-11, P5-08).
		return api.StartSession422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse{
			Code: "source.unsupported", Message: "start this source from its own endpoint, or use browser capture",
			Params: &map[string]any{"source": *req.Body.Source},
		}}, nil
	}
	st, err := s.Manager.Start(ctx, req.SessionId, nil)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.StartSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case errors.Is(err, session.ErrState):
		return api.StartSession409JSONResponse{ConflictJSONResponse: s.stateConflict(req.SessionId)}, nil
	case errors.Is(err, session.ErrUnavailable):
		e := api.Error{Code: session.CodeProviderUnavailable, Message: err.Error()}
		if st.Error != nil {
			e = *st.Error
		}
		return api.StartSession422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse(e)}, nil
	case err != nil:
		return nil, err
	}
	return api.StartSession200JSONResponse(st), nil
}

func (s *Server) PauseSession(ctx context.Context, req api.PauseSessionRequestObject) (api.PauseSessionResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	st, err := s.Manager.Pause(ctx, req.SessionId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.PauseSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case errors.Is(err, session.ErrState):
		return api.PauseSession409JSONResponse{ConflictJSONResponse: s.stateConflict(req.SessionId)}, nil
	case err != nil:
		return nil, err
	}
	return api.PauseSession200JSONResponse(st), nil
}

func (s *Server) StopSession(ctx context.Context, req api.StopSessionRequestObject) (api.StopSessionResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	st, err := s.Manager.Stop(ctx, req.SessionId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.StopSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case err != nil:
		return nil, err
	}
	return api.StopSession200JSONResponse(st), nil
}

func (s *Server) GetSessionStatus(ctx context.Context, req api.GetSessionStatusRequestObject) (api.GetSessionStatusResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	st, err := s.Manager.Status(ctx, req.SessionId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.GetSessionStatus404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case err != nil:
		return nil, err
	}
	return api.GetSessionStatus200JSONResponse(st), nil
}

func (s *Server) StartFileSource(ctx context.Context, req api.StartFileSourceRequestObject) (api.StartFileSourceResponseObject, error) {
	if s.Manager == nil || s.Files == nil {
		return nil, api.ErrNotImplemented
	}
	badRequest := func(code, msg string) api.StartFileSource400JSONResponse {
		return api.StartFileSource400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: code, Message: msg, Fields: &map[string]string{"uri": code},
		}}
	}
	if req.Body == nil {
		return badRequest("source.file_not_allowed", "a uri is required"), nil
	}
	in := ffmpeg.FileInput{URI: req.Body.Uri}
	if req.Body.Loop != nil {
		in.Loop = *req.Body.Loop
	}
	if req.Body.StartAtSec != nil {
		if *req.Body.StartAtSec < 0 {
			return api.StartFileSource400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
				Code: "request.invalid", Message: "startAtSec must not be negative",
				Fields: &map[string]string{"startAtSec": "request.invalid"},
			}}, nil
		}
		in.StartAt = time.Duration(float64(*req.Body.StartAtSec) * float64(time.Second))
	}
	src, err := s.Files.Open(in)
	switch {
	case errors.Is(err, ffmpeg.ErrNotFound):
		return badRequest("source.file_not_found", err.Error()), nil
	case errors.Is(err, ffmpeg.ErrNotAllowed):
		return badRequest("source.file_not_allowed", err.Error()), nil
	case err != nil:
		return nil, err
	}
	st, err := s.Manager.Start(ctx, req.SessionId, src)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return api.StartFileSource404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	case errors.Is(err, session.ErrState):
		return api.StartFileSource409JSONResponse{ConflictJSONResponse: s.stateConflict(req.SessionId)}, nil
	case errors.Is(err, session.ErrUnavailable):
		e := api.Error{Code: session.CodeSourceUnavailable, Message: err.Error()}
		if st.Error != nil {
			e = *st.Error
		}
		return api.StartFileSource422JSONResponse{UnprocessableJSONResponse: api.UnprocessableJSONResponse(e)}, nil
	case err != nil:
		return nil, err
	}
	return api.StartFileSource202JSONResponse(st), nil
}

// StopFileSource stops the session if a file source feeds it.
func (s *Server) StopFileSource(ctx context.Context, req api.StopFileSourceRequestObject) (api.StopFileSourceResponseObject, error) {
	if s.Manager == nil {
		return nil, api.ErrNotImplemented
	}
	if _, err := s.Manager.Status(ctx, req.SessionId); errors.Is(err, domain.ErrNotFound) {
		return api.StopFileSource404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	} else if err != nil {
		return nil, err
	}
	if kind, ok := s.Manager.SourceKind(req.SessionId); !ok || kind != api.AudioSourceKindFile {
		return api.StopFileSource404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse{
			Code: "source.not_running", Message: "no file source is running for this session",
		}}, nil
	}
	if _, err := s.Manager.Stop(ctx, req.SessionId); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	return api.StopFileSource204Response{}, nil
}

// ── Session CRUD (SES-1) ──

var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	languagePattern = regexp.MustCompile(`^[a-z]{2}$`)
)

// sessionDefaults are the settings a new session starts from when the
// request leaves them out.
type sessionDefaults struct {
	source    api.SourceLanguage
	targets   []api.LanguageCode
	glossary  *string
	recording bool
}

func (s *Server) sessionDefaults(ctx context.Context) sessionDefaults {
	d := sessionDefaults{source: api.Auto, targets: []api.LanguageCode{"es", "en"}, recording: true}
	if s.Settings == nil {
		return d
	}
	st, err := s.Settings.Settings(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			s.log.WarnContext(ctx, "read settings; using default session settings", "err", err)
		}
		return d
	}
	if st.DefaultSourceLanguage != "" {
		d.source = st.DefaultSourceLanguage
	}
	if len(st.DefaultTargetLanguages) > 0 {
		d.targets = st.DefaultTargetLanguages
	}
	d.glossary = st.DefaultGlossaryId
	d.recording = st.Recording.EnabledByDefault
	return d
}

// validateSession checks what the generated router doesn't: patterns,
// lengths and enums. It returns field → error code.
func validateSession(b api.SessionBase, requireName bool) map[string]string {
	bad := map[string]string{}
	if b.Name != nil || requireName {
		if n := utf8.RuneCountInString(strings.TrimSpace(deref(b.Name))); n < 1 || n > 120 {
			bad["name"] = "request.invalid"
		}
	}
	if b.Room != nil && utf8.RuneCountInString(*b.Room) > 120 {
		bad["room"] = "request.invalid"
	}
	if b.SourceLanguage != nil {
		switch sl := *b.SourceLanguage; {
		case !languagePattern.MatchString(string(sl)) && sl != api.Auto:
			bad["sourceLanguage"] = "request.invalid"
		case !domain.SupportedSource(sl):
			bad["sourceLanguage"] = codeInvalidLanguage
		}
	}
	if b.TargetLanguages != nil {
		langs := *b.TargetLanguages
		seen := map[string]bool{}
		wellFormed, supported := len(langs) > 0, true
		for _, l := range langs {
			wellFormed = wellFormed && languagePattern.MatchString(l) && !seen[l]
			supported = supported && domain.SupportedTarget(l)
			seen[l] = true
		}
		switch {
		case !wellFormed:
			bad["targetLanguages"] = "request.invalid"
		case !supported:
			bad["targetLanguages"] = codeInvalidLanguage
		}
	}
	if b.Provider != nil && !b.Provider.Valid() {
		bad["provider"] = "request.invalid"
	}
	if st := b.StageStyle; st != nil {
		if (st.Preset != nil && !st.Preset.Valid()) || (st.Lines != nil && (*st.Lines < 1 || *st.Lines > 5)) {
			bad["stageStyle"] = "request.invalid"
		}
	}
	if sc := b.StreamCaptions; sc != nil {
		if (sc.Target != nil && !sc.Target.Valid()) ||
			(sc.MaxCharsPerLine != nil && (*sc.MaxCharsPerLine < 20 || *sc.MaxCharsPerLine > 64)) {
			bad["streamCaptions"] = "request.invalid"
		}
	}
	return bad
}

// codeInvalidLanguage: a well-formed language code that isn't in the
// catalog (GET /api/languages), or can't be the source language.
const codeInvalidLanguage = "session.invalid_language"

// invalidSession answers 400 with the bad fields. When every bad field
// has the same specific code (a bad slug, an unsupported language), that
// code is the response's code; otherwise it's request.invalid.
func invalidSession(fields map[string]string) api.BadRequestJSONResponse {
	code := ""
	for _, c := range fields {
		if code != "" && c != code {
			code = "request.invalid"
			break
		}
		code = c
	}
	if code == "" {
		code = "request.invalid"
	}
	return api.BadRequestJSONResponse{Code: code, Message: "invalid session", Fields: &fields}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// applySessionBase copies the fields present in b onto sess.
func applySessionBase(sess *domain.Session, b api.SessionBase) {
	if b.Name != nil {
		sess.Name = strings.TrimSpace(*b.Name)
	}
	if b.Room != nil {
		sess.Room = b.Room
	}
	if b.SourceLanguage != nil {
		sess.SourceLanguage = *b.SourceLanguage
	}
	if b.TargetLanguages != nil {
		sess.TargetLanguages = *b.TargetLanguages
	}
	if b.Provider != nil {
		sess.Provider = *b.Provider
	}
	if b.GlossaryId != nil {
		sess.GlossaryId = b.GlossaryId
	}
	if b.RecordingEnabled != nil {
		sess.RecordingEnabled = *b.RecordingEnabled
	}
	if b.StageStyle != nil {
		sess.StageStyle = b.StageStyle
	}
	if b.StreamCaptions != nil {
		sc := *b.StreamCaptions
		sc.YoutubeUrlSet = nil // read-only; reported from the secrets store (P3-16)
		sess.StreamCaptions = &sc
	}
}

// present fills in the runtime fields of a stored session: state,
// effective provider and URLs.
func (s *Server) present(ctx context.Context, sess domain.Session) domain.Session {
	sess.State = api.SessionStateIdle
	sess.EffectiveProvider = api.ProviderKindMock
	if s.Manager != nil {
		sess.State = s.Manager.State(sess.Id)
		sess.EffectiveProvider = s.Manager.EffectiveProvider(ctx, sess.Provider)
	}
	sess.Urls = s.sessionURLs(sess)
	return sess
}

// sessionURLs builds the audience, stage, overlay, capture and replay
// links from the network's viewer base URL (OUT-4).
func (s *Server) sessionURLs(sess domain.Session) api.SessionUrls {
	base := ""
	if s.Network != nil {
		base = strings.TrimSuffix(s.Network().ViewerBaseUrl, "/")
	}
	id := url.PathEscape(sess.Id)
	overlay := base + "/overlay/" + id
	if len(sess.TargetLanguages) > 0 {
		overlay += "?lang=" + url.QueryEscape(sess.TargetLanguages[0])
	}
	return api.SessionUrls{
		Viewer:  base + "/s/" + id,
		Stage:   base + "/stage/" + id,
		Overlay: overlay,
		Capture: base + "/capture/" + id,
		Replay:  base + "/replay/" + id,
	}
}

// running reports a session whose pipeline settings can't change now.
func (s *Server) running(id string) bool {
	if s.Manager == nil {
		return false
	}
	st := s.Manager.State(id)
	return st != api.SessionStateIdle && st != api.SessionStateError
}

// sessionEvent tells admin dashboards (/ws/admin) a session changed.
func (s *Server) sessionEvent(typ api.AdminEventType, sess *domain.Session, id string) {
	if s.Manager == nil {
		return
	}
	s.Manager.Events().Publish(api.AdminEvent{Type: typ, At: time.Now(), Session: sess, SessionId: &id})
}

func (s *Server) ListSessions(ctx context.Context, _ api.ListSessionsRequestObject) (api.ListSessionsResponseObject, error) {
	if s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	list, err := s.Sessions.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make(api.ListSessions200JSONResponse, 0, len(list))
	for _, sess := range list {
		out = append(out, s.present(ctx, sess))
	}
	return out, nil
}

// CreateSession stores a new session with defaults from the settings and
// returns its ingest token, the only time besides rotation it's shown.
func (s *Server) CreateSession(ctx context.Context, req api.CreateSessionRequestObject) (api.CreateSessionResponseObject, error) {
	if s.Sessions == nil || s.Auth == nil {
		return nil, api.ErrNotImplemented
	}
	if req.Body == nil {
		return api.CreateSession400JSONResponse{BadRequestJSONResponse: invalidSession(map[string]string{"slug": "session.slug_invalid"})}, nil
	}
	b := req.Body
	base := api.SessionBase{Name: &b.Name, Room: b.Room, SourceLanguage: b.SourceLanguage, TargetLanguages: b.TargetLanguages,
		Provider: b.Provider, GlossaryId: b.GlossaryId, RecordingEnabled: b.RecordingEnabled, StageStyle: b.StageStyle,
		StreamCaptions: b.StreamCaptions}
	bad := validateSession(base, true)
	if !slugPattern.MatchString(b.Slug) {
		bad["slug"] = "session.slug_invalid"
	}
	if err := s.checkGlossaryRef(ctx, b.GlossaryId, "glossaryId", bad); err != nil {
		return nil, err
	}
	if len(bad) > 0 {
		return api.CreateSession400JSONResponse{BadRequestJSONResponse: invalidSession(bad)}, nil
	}

	d := s.sessionDefaults(ctx)
	now := time.Now().UTC()
	sess := domain.Session{Id: b.Slug, SourceLanguage: d.source, TargetLanguages: d.targets, GlossaryId: d.glossary,
		Provider: api.ProviderChoiceDefault, RecordingEnabled: d.recording, CreatedAt: now, UpdatedAt: now}
	applySessionBase(&sess, base)
	err := s.Sessions.CreateSession(ctx, sess)
	if errors.Is(err, domain.ErrConflict) {
		return api.CreateSession409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse{
			Code: "session.slug_taken", Message: "a session with this slug exists", Params: &map[string]any{"slug": b.Slug},
		}}, nil
	}
	if err != nil {
		return nil, err
	}
	token, err := s.Auth.RotateIngestToken(ctx, sess.Id)
	if err != nil {
		return nil, err
	}
	out := s.present(ctx, sess)
	s.sessionEvent(api.AdminEventTypeSessionCreated, &out, out.Id)
	return api.CreateSession201JSONResponse{
		Id: out.Id, Name: out.Name, Room: out.Room, SourceLanguage: out.SourceLanguage, TargetLanguages: out.TargetLanguages,
		Provider: out.Provider, EffectiveProvider: out.EffectiveProvider, GlossaryId: out.GlossaryId,
		RecordingEnabled: out.RecordingEnabled, StageStyle: out.StageStyle, StreamCaptions: out.StreamCaptions,
		State: out.State, Urls: out.Urls, CreatedAt: out.CreatedAt, UpdatedAt: out.UpdatedAt, IngestToken: token,
	}, nil
}

func (s *Server) GetSession(ctx context.Context, req api.GetSessionRequestObject) (api.GetSessionResponseObject, error) {
	if s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	sess, err := s.Sessions.GetSession(ctx, req.SessionId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.GetSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetSession200JSONResponse(s.present(ctx, sess)), nil
}

// UpdateSession applies the fields present in the body. Name, room,
// recording and styles change any time; languages, provider and glossary
// only while the session isn't running, since the pipeline reads them at
// start.
func (s *Server) UpdateSession(ctx context.Context, req api.UpdateSessionRequestObject) (api.UpdateSessionResponseObject, error) {
	if s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	if req.Body == nil {
		return api.UpdateSession400JSONResponse{BadRequestJSONResponse: invalidSession(map[string]string{})}, nil
	}
	bad := validateSession(*req.Body, false)
	if err := s.checkGlossaryRef(ctx, req.Body.GlossaryId, "glossaryId", bad); err != nil {
		return nil, err
	}
	if len(bad) > 0 {
		return api.UpdateSession400JSONResponse{BadRequestJSONResponse: invalidSession(bad)}, nil
	}
	sess, err := s.Sessions.GetSession(ctx, req.SessionId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.UpdateSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	b := req.Body
	pipeline := b.SourceLanguage != nil || b.TargetLanguages != nil || b.Provider != nil || b.GlossaryId != nil
	if pipeline && s.running(sess.Id) {
		return api.UpdateSession409JSONResponse{ConflictJSONResponse: s.stateConflict(sess.Id)}, nil
	}
	applySessionBase(&sess, *b)
	sess.UpdatedAt = time.Now().UTC()
	err = s.Sessions.UpdateSession(ctx, sess)
	if errors.Is(err, domain.ErrNotFound) {
		return api.UpdateSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	out := s.present(ctx, sess)
	s.sessionEvent(api.AdminEventTypeSessionUpdated, &out, out.Id)
	return api.UpdateSession200JSONResponse(out), nil
}

// DeleteSession removes a session that isn't running, with its captions.
func (s *Server) DeleteSession(ctx context.Context, req api.DeleteSessionRequestObject) (api.DeleteSessionResponseObject, error) {
	if s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	if _, err := s.Sessions.GetSession(ctx, req.SessionId); errors.Is(err, domain.ErrNotFound) {
		return api.DeleteSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	} else if err != nil {
		return nil, err
	}
	if s.Manager != nil {
		if err := s.Manager.Forget(req.SessionId); errors.Is(err, session.ErrState) {
			return api.DeleteSession409JSONResponse{ConflictJSONResponse: s.stateConflict(req.SessionId)}, nil
		}
	}
	err := s.Sessions.DeleteSession(ctx, req.SessionId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.DeleteSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	s.sessionEvent(api.AdminEventTypeSessionDeleted, nil, req.SessionId)
	return api.DeleteSession204Response{}, nil
}

// publicSession is what the audience may see: no tokens, no provider config.
func (s *Server) publicSession(sess domain.Session) api.PublicSession {
	p := api.PublicSession{Id: sess.Id, Name: sess.Name, Room: sess.Room, State: api.SessionStateIdle,
		Languages: sess.TargetLanguages, Recording: &sess.RecordingEnabled, StageStyle: sess.StageStyle}
	if p.Languages == nil {
		p.Languages = []api.LanguageCode{}
	}
	if s.Manager != nil {
		st := s.Manager.StatusOf(sess.Id)
		p.State, p.DetectedLanguage = st.State, st.DetectedLanguage
	}
	return p
}

func (s *Server) ListPublicSessions(ctx context.Context, _ api.ListPublicSessionsRequestObject) (api.ListPublicSessionsResponseObject, error) {
	if s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	list, err := s.Sessions.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make(api.ListPublicSessions200JSONResponse, 0, len(list))
	for _, sess := range list {
		out = append(out, s.publicSession(sess))
	}
	return out, nil
}

func (s *Server) GetPublicSession(ctx context.Context, req api.GetPublicSessionRequestObject) (api.GetPublicSessionResponseObject, error) {
	if s.Sessions == nil {
		return nil, api.ErrNotImplemented
	}
	sess, err := s.Sessions.GetSession(ctx, req.SessionId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.GetPublicSession404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetPublicSession200JSONResponse(s.publicSession(sess)), nil
}
