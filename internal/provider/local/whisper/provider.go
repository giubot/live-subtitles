// SPDX-License-Identifier: Apache-2.0

package whisper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Defaults of Settings.providers.local (they match api/openapi.yaml).
const (
	DefaultURL   = "http://127.0.0.1:8178"
	DefaultModel = "large-v3-turbo"
)

// CodeEnglishOnlyModel is the error code of a refused `.en` model.
const CodeEnglishOnlyModel = "provider.model_english_only"

// Timeouts of the calls to whisper-server.
const (
	healthTimeout = 3 * time.Second
	// probeTimeout allows for a cold model on a CPU.
	probeTimeout     = 30 * time.Second
	inferenceTimeout = 30 * time.Second
	retryDelay       = 300 * time.Millisecond
	// langLockAfter is the utterance audio after which its detected
	// language is kept for its later interims.
	langLockAfter = 2 * time.Second
)

// Provider is the local domain.ASRProvider: whisper-server over HTTP with
// energy-VAD chunking (see the package doc).
type Provider struct {
	// Settings returns the admin settings; the whisper URL and model come
	// from providers.local and are read at every Start. Nil, an
	// ErrNotFound or empty fields use DefaultURL and DefaultModel.
	Settings func(ctx context.Context) (api.Settings, error)
	// HTTPClient defaults to a client without a global timeout (every
	// call has its own).
	HTTPClient *http.Client
	VAD        VADConfig
	Logger     *slog.Logger
}

var _ domain.ASRProvider = (*Provider)(nil)

func (p *Provider) Kind() domain.ProviderKind { return api.ProviderKindLocal }

// config is what Start resolves from the settings.
type config struct {
	url, model string
}

func (p *Provider) config(ctx context.Context) (config, error) {
	c := config{url: DefaultURL, model: DefaultModel}
	if p.Settings == nil {
		return c, nil
	}
	s, err := p.Settings(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read settings: %w", err)
	}
	if u := strings.TrimSpace(s.Providers.Local.WhisperUrl); u != "" {
		c.url = u
	}
	if m := strings.TrimSpace(s.Providers.Local.WhisperModel); m != "" {
		c.model = m
	}
	c.url = strings.TrimRight(c.url, "/")
	return c, nil
}

// englishOnlyName matches whisper.cpp's English-only model names:
// tiny.en, ggml-base.en.bin, small.en-q5_1, distil-medium.en.
var englishOnlyName = regexp.MustCompile(`\.en($|[-_.])`)

// IsEnglishOnlyModel reports whether name (a model id or file name) is an
// English-only whisper model, which can't transcribe Spanish (AI-3).
func IsEnglishOnlyModel(name string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
	base = strings.TrimSuffix(base, ".bin")
	return englishOnlyName.MatchString(base)
}

func englishOnly(model, how string) error {
	return &domain.CodedError{
		Code:    CodeEnglishOnlyModel,
		Message: fmt.Sprintf("whisper model %q is English-only (%s); the local provider needs a multilingual model such as %s", model, how, DefaultModel),
		Params:  map[string]any{"model": model},
	}
}

// Start checks that whisper-server answers with a multilingual model and
// opens a stream. Errors are the session's provider.unavailable, or a
// *domain.CodedError with CodeEnglishOnlyModel.
func (p *Provider) Start(ctx context.Context, cfg domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	conf, err := p.config(ctx)
	if err != nil {
		return nil, nil, err
	}
	if IsEnglishOnlyModel(conf.model) {
		return nil, nil, englishOnly(conf.model, "by its name")
	}
	hc := p.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	c := &client{base: conf.url, http: hc}
	hctx, cancel := context.WithTimeout(ctx, healthTimeout)
	err = c.health(hctx)
	cancel()
	if err != nil {
		return nil, nil, fmt.Errorf("whisper-server at %s isn't reachable: %w", conf.url, err)
	}
	// An English-only model ignores the language and answers "english".
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	probe, err := c.inference(pctx, request{pcm: make([]int16, domain.SampleRate), language: "es"})
	cancel()
	if err != nil {
		return nil, nil, fmt.Errorf("whisper-server at %s: %w", conf.url, err)
	}
	if strings.EqualFold(probe.Language, "english") {
		return nil, nil, englishOnly(conf.model, "the server transcribes Spanish as English")
	}

	log := p.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log.Info("whisper stream started", "session", cfg.SessionID, "url", conf.url, "model", conf.model, "language", cfg.SourceLanguage)
	in := make(chan domain.AudioFrame, 256)
	out := make(chan domain.ASREvent, 64)
	s := &stream{
		c: c, cfg: cfg, log: log.With("session", cfg.SessionID),
		seg: newSegmenter(p.VAD), q: newQueue(), out: out,
		prompt: glossaryPrompt(cfg.Glossary), prev: "en",
	}
	if l, ok := languageCode(string(cfg.SourceLanguage)); ok {
		s.pinned, s.prev = l, l
	}
	go s.segment(ctx, in)
	go s.transcribe(ctx)
	return in, out, nil
}

