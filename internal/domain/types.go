// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
)

// Types shared with the HTTP contract are aliases of the generated ones, so
// they cross package boundaries without mapping.
type (
	Session        = api.Session
	SessionState   = api.SessionState
	SessionStatus  = api.SessionStatus
	SourceLanguage = api.SourceLanguage
	ProviderKind   = api.ProviderKind
	LanguageCode   = api.LanguageCode
	Glossary       = api.Glossary
	Recording      = api.Recording
	SecretInfo     = api.SecretInfo

	// CaptionEvent is one caption on one track. Interim events (Final=false)
	// are replaced by later events with the same SegmentId (AI-6).
	CaptionEvent = api.Caption

	// BusMessage is what caption subscribers receive; it's the /ws/captions frame.
	BusMessage = api.CaptionsServerMessage
)

// SourceTrack is the caption track carrying the untranslated transcription.
const SourceTrack = "source"

// Audio format used everywhere after the source: 16 kHz mono s16le.
const (
	SampleRate    = 16000
	FrameDuration = 20 * time.Millisecond
	FrameSamples  = SampleRate * int(FrameDuration) / int(time.Second) // 320
)

// AudioFrame is a chunk of PCM audio. T is the offset of its first sample
// from the session clock origin (the recording start, REC-3).
type AudioFrame struct {
	PCM []int16
	T   time.Duration
}

// End is the offset just after the last sample.
func (f AudioFrame) End() time.Duration {
	return f.T + time.Duration(len(f.PCM))*time.Second/SampleRate
}

// Seconds converts a session clock offset to the seconds used in captions.
func Seconds(d time.Duration) float32 { return float32(d.Seconds()) }

// Clock abstracts wall time so tests can control it.
type Clock interface {
	Now() time.Time
}

// SystemClock is the real wall clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// Usage is provider consumption, summed into SessionStatus.usage (AI-9).
type Usage struct {
	AudioSeconds float64
	InputTokens  int64
	OutputTokens int64
}

// Add returns the sum of u and o.
func (u Usage) Add(o Usage) Usage {
	return Usage{u.AudioSeconds + o.AudioSeconds, u.InputTokens + o.InputTokens, u.OutputTokens + o.OutputTokens}
}

// Sentinel errors returned by stores and services; handlers map them to
// Error codes.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")
)
