// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"slices"

	"github.com/iencodev/live-subtitles/internal/api"
)

// Language is one entry of the supported language catalog.
type Language = api.Language

// languages is the curated catalog (AI-5): every entry can be a target
// language; only English and Spanish can be the source (AI-10).
var languages = []Language{
	{Code: "es", Name: "Spanish", NativeName: "Español", CanBeSource: true},
	{Code: "en", Name: "English", NativeName: "English", CanBeSource: true},
	{Code: "pt", Name: "Portuguese", NativeName: "Português"},
	{Code: "fr", Name: "French", NativeName: "Français"},
	{Code: "de", Name: "German", NativeName: "Deutsch"},
	{Code: "it", Name: "Italian", NativeName: "Italiano"},
	{Code: "zh", Name: "Chinese", NativeName: "中文"},
	{Code: "ja", Name: "Japanese", NativeName: "日本語"},
	{Code: "ko", Name: "Korean", NativeName: "한국어"},
}

// Languages returns the supported languages, in display order. The slice
// is a copy the caller may modify.
func Languages() []Language { return slices.Clone(languages) }

// LookupLanguage returns the catalog entry for code.
func LookupLanguage(code LanguageCode) (Language, bool) {
	i := slices.IndexFunc(languages, func(l Language) bool { return l.Code == code })
	if i < 0 {
		return Language{}, false
	}
	return languages[i], true
}

// SupportedTarget reports whether code can be a caption track's language.
func SupportedTarget(code LanguageCode) bool {
	_, ok := LookupLanguage(code)
	return ok
}

// SupportedSource reports whether s is a valid session source language:
// `auto` or a catalog language that can be the source.
func SupportedSource(s SourceLanguage) bool {
	if s == api.Auto {
		return true
	}
	l, ok := LookupLanguage(LanguageCode(s))
	return ok && l.CanBeSource
}
