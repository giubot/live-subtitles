// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/store"
	"github.com/iencodev/live-subtitles/internal/subtitle"
)

// With recordingId, captions and subtitles cover only that recording and
// their times are relative to its start, so they line up with its audio
// (REC-3); that needs the recordings service.

func (s *Server) ListCaptions(ctx context.Context, req api.ListCaptionsRequestObject) (api.ListCaptionsResponseObject, error) {
	if s.Sessions == nil || s.Captions == nil || (req.Params.RecordingId != nil && s.Recordings == nil) {
		return nil, api.ErrNotImplemented
	}
	if _, err := s.Sessions.GetSession(ctx, req.SessionId); errors.Is(err, domain.ErrNotFound) {
		return api.ListCaptions404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	} else if err != nil {
		return nil, err
	}
	q := domain.CaptionQuery{SessionID: req.SessionId, Track: req.Params.Lang}
	if req.Params.RecordingId != nil {
		from, to, ok, err := s.recordingWindow(ctx, req.SessionId, *req.Params.RecordingId)
		if err != nil {
			return nil, err
		}
		if !ok {
			return api.ListCaptions404JSONResponse{NotFoundJSONResponse: recordingNotFound}, nil
		}
		q.From, q.To = from, to
	}
	if req.Params.After != nil {
		q.Cursor = *req.Params.After
	}
	if req.Params.Limit != nil {
		q.Limit = *req.Params.Limit
	}
	items, next, err := s.Captions.ListCaptions(ctx, q)
	if errors.Is(err, domain.ErrInvalid) {
		return api.ListCaptions400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: "request.invalid", Message: "unreadable cursor",
			Fields: &map[string]string{"after": "request.invalid"},
		}}, nil
	}
	if err != nil {
		return nil, err
	}
	page := api.ListCaptions200JSONResponse{Items: shift(visible(items), q.From)}
	if next != "" {
		page.NextCursor = &next
	}
	return page, nil
}

func (s *Server) GetSubtitles(ctx context.Context, req api.GetSubtitlesRequestObject) (api.GetSubtitlesResponseObject, error) {
	if s.Sessions == nil || s.Captions == nil || (req.Params.RecordingId != nil && s.Recordings == nil) {
		return nil, api.ErrNotImplemented
	}
	format := req.Params.Format
	if !format.Valid() {
		return api.GetSubtitles400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: "request.invalid", Message: "format must be vtt, srt, txt or json",
			Fields: &map[string]string{"format": "request.invalid"},
		}}, nil
	}
	if _, err := s.Sessions.GetSession(ctx, req.SessionId); errors.Is(err, domain.ErrNotFound) {
		return api.GetSubtitles404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	} else if err != nil {
		return nil, err
	}
	q := domain.CaptionQuery{SessionID: req.SessionId, Track: req.Params.Lang}
	name := req.SessionId
	if req.Params.RecordingId != nil {
		from, to, ok, err := s.recordingWindow(ctx, req.SessionId, *req.Params.RecordingId)
		if err != nil {
			return nil, err
		}
		if !ok {
			return api.GetSubtitles404JSONResponse{NotFoundJSONResponse: recordingNotFound}, nil
		}
		q.From, q.To, name = from, to, *req.Params.RecordingId
	}
	captions, err := s.allCaptions(ctx, q)
	if err != nil {
		return nil, err
	}
	captions = shift(captions, q.From)

	var headers api.GetSubtitles200ResponseHeaders
	if req.Params.Live != nil && *req.Params.Live {
		noStore := "no-store"
		headers.CacheControl = &noStore
	} else {
		d := fmt.Sprintf(`attachment; filename="%s-%s.%s"`, fileSafe(name), fileSafe(req.Params.Lang), format)
		headers.ContentDisposition = &d
	}

	var b bytes.Buffer
	switch format {
	case api.SubtitleFormatJson:
		return api.GetSubtitles200JSONResponse{Body: captions, Headers: headers}, nil
	case api.SubtitleFormatTxt:
		// A BOM makes browsers read the live transcript as UTF-8, since the
		// generated response sends text/plain without a charset.
		b.WriteRune(0xFEFF)
		if err := subtitle.WriteTXT(&b, captions); err != nil {
			return nil, err
		}
		return api.GetSubtitles200TextResponse{Body: b.String(), Headers: headers}, nil
	}
	cues := subtitle.Segment(captions, s.subtitleOptions(ctx))
	if format == api.SubtitleFormatSrt {
		if err := subtitle.WriteSRT(&b, cues); err != nil {
			return nil, err
		}
		return api.GetSubtitles200ApplicationxSubripResponse{Body: &b, ContentLength: int64(b.Len()), Headers: headers}, nil
	}
	if err := subtitle.WriteVTT(&b, cues); err != nil {
		return nil, err
	}
	return api.GetSubtitles200TextvttResponse{Body: &b, ContentLength: int64(b.Len()), Headers: headers}, nil
}

