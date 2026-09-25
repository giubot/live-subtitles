// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
)

// AudioSource produces session audio: WebSocket ingest, an ffmpeg file/URL
// or SRT listener, or a fake generator in tests.
type AudioSource interface {
	Kind() api.AudioSourceKind
	// Start begins producing frames. The channel is closed when the source
	// ends or ctx is cancelled; Err then reports why (nil on a clean end).
	Start(ctx context.Context) (<-chan AudioFrame, error)
	Err() error
}

// ASRConfig configures one ASR stream.
type ASRConfig struct {
	SessionID string
	// SourceLanguage is `auto` (detect EN/ES per segment) or a pinned language.
	SourceLanguage SourceLanguage
	Glossary       *Glossary
}

// ASREvent is one transcription update. Interim events (Final=false) are
// replaced by later events with the same SegmentID.
type ASREvent struct {
	SegmentID string
	Text      string
	Final     bool
	// Start and End are session clock offsets of the audio the text covers.
	Start, End time.Duration
	// Lang is the detected (or pinned) source language of the segment.
	Lang  LanguageCode
	Usage Usage
	// Err reports a provider error; the stream may continue or close after it.
	Err error
}

// ASRProvider turns audio into source-language captions (AI-1).
type ASRProvider interface {
	Kind() ProviderKind
	// Start opens a stream. The caller sends frames and closes the input
	// channel when audio ends; the provider flushes pending text as final
	// events and then closes the output channel. Cancelling ctx aborts.
	Start(ctx context.Context, cfg ASRConfig) (chan<- AudioFrame, <-chan ASREvent, error)
}

// TranslateRequest asks for one segment in one target language.
type TranslateRequest struct {
	SessionID string
	SegmentID string
	Text      string
	From, To  LanguageCode
	// Final is false for interim text, which may be translated more cheaply.
	Final bool
	// Context holds the previous final source sentences, oldest first.
	Context  []string
	Glossary *Glossary
}

// TranslateResult is the translated text of a TranslateRequest.
type TranslateResult struct {
	Text  string
	Usage Usage
}

// Translator translates caption text between languages (AI-5).
type Translator interface {
	Kind() ProviderKind
	Translate(ctx context.Context, req TranslateRequest) (TranslateResult, error)
}

// StreamingTranslator is optionally implemented by translators that can
// report partial output; partial gets the text so far.
type StreamingTranslator interface {
	Translator
	TranslateStream(ctx context.Context, req TranslateRequest, partial func(text string)) (TranslateResult, error)
}

// CaptionBus fans messages out to the viewers of each session and track.
type CaptionBus interface {
	// Publish delivers msg to the session's subscribers. Caption messages go
	// to subscribers of the caption's track; other messages (state, viewers,
	// error) go to every subscriber of the session.
	Publish(sessionID string, msg BusMessage)
	// Subscribe returns a channel whose first message is the history of the
	// requested tracks. It is closed when ctx is cancelled or the subscriber
	// falls too far behind.
	Subscribe(ctx context.Context, sessionID string, tracks []string) <-chan BusMessage
	Viewers(sessionID string) int
}

// SessionStore persists session configuration.
type SessionStore interface {
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, id string) (Session, error)
	ListSessions(ctx context.Context) ([]Session, error)
	UpdateSession(ctx context.Context, s Session) error
	DeleteSession(ctx context.Context, id string) error
}

// SettingsStore persists the admin-editable settings. Settings returns
// ErrNotFound until the first PutSettings; callers then use defaults.
type SettingsStore interface {
	Settings(ctx context.Context) (api.Settings, error)
	PutSettings(ctx context.Context, s api.Settings) error
}

// CaptionQuery selects stored final captions.
type CaptionQuery struct {
	SessionID string
	Track     string
	// Cursor comes from a previous page's next cursor; empty starts at the beginning.
	Cursor string
	Limit  int
	// From and To, when To > From, keep only captions whose start is in
	// [From, To) on the session clock (a recording's window).
	From, To time.Duration
}

// CaptionStore persists final captions for exports and replay.
type CaptionStore interface {
	// SaveCaption upserts by (session, track, segment).
	SaveCaption(ctx context.Context, c CaptionEvent) error
	ListCaptions(ctx context.Context, q CaptionQuery) (items []CaptionEvent, next string, err error)
}

// SecretSource tells where a secret value was found.
type SecretSource string

const (
	SecretFromEnv      SecretSource = "env"
	SecretFromKeychain SecretSource = "keychain"
	SecretFromFile     SecretSource = "file"
)

// SecretStore resolves secrets (env → keychain → encrypted file). Values
// never leave the server; the UI only sees SecretInfo.
type SecretStore interface {
	GetSecret(ctx context.Context, name string) (value string, source SecretSource, err error)
	SetSecret(ctx context.Context, name, value string) error
	DeleteSecret(ctx context.Context, name string) error
	SecretInfo(ctx context.Context, name string) (SecretInfo, error)
}

// Recorder writes session audio to disk (REC-1).
type Recorder interface {
	Start(ctx context.Context, sessionID string) (RecordingSink, error)
}

// RecordingSink receives the frames of one recording.
type RecordingSink interface {
	ID() string
	Write(f AudioFrame) error
	// Close finalizes the file.
	Close() error
}