// glossaryPrompt primes whisper with the glossary's spellings (AI-7).
func glossaryPrompt(g *domain.Glossary) string {
	if g == nil {
		return ""
	}
	var terms []string
	seen := map[string]bool{}
	add := func(t string) {
		if t = strings.TrimSpace(t); t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			terms = append(terms, t)
		}
	}
	for _, t := range g.Terms {
		add(t.Term)
	}
	for _, t := range g.DoNotTranslate {
		add(t)
	}
	if len(terms) == 0 {
		return ""
	}
	// whisper keeps the last 224 prompt tokens; a long glossary is cut.
	p := strings.Join(terms, ", ") + "."
	if len(p) > 600 {
		p = p[:600]
	}
	return p
}

// stream is one ASR stream: segment cuts the audio into chunks, and
// transcribe sends them to the server one at a time.
type stream struct {
	c      *client
	cfg    domain.ASRConfig
	log    *slog.Logger
	seg    *segmenter
	q      *queue
	out    chan<- domain.ASREvent
	prompt string
	pinned domain.LanguageCode // "" detects per utterance

	// Owned by transcribe.
	prev    domain.LanguageCode         // language of the last final
	lang    map[int]domain.LanguageCode // locked language per utterance
	interim map[int]shown               // last interim per utterance
}

// shown is an interim text sent to the session.
type shown struct {
	text string
	lang domain.LanguageCode
}

