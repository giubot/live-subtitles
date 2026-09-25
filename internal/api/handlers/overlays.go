// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// OverlayPresets stores the saved overlay presets (internal/store). Create
// returns domain.ErrConflict for a taken id; Get, Update and Delete return
// domain.ErrNotFound for a missing one.
type OverlayPresets interface {
	CreateOverlayPreset(ctx context.Context, p api.OverlayPreset) error
	GetOverlayPreset(ctx context.Context, id string) (api.OverlayPreset, error)
	ListOverlayPresets(ctx context.Context) ([]api.OverlayPreset, error)
	UpdateOverlayPreset(ctx context.Context, p api.OverlayPreset) error
	DeleteOverlayPreset(ctx context.Context, id string) error
}

// Error codes of the overlay preset operations.
const (
	codeOverlayNotFound   = "overlay.not_found"
	codeOverlayBuiltIn    = "overlay.builtin_read_only"
	codeOverlayInvalid    = "overlay.invalid"
	codeOverlayName       = "overlay.invalid_name"
	codeOverlayColor      = "overlay.invalid_color"
	codeOverlayOutOfRange = "overlay.out_of_range"
	maxOverlayNameLength  = 80
)

// builtinOverlayPresets are the presets of docs/design.md § Surfaces, the
// same looks as web/src/features/overlay/overlayStyle.ts. Their colours are
// the overlay tokens of web/src/theme/tokens.css, which the overlay page
// defines.
func builtinOverlayPresets() []api.OverlayPreset {
	classic := api.OverlayStyle{
		FontSizePx:     new(44),
		FontWeight:     new(600),
		Color:          new("var(--overlay-fg)"),
		OutlineColor:   new("var(--overlay-outline)"),
		OutlineWidthPx: new(1),
		Background:     new("var(--overlay-box)"),
		Position:       new(api.Bottom),
		Align:          new(api.Center),
		MarginPx:       new(60),
		MaxLines:       new(2),
		FadeAfterMs:    new(6000),
		ShowInterim:    new(true),
	}
	outline := classic
	outline.Background, outline.OutlineWidthPx = new("transparent"), new(3)
	lowerThird := classic
	lowerThird.Align, lowerThird.FontSizePx, lowerThird.MarginPx = new(api.Left), new(38), new(80)
	return []api.OverlayPreset{
		{Id: "classic", Name: "Classic box", Style: classic, BuiltIn: new(true)},
		{Id: "outline", Name: "Outline only", Style: outline, BuiltIn: new(true)},
		{Id: "lower-third", Name: "Lower third", Style: lowerThird, BuiltIn: new(true)},
	}
}

func builtinOverlayPreset(id string) (api.OverlayPreset, bool) {
	all := builtinOverlayPresets()
	i := slices.IndexFunc(all, func(p api.OverlayPreset) bool { return p.Id == id })
	if i < 0 {
		return api.OverlayPreset{}, false
	}
	return all[i], true
}

// overlayRanges are the accepted integer ranges (lookLimits in
// web/src/features/overlay/overlayStyle.ts).
var overlayRanges = map[string][2]int{
	"style.fontSizePx":     {12, 200},
	"style.fontWeight":     {100, 900},
	"style.outlineWidthPx": {0, 12},
	"style.marginPx":       {0, 400},
	"style.maxLines":       {1, 4},
	"style.fadeAfterMs":    {0, 600000},
}

// cssColor is what the overlay page accepts as a colour when it can't ask
// the browser: no quotes, semicolons or braces, so a value can't escape
// its CSS declaration.
var cssColor = regexp.MustCompile(`^[#a-zA-Z0-9(),.%\s/-]{1,64}$`)

// fontFamily is a CSS font-family list.
var fontFamily = regexp.MustCompile(`^[a-zA-Z0-9 ,"'-]{1,200}$`)

