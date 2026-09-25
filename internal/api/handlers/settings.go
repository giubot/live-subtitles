// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
	"github.com/iencodev/live-subtitles/internal/provider/gemini"
	"github.com/iencodev/live-subtitles/internal/provider/local/gemma"
	"github.com/iencodev/live-subtitles/internal/provider/local/whisper"
	"github.com/iencodev/live-subtitles/internal/recording"
	"github.com/iencodev/live-subtitles/internal/subtitle"
	"github.com/iencodev/live-subtitles/internal/translate"
)

// Defaults of the settings that no consumer package owns yet (they match
// api/openapi.yaml § Settings).
const (
	defaultOBSWebsocketURL = "ws://127.0.0.1:4455"
	defaultSRTPort         = 9000
	defaultSRTLatencyMs    = 200
)

// Error codes of an invalid Settings body, per field and for the response.
const (
	codeSettingsInvalid     = "settings.invalid"
	codeSettingsLanguage    = "settings.invalid_language"
	codeSettingsURL         = "settings.invalid_url"
	codeSettingsOutOfRange  = "settings.out_of_range"
	codeSettingsTooLong     = "settings.too_long"
	maxSettingsStringLength = 200
)

// settingsRanges are the accepted integer ranges, by field path (the same
// limits as the Settings form in web/src/features/settings/settingsForm.ts).
var settingsRanges = map[string][2]int{
	"translation.contextSentences": {0, translate.MaxContextSentences},
	"captions.maxCharsPerLine":     {10, 120},
	"captions.maxLines":            {1, 4},
	"recording.retentionDays":      {0, 3650},
	"srt.port":                     {1, 65535},
	"srt.latencyMs":                {20, 8000},
}

// DefaultSettings are the settings before the admin saves any. Each value
// comes from the package that applies it when the setting is unset, so the
// API shows what the server actually does.
func DefaultSettings() api.Settings {
	var s api.Settings
	s.Recording.EnabledByDefault = true
	s.Recording.RetentionDays = recording.DefaultRetentionDays
	withSettingsDefaults(&s)
	return s
}

// withSettingsDefaults fills every unset field of s with its default. An
// empty string or a missing object means "unset"; so does bitrate 0, which
// isn't a valid bitrate. Booleans and retention (where 0 means forever) of
// a present object are kept as given.
func withSettingsDefaults(s *api.Settings) {
	if s.DefaultSourceLanguage == "" {
		s.DefaultSourceLanguage = api.Auto
	}
	if len(s.DefaultTargetLanguages) == 0 {
		s.DefaultTargetLanguages = []api.LanguageCode{"es", "en"}
	}
	s.DefaultGlossaryId = nonEmpty(s.DefaultGlossaryId)

	p := &s.Providers
	orDefault(&p.Gemini.LiveModel, gemini.DefaultLiveModel)
	orDefault(&p.Gemini.TranslationModel, gemini.DefaultTranslationModel)
	orDefault(&p.Local.WhisperUrl, whisper.DefaultURL)
	orDefault(&p.Local.WhisperModel, whisper.DefaultModel)
	orDefault(&p.Local.OllamaUrl, gemma.DefaultURL)
	orDefault(&p.Local.GemmaModel, gemma.DefaultModel)
	ptrDefault(&p.Fallback, false)

	ptrDefault(&ensure(&s.Translation).ContextSentences, translate.DefaultContextSentences)
	c := ensure(&s.Captions)
	ptrDefault(&c.MaxCharsPerLine, subtitle.DefaultMaxCharsPerLine)
	ptrDefault(&c.MaxLines, subtitle.DefaultMaxLines)

	s.Network.PublicBaseUrl = nonEmpty(s.Network.PublicBaseUrl)
	s.Network.PreferredInterface = nonEmpty(s.Network.PreferredInterface)

	if s.Recording.BitrateKbps == 0 {
		s.Recording.BitrateKbps = recording.DefaultBitrateKbps
	}

	o := ensure(&s.Obs)
	o.WebsocketUrl = nonEmpty(o.WebsocketUrl)
	ptrDefault(&o.WebsocketUrl, defaultOBSWebsocketURL)
	srt := ensure(&s.Srt)
	ptrDefault(&srt.Enabled, true)
	ptrDefault(&srt.Port, defaultSRTPort)
	ptrDefault(&srt.LatencyMs, defaultSRTLatencyMs)
}

// trimSettings trims the free-text fields in place.
func trimSettings(s *api.Settings) {
	for _, f := range []*string{
		&s.Providers.Gemini.LiveModel, &s.Providers.Gemini.TranslationModel,
		&s.Providers.Local.WhisperUrl, &s.Providers.Local.WhisperModel,
		&s.Providers.Local.OllamaUrl, &s.Providers.Local.GemmaModel,
	} {
		*f = strings.TrimSpace(*f)
	}
	for _, f := range []*string{s.DefaultGlossaryId, s.Network.PublicBaseUrl, s.Network.PreferredInterface} {
		if f != nil {
			*f = strings.TrimSpace(*f)
		}
	}
	if s.Obs != nil && s.Obs.WebsocketUrl != nil {
		*s.Obs.WebsocketUrl = strings.TrimSpace(*s.Obs.WebsocketUrl)
	}
}