func (s *stream) segment(ctx context.Context, in <-chan domain.AudioFrame) {
	defer s.q.close()
	for {
		select {
		case f, ok := <-in:
			if !ok {
				if c, ok := s.seg.flush(); ok {
					s.q.push(c)
				}
				return
			}
			for _, c := range s.seg.write(f) {
				s.q.push(c)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *stream) transcribe(ctx context.Context) {
	defer close(s.out)
	s.lang, s.interim = map[int]domain.LanguageCode{}, map[int]shown{}
	for {
		c, ok := s.q.next(ctx)
		if !ok {
			return
		}
		ev, ok := s.process(ctx, c)
		if !ok {
			continue
		}
		select {
		case s.out <- ev:
		case <-ctx.Done():
			return
		}
	}
}

// process transcribes one chunk; ok is false when there is nothing to send.
func (s *stream) process(ctx context.Context, c chunk) (domain.ASREvent, bool) {
	ev := domain.ASREvent{
		SegmentID: fmt.Sprintf("w-%06d", c.utterance),
		Final:     c.final,
		Start:     c.start,
		End:       c.end,
	}
	text, lang, err := s.recognize(ctx, c)
	if err != nil && c.final && ctx.Err() == nil {
		// One retry: a lost final is a gap in the captions.
		select {
		case <-time.After(retryDelay):
			text, lang, err = s.recognize(ctx, c)
		case <-ctx.Done():
		}
	}
	if ctx.Err() != nil {
		return ev, false
	}
	last := s.interim[c.utterance]
	if err != nil {
		s.log.Warn("whisper inference failed", "segment", ev.SegmentID, "final", c.final, "err", err)
		if !c.final {
			return ev, false // the next interim or the final covers it
		}
		// Report the error; the text shown so far becomes final rather
		// than staying interim forever.
		s.forget(c.utterance)
		if last.text != "" {
			ev.Text, ev.Lang = last.text, last.lang
			s.send(ctx, domain.ASREvent{Err: err})
			return ev, true
		}
		return domain.ASREvent{Err: err}, true
	}
	if !c.final {
		if text == "" || text == last.text {
			return ev, false
		}
		s.interim[c.utterance] = shown{text, lang}
		if c.duration() >= langLockAfter {
			s.lang[c.utterance] = lang
		}
		ev.Text, ev.Lang = text, lang
		return ev, true
	}
	s.forget(c.utterance)
	if text == "" {
		// The whole utterance turned out to be noise. An interim already
		// shown still needs a final to replace it.
		if last.text == "" {
			return ev, false
		}
		text, lang = last.text, last.lang
	}
	s.prev = lang
	ev.Text, ev.Lang = text, lang
	ev.Usage = domain.Usage{AudioSeconds: c.duration().Seconds()}
	return ev, true
}

func (s *stream) forget(utterance int) {
	delete(s.interim, utterance)
	delete(s.lang, utterance)
}

func (s *stream) send(ctx context.Context, ev domain.ASREvent) {
	select {
	case s.out <- ev:
	case <-ctx.Done():
	}
}

// recognize transcribes a chunk in the pinned or locked language, or
// detects it between English and Spanish.
func (s *stream) recognize(ctx context.Context, c chunk) (string, domain.LanguageCode, error) {
	lang := s.pinned
	if lang == "" && !c.final {
		lang = s.lang[c.utterance]
	}
	req := request{pcm: c.pcm, language: "auto", prompt: s.prompt}
	if lang != "" {
		req.language = string(lang)
	}
	rctx, cancel := context.WithTimeout(ctx, inferenceTimeout)
	defer cancel()
	resp, err := s.c.inference(rctx, req)
	if err != nil {
		return "", "", err
	}
	if lang != "" {
		return transcript(resp), lang, nil
	}
	lang = s.pick(resp)
	if got, ok := languageCode(resp.Language); !ok || got != lang {
		// whisper transcribed in a third language: do it again in ours.
		req.language = string(lang)
		if resp, err = s.c.inference(rctx, req); err != nil {
			return "", "", err
		}
	}
	return transcript(resp), lang, nil
}

// pick chooses between English and Spanish (AI-10): the likelier of the two
// in the language probabilities, else the detected language, else the
// language of the previous segment.
func (s *stream) pick(r response) domain.LanguageCode {
	en, okEN := r.LanguageProbabilities["en"]
	es, okES := r.LanguageProbabilities["es"]
	switch {
	case (okEN || okES) && en > es:
		return "en"
	case (okEN || okES) && es > en:
		return "es"
	}
	for _, name := range []string{r.DetectedLanguage, r.Language} {
		if l, ok := languageCode(name); ok {
			return l
		}
	}
	return s.prev
}

// queue hands chunks from segment to transcribe. Finals are kept in order;
// of the interims only the latest is kept, and it is dropped once its
// utterance's final is queued. Finals go first, so a slow server skips
// interims instead of delaying finals.
type queue struct {
	mu      sync.Mutex
	finals  []chunk
	interim *chunk
	closed  bool
	wake    chan struct{}
}

func newQueue() *queue { return &queue{wake: make(chan struct{}, 1)} }

func (q *queue) push(c chunk) {
	q.mu.Lock()
	if c.final {
		q.finals = append(q.finals, c)
		if q.interim != nil && q.interim.utterance == c.utterance {
			q.interim = nil
		}
	} else {
		q.interim = &c
	}
	q.mu.Unlock()
	q.signal()
}

func (q *queue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

func (q *queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// next blocks for the next chunk; ok is false once the queue is closed and
// drained, or ctx is done.
func (q *queue) next(ctx context.Context) (chunk, bool) {
	for {
		q.mu.Lock()
		switch {
		case len(q.finals) > 0:
			c := q.finals[0]
			q.finals = q.finals[1:]
			q.mu.Unlock()
			return c, true
		case q.interim != nil:
			c := *q.interim
			q.interim = nil
			q.mu.Unlock()
			return c, true
		case q.closed:
			q.mu.Unlock()
			return chunk{}, false
		}
		q.mu.Unlock()
		select {
		case <-q.wake:
		case <-ctx.Done():
			return chunk{}, false
		}
	}
}