// validateOverlayPreset checks a preset body (trimmed). It returns field
// path → error code.
func validateOverlayPreset(in api.OverlayPresetInput) map[string]string {
	bad := map[string]string{}
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > maxOverlayNameLength {
		bad["name"] = codeOverlayName
	}
	st := in.Style
	for path, v := range map[string]*int{
		"style.fontSizePx":     st.FontSizePx,
		"style.fontWeight":     st.FontWeight,
		"style.outlineWidthPx": st.OutlineWidthPx,
		"style.marginPx":       st.MarginPx,
		"style.maxLines":       st.MaxLines,
		"style.fadeAfterMs":    st.FadeAfterMs,
	} {
		if r := overlayRanges[path]; v != nil && (*v < r[0] || *v > r[1]) {
			bad[path] = codeOverlayOutOfRange
		}
	}
	for path, v := range map[string]*string{
		"style.color":        st.Color,
		"style.outlineColor": st.OutlineColor,
		"style.background":   st.Background,
	} {
		if v != nil && !cssColor.MatchString(*v) {
			bad[path] = codeOverlayColor
		}
	}
	if st.FontFamily != nil && !fontFamily.MatchString(*st.FontFamily) {
		bad["style.fontFamily"] = "request.invalid"
	}
	if st.Position != nil && !st.Position.Valid() {
		bad["style.position"] = "request.invalid"
	}
	if st.Align != nil && !st.Align.Valid() {
		bad["style.align"] = "request.invalid"
	}
	return bad
}

// trimOverlayPreset trims the name and the string style fields in place.
func trimOverlayPreset(in *api.OverlayPresetInput) {
	in.Name = strings.TrimSpace(in.Name)
	for _, p := range []*string{in.Style.Color, in.Style.OutlineColor, in.Style.Background, in.Style.FontFamily} {
		if p != nil {
			*p = strings.TrimSpace(*p)
		}
	}
}

// overlayInput checks a create or update body; bad is nil when it's valid.
func overlayInput(body *api.OverlayPresetInput) (api.OverlayPresetInput, *api.BadRequestJSONResponse) {
	if body == nil {
		return api.OverlayPresetInput{}, &api.BadRequestJSONResponse{Code: "request.invalid", Message: "a preset body is required"}
	}
	in := *body
	trimOverlayPreset(&in)
	if bad := validateOverlayPreset(in); len(bad) > 0 {
		r := badRequest(bad, codeOverlayInvalid, "invalid overlay preset")
		return in, &r
	}
	return in, nil
}

var overlayNotFound = api.NotFoundJSONResponse{Code: codeOverlayNotFound, Message: "overlay preset not found"}

var overlayBuiltIn = api.ConflictJSONResponse{Code: codeOverlayBuiltIn, Message: "built-in overlay presets can't be changed"}