// validateSettings checks a Settings body with its defaults applied. It
// returns field path → error code.
func validateSettings(s api.Settings) map[string]string {
	bad := map[string]string{}
	if !domain.SupportedSource(s.DefaultSourceLanguage) {
		bad["defaultSourceLanguage"] = codeSettingsLanguage
	}
	seen := map[string]bool{}
	for _, l := range s.DefaultTargetLanguages {
		switch {
		case seen[l]:
			bad["defaultTargetLanguages"] = "request.invalid"
		case !domain.SupportedTarget(l):
			bad["defaultTargetLanguages"] = codeSettingsLanguage
		}
		seen[l] = true
	}

	for path, v := range map[string]string{
		"providers.gemini.liveModel":        s.Providers.Gemini.LiveModel,
		"providers.gemini.translationModel": s.Providers.Gemini.TranslationModel,
		"providers.local.whisperModel":      s.Providers.Local.WhisperModel,
		"providers.local.gemmaModel":        s.Providers.Local.GemmaModel,
		"network.preferredInterface":        deref(s.Network.PreferredInterface),
		"defaultGlossaryId":                 deref(s.DefaultGlossaryId),
	} {
		if utf8.RuneCountInString(v) > maxSettingsStringLength {
			bad[path] = codeSettingsTooLong
		}
	}
	for path, v := range map[string]string{
		"providers.local.whisperUrl": s.Providers.Local.WhisperUrl,
		"providers.local.ollamaUrl":  s.Providers.Local.OllamaUrl,
	} {
		if !validURL(v, "http", "https") {
			bad[path] = codeSettingsURL
		}
	}
	if p := s.Network.PublicBaseUrl; p != nil && !validURL(*p, "http", "https") {
		bad["network.publicBaseUrl"] = codeSettingsURL
	}
	if !validURL(deref(s.Obs.WebsocketUrl), "ws", "wss") {
		bad["obs.websocketUrl"] = codeSettingsURL
	}

	for path, v := range map[string]int{
		"translation.contextSentences": *s.Translation.ContextSentences,
		"captions.maxCharsPerLine":     *s.Captions.MaxCharsPerLine,
		"captions.maxLines":            *s.Captions.MaxLines,
		"recording.retentionDays":      s.Recording.RetentionDays,
		"srt.port":                     *s.Srt.Port,
		"srt.latencyMs":                *s.Srt.LatencyMs,
	} {
		if r := settingsRanges[path]; v < r[0] || v > r[1] {
			bad[path] = codeSettingsOutOfRange
		}
	}
	if !s.Recording.BitrateKbps.Valid() {
		bad["recording.bitrateKbps"] = codeSettingsOutOfRange
	}
	return bad
}

// validURL reports an absolute URL with one of the schemes and a host, and
// no user info, query or fragment.
func validURL(v string, schemes ...string) bool {
	if len(v) > 2048 {
		return false
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	for _, s := range schemes {
		if u.Scheme == s {
			return true
		}
	}
	return false
}

// badRequest answers 400 with the bad fields. When every field has the
// same code, that code is the response's; otherwise it's fallback.
func badRequest(fields map[string]string, fallback, message string) api.BadRequestJSONResponse {
	code := ""
	for _, c := range fields {
		if code != "" && c != code {
			code = fallback
			break
		}
		code = c
	}
	if code == "" {
		code = fallback
	}
	return api.BadRequestJSONResponse{Code: code, Message: message, Fields: &fields}
}

// settings returns the stored settings with defaults for what's unset, or
// DefaultSettings before the first save.
func (s *Server) settings(ctx context.Context) (api.Settings, error) {
	st, err := s.Settings.Settings(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return api.Settings{}, err
	}
	withSettingsDefaults(&st)
	st.Srt.PassphraseSet = nil
	return st, nil
}

func (s *Server) GetSettings(ctx context.Context, _ api.GetSettingsRequestObject) (api.GetSettingsResponseObject, error) {
	if s.Settings == nil {
		return nil, api.ErrNotImplemented
	}
	st, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetSettings200JSONResponse(st), nil
}

// UpdateSettings replaces the settings. Unset fields take their defaults,
// which are saved, so the answer is what GetSettings returns next. Every
// consumer reads the settings when it needs them (a session start, a
// recording, a subtitle file, the network info), so no restart is needed.
func (s *Server) UpdateSettings(ctx context.Context, req api.UpdateSettingsRequestObject) (api.UpdateSettingsResponseObject, error) {
	if s.Settings == nil {
		return nil, api.ErrNotImplemented
	}
	if req.Body == nil {
		return api.UpdateSettings400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: "request.invalid", Message: "a settings body is required",
		}}, nil
	}
	st := *req.Body
	trimSettings(&st)
	withSettingsDefaults(&st)
	st.Srt.PassphraseSet = nil // read-only
	bad := validateSettings(st)
	if err := s.checkGlossaryRef(ctx, st.DefaultGlossaryId, "defaultGlossaryId", bad); err != nil {
		return nil, err
	}
	if len(bad) > 0 {
		return api.UpdateSettings400JSONResponse{
			BadRequestJSONResponse: badRequest(bad, codeSettingsInvalid, "invalid settings"),
		}, nil
	}
	if err := s.Settings.PutSettings(ctx, st); err != nil {
		return nil, err
	}
	return api.UpdateSettings200JSONResponse(st), nil
}

// ensure allocates *p if it's nil and returns it; it names the generated
// anonymous struct types without spelling them out.
func ensure[T any](p **T) *T {
	if *p == nil {
		*p = new(T)
	}
	return *p
}

func ptrDefault[T any](p **T, v T) {
	if *p == nil {
		*p = &v
	}
}

func orDefault(p *string, v string) {
	if *p == "" {
		*p = v
	}
}

// nonEmpty maps an empty string to nil.
func nonEmpty(p *string) *string {
	if p == nil || *p == "" {
		return nil
	}
	return p
}
