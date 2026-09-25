// SPDX-License-Identifier: Apache-2.0

package gemini

import (
	"context"

	"google.golang.org/genai"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// liveConn is one Live API connection. The stream goroutine is its only
// writer; one reader goroutine calls Receive; Close unblocks Receive.
type liveConn interface {
	// SendAudio sends 16 kHz mono s16le PCM.
	SendAudio(pcm []byte) error
	// SendAudioStreamEnd tells the server the audio ended, so it flushes the
	// last transcription.
	SendAudioStreamEnd() error
	Receive() (*genai.LiveServerMessage, error)
	Close() error
}

// dialConfig is what a connection is opened with.
type dialConfig struct {
	APIKey string
	Model  string
	// Language is the pinned source language, or "" to detect it.
	Language domain.LanguageCode
	// Vocabulary biases the recognition toward the glossary (AI-7).
	Vocabulary []string
}

type dialFunc func(ctx context.Context, dc dialConfig) (liveConn, error)

// liveAudioMIME is the realtime input format.
const liveAudioMIME = "audio/pcm;rate=16000"

// dialGenAI opens a Live connection with the genai SDK.
func dialGenAI(ctx context.Context, dc dialConfig) (liveConn, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      dc.APIKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{APIVersion: "v1beta"},
	})
	if err != nil {
		return nil, err
	}
	s, err := client.Live.Connect(ctx, dc.Model, liveConfig(dc))
	if err != nil {
		return nil, err
	}
	return genaiConn{s}, nil
}

type genaiConn struct{ s *genai.Session }

func (c genaiConn) SendAudio(pcm []byte) error {
	return c.s.SendRealtimeInput(genai.LiveRealtimeInput{Audio: &genai.Blob{Data: pcm, MIMEType: liveAudioMIME}})
}

func (c genaiConn) SendAudioStreamEnd() error {
	return c.s.SendRealtimeInput(genai.LiveRealtimeInput{AudioStreamEnd: true})
}

func (c genaiConn) Receive() (*genai.LiveServerMessage, error) { return c.s.Receive() }
func (c genaiConn) Close() error                               { return c.s.Close() }

// liveConfig is the setup of a transcription connection: text responses,
// input transcription with the language hint, the glossary as custom
// vocabulary, and SMART mode, which drops filler words and false starts so
// captions read cleanly. Voice activity detection stays automatic: the
// server finalizes each utterance when the speaker pauses.
//
// The transcription model supports neither session resumption nor context
// window compression; the stream replaces connections itself.
func liveConfig(dc dialConfig) *genai.LiveConnectConfig {
	return &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityText},
		InputAudioTranscription: &genai.AudioTranscriptionConfig{
			LanguageCodes:    languageCodes(dc.Language),
			CustomVocabulary: dc.Vocabulary,
			Mode:             genai.AudioTranscriptionConfigModeSmart,
		},
	}
}

// languageCodes is the BCP-47 hint for a pinned language: the API lists
// regional codes only (en-US, es-419...). Auto sends none, which turns on
// the model's own language identification.
func languageCodes(pinned domain.LanguageCode) []string {
	switch pinned {
	case "en":
		return []string{"en-US"}
	case "es":
		return []string{"es-419"}
	}
	return nil
}