// ListOverlayPresets answers the built-ins, then the saved presets by name.
// It works without a store, with the built-ins only.
func (s *Server) ListOverlayPresets(ctx context.Context, _ api.ListOverlayPresetsRequestObject) (api.ListOverlayPresetsResponseObject, error) {
	out := builtinOverlayPresets()
	if s.OverlayPresets != nil {
		saved, err := s.OverlayPresets.ListOverlayPresets(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range saved {
			p.BuiltIn = new(false)
			out = append(out, p)
		}
	}
	return api.ListOverlayPresets200JSONResponse(out), nil
}

func (s *Server) GetOverlayPreset(ctx context.Context, req api.GetOverlayPresetRequestObject) (api.GetOverlayPresetResponseObject, error) {
	if p, ok := builtinOverlayPreset(req.PresetId); ok {
		return api.GetOverlayPreset200JSONResponse(p), nil
	}
	if s.OverlayPresets == nil {
		return api.GetOverlayPreset404JSONResponse{NotFoundJSONResponse: overlayNotFound}, nil
	}
	p, err := s.OverlayPresets.GetOverlayPreset(ctx, req.PresetId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.GetOverlayPreset404JSONResponse{NotFoundJSONResponse: overlayNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	p.BuiltIn = new(false)
	return api.GetOverlayPreset200JSONResponse(p), nil
}

// CreateOverlayPreset saves a preset under an id made from its name
// (`My box` → `my-box`, then `my-box-2`…), readable in overlay links.
func (s *Server) CreateOverlayPreset(ctx context.Context, req api.CreateOverlayPresetRequestObject) (api.CreateOverlayPresetResponseObject, error) {
	if s.OverlayPresets == nil {
		return nil, api.ErrNotImplemented
	}
	in, bad := overlayInput(req.Body)
	if bad != nil {
		return api.CreateOverlayPreset400JSONResponse{BadRequestJSONResponse: *bad}, nil
	}
	base := overlayPresetSlug(in.Name)
	for n := 1; ; n++ {
		p := api.OverlayPreset{Id: base, Name: in.Name, Style: in.Style}
		if n > 1 {
			p.Id = base + "-" + strconv.Itoa(n)
		}
		if _, builtin := builtinOverlayPreset(p.Id); builtin {
			continue
		}
		err := s.OverlayPresets.CreateOverlayPreset(ctx, p)
		if errors.Is(err, domain.ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		p.BuiltIn = new(false)
		return api.CreateOverlayPreset201JSONResponse(p), nil
	}
}

func (s *Server) UpdateOverlayPreset(ctx context.Context, req api.UpdateOverlayPresetRequestObject) (api.UpdateOverlayPresetResponseObject, error) {
	if s.OverlayPresets == nil {
		return nil, api.ErrNotImplemented
	}
	if _, ok := builtinOverlayPreset(req.PresetId); ok {
		return api.UpdateOverlayPreset409JSONResponse{ConflictJSONResponse: overlayBuiltIn}, nil
	}
	in, bad := overlayInput(req.Body)
	if bad != nil {
		return api.UpdateOverlayPreset400JSONResponse{BadRequestJSONResponse: *bad}, nil
	}
	p := api.OverlayPreset{Id: req.PresetId, Name: in.Name, Style: in.Style}
	err := s.OverlayPresets.UpdateOverlayPreset(ctx, p)
	if errors.Is(err, domain.ErrNotFound) {
		return api.UpdateOverlayPreset404JSONResponse{NotFoundJSONResponse: overlayNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	p.BuiltIn = new(false)
	return api.UpdateOverlayPreset200JSONResponse(p), nil
}

func (s *Server) DeleteOverlayPreset(ctx context.Context, req api.DeleteOverlayPresetRequestObject) (api.DeleteOverlayPresetResponseObject, error) {
	if s.OverlayPresets == nil {
		return nil, api.ErrNotImplemented
	}
	if _, ok := builtinOverlayPreset(req.PresetId); ok {
		return api.DeleteOverlayPreset409JSONResponse{ConflictJSONResponse: overlayBuiltIn}, nil
	}
	err := s.OverlayPresets.DeleteOverlayPreset(ctx, req.PresetId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.DeleteOverlayPreset404JSONResponse{NotFoundJSONResponse: overlayNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.DeleteOverlayPreset204Response{}, nil
}

// maxOverlaySlug leaves room for a `-N` suffix within the 64 characters the
// overlay page accepts in `?preset=`.
const maxOverlaySlug = 48

// unaccent folds the accented Latin letters of Spanish, Portuguese and
// French names ("Caja clásica" → "caja-clasica").
var unaccent = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c",
)

// overlayPresetSlug makes an id from a name: lowercase ASCII letters and
// digits joined by hyphens, or `preset` when nothing is left.
func overlayPresetSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range unaccent.Replace(strings.ToLower(name)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		default:
			dash = true
		}
		if b.Len() >= maxOverlaySlug {
			break
		}
	}
	if b.Len() == 0 {
		return "preset"
	}
	return strings.TrimRight(b.String()[:min(b.Len(), maxOverlaySlug)], "-")
}