var captionNotFound = api.NotFoundJSONResponse{Code: "caption.not_found", Message: "caption not found"}

// PatchCaption corrects or hides one stored final caption (ADM-4) and
// broadcasts the result to the track's live viewers, who replace the line
// by segmentId. Exports, replay and the bus history then show the
// correction and leave out hidden lines.
func (s *Server) PatchCaption(ctx context.Context, req api.PatchCaptionRequestObject) (api.PatchCaptionResponseObject, error) {
	if s.Sessions == nil || s.CaptionEdits == nil {
		return nil, api.ErrNotImplemented
	}
	invalid := func(field, msg string) api.PatchCaption400JSONResponse {
		return api.PatchCaption400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: "request.invalid", Message: msg, Fields: &map[string]string{field: "request.invalid"},
		}}
	}
	var e domain.CaptionEdit
	if req.Body != nil {
		e.Hidden = req.Body.Hidden
		if req.Body.Text != nil {
			text := strings.TrimSpace(*req.Body.Text)
			if text == "" {
				return invalid("text", "text must not be blank; hide the caption instead"), nil
			}
			e.Text = &text
		}
	}
	if e.Text == nil && e.Hidden == nil {
		return invalid("text", "set text or hidden"), nil
	}
	if _, err := s.Sessions.GetSession(ctx, req.SessionId); errors.Is(err, domain.ErrNotFound) {
		return api.PatchCaption404JSONResponse{NotFoundJSONResponse: sessionNotFound}, nil
	} else if err != nil {
		return nil, err
	}
	c, err := s.CaptionEdits.EditCaption(ctx, req.SessionId, req.Params.Lang, req.SegmentId, e)
	if errors.Is(err, domain.ErrNotFound) {
		return api.PatchCaption404JSONResponse{NotFoundJSONResponse: captionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	if s.CaptionBus != nil {
		live := c
		s.CaptionBus.Publish(req.SessionId, domain.BusMessage{Type: api.CaptionsServerMessageTypeCaption, Caption: &live})
	}
	return api.PatchCaption200JSONResponse(c), nil
}

// allCaptions returns every visible final caption q selects, in order.
func (s *Server) allCaptions(ctx context.Context, q domain.CaptionQuery) ([]api.Caption, error) {
	q.Limit = store.MaxCaptionLimit
	out := []api.Caption{}
	for {
		items, next, err := s.Captions.ListCaptions(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, visible(items)...)
		if next == "" {
			return out, nil
		}
		q.Cursor = next
	}
}

// visible drops captions an admin hid (ADM-4) from public output.
func visible(items []api.Caption) []api.Caption {
	out := items[:0:0]
	for _, c := range items {
		if c.Hidden == nil || !*c.Hidden {
			out = append(out, c)
		}
	}
	if out == nil {
		out = []api.Caption{}
	}
	return out
}

// subtitleOptions reads the caption line limits from the settings.
func (s *Server) subtitleOptions(ctx context.Context) subtitle.Options {
	var o subtitle.Options
	if s.Settings == nil {
		return o
	}
	st, err := s.Settings.Settings(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			s.log.WarnContext(ctx, "read settings; using default caption limits", "err", err)
		}
		return o
	}
	if c := st.Captions; c != nil {
		if c.MaxCharsPerLine != nil {
			o.MaxCharsPerLine = *c.MaxCharsPerLine
		}
		if c.MaxLines != nil {
			o.MaxLines = *c.MaxLines
		}
	}
	return o
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// fileSafe keeps request values from breaking out of the filename quotes.
func fileSafe(s string) string { return unsafeFileChars.ReplaceAllString(s, "_") }
