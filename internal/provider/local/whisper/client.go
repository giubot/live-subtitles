// SPDX-License-Identifier: Apache-2.0

package whisper

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/iencodev/live-subtitles/internal/domain"
)

// client talks to one whisper-server.
type client struct {
	base string // URL without a trailing slash
	http *http.Client
}

// errLoading means the server is up but still loading its model.
var errLoading = errors.New("whisper-server is still loading its model")

// health checks that the server answers. Servers older than /health answer
// 404, which still proves they are up.
func (c *client) health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	switch {
	case resp.StatusCode == http.StatusServiceUnavailable:
		return errLoading
	case resp.StatusCode < 300, resp.StatusCode == http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("health: %s", resp.Status)
	}
}

// request is one POST /inference.
type request struct {
	pcm []int16
	// language is `auto` or a whisper language code (`en`, `es`).
	language string
	prompt   string
}

// response is the verbose_json answer of POST /inference.
type response struct {
	// Language is the transcription language as a whisper name ("english").
	Language string    `json:"language"`
	Text     string    `json:"text"`
	Segments []segment `json:"segments"`
	// DetectedLanguage is set with language=auto.
	DetectedLanguage string `json:"detected_language"`
	// LanguageProbabilities maps codes (`en`, `es`, …) to probabilities; it
	// is left out when the server runs with --no-language-probabilities.
	LanguageProbabilities map[string]float64 `json:"language_probabilities"`
}

type segment struct {
	Text         string  `json:"text"`
	Start        float64 `json:"start"`
	End          float64 `json:"end"`
	AvgLogprob   float64 `json:"avg_logprob"`
	NoSpeechProb float64 `json:"no_speech_prob"`
}

// inference transcribes req.pcm.
func (c *client) inference(ctx context.Context, r request) (response, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "audio.wav")
	if err != nil {
		return response{}, err
	}
	if _, err := fw.Write(wav(r.pcm)); err != nil {
		return response{}, err
	}
	fields := [][2]string{
		{"response_format", "verbose_json"},
		{"language", r.language},
		// Greedy first; whisper retries hotter only when decoding fails.
		{"temperature", "0.0"},
		{"temperature_inc", "0.2"},
	}
	if r.prompt != "" {
		fields = append(fields, [2]string{"prompt", r.prompt})
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return response{}, err
		}
	}
	if err := mw.Close(); err != nil {
		return response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/inference", &body)
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return response{}, err
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusServiceUnavailable {
		return response{}, errLoading
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return response{}, fmt.Errorf("inference: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var out struct {
		response
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return response{}, fmt.Errorf("inference: decode answer: %w", err)
	}
	if out.Error != "" {
		return response{}, fmt.Errorf("inference: %s", out.Error)
	}
	return out.response, nil
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// wav encodes pcm as a 16 kHz mono s16le WAV file.
func wav(pcm []int16) []byte {
	const header = 44
	n := len(pcm) * 2
	b := make([]byte, header+n)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(b[20:], 1)  // PCM
	binary.LittleEndian.PutUint16(b[22:], 1)  // mono
	binary.LittleEndian.PutUint32(b[24:], domain.SampleRate)
	binary.LittleEndian.PutUint32(b[28:], domain.SampleRate*2) // byte rate
	binary.LittleEndian.PutUint16(b[32:], 2)                   // block align
	binary.LittleEndian.PutUint16(b[34:], 16)                  // bits per sample
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n))
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(b[header+2*i:], uint16(s))
	}
	return b
}

// languageCode maps whisper's language names to the codes the pipeline
// uses; only English and Spanish can be a source (AI-10).
func languageCode(name string) (domain.LanguageCode, bool) {
	switch strings.ToLower(name) {
	case "english", "en":
		return "en", true
	case "spanish", "es":
		return "es", true
	}
	return "", false
}
