// SPDX-License-Identifier: Apache-2.0

package translate

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Defaults for Options.
const (
	DefaultTimeout         = 15 * time.Second
	DefaultPartialInterval = 150 * time.Millisecond
)

// Warmer is implemented by translators that benefit from loading their
// model before the first caption (Gemma via Ollama). New calls Warm in the
// background.
type Warmer interface {
	Warm(ctx context.Context) error
}

// Options configure a Fanout. Translator and Publish are required.
type Options struct {
	SessionID  string
	Translator domain.Translator
	// Targets are the target tracks; empty, duplicate and `source` entries
	// are ignored.
	Targets []domain.LanguageCode
	// ContextSentences is how many previous final source sentences go with
	// each request (0: none). See ContextSentences for the settings value.
	ContextSentences int
	// Glossary goes into every request (AI-7), and its do-not-translate
	// entries are enforced on the output (see EnforceDoNotTranslate).
	Glossary *domain.Glossary
	// Timeout bounds one translation call (default DefaultTimeout).
	Timeout time.Duration
	// PartialInterval is the minimum time between streamed partial
	// translations of a final (default DefaultPartialInterval; negative:
	// no limit).
	PartialInterval time.Duration

	// Publish gets each translated caption: the source caption with Lang
	// set to the target and Text translated. It's called from the target's
	// goroutine, in order per target.
	Publish func(c api.Caption)
	// Usage, if set, gets the provider usage of each call.
	Usage func(u domain.Usage)
	// Failed, if set, is told when a final can't be translated; that line
	// is then missing from the lang track. Failed interims are only logged.
	Failed func(lang domain.LanguageCode, c api.Caption, err error)
	Logger *slog.Logger
}

// Fanout translates one session's source captions into its target tracks
// (AI-5, AI-6). Each target has its own goroutine:
//
//   - Finals are translated in order and never dropped.
//   - Only the newest interim waits: while a translation is in flight,
//     newer interims replace older ones, which debounces them to the
//     translator's pace. A final supersedes the pending interim of its
//     segment.
//   - A target equal to the caption's source language passes the text
//     through without a translation call.
//   - With a domain.StreamingTranslator, a final's partial output is
//     published as an interim of its segment while it streams, but never
//     shorter than the interim already shown.
//
// Each request carries the last ContextSentences final source sentences.
type Fanout struct {
	opts    Options
	ctx     context.Context
	log     *slog.Logger
	targets []*target

	mu      sync.Mutex
	history []string // last final source sentences, oldest first
}

// New starts the target goroutines; they stop when ctx is cancelled or
// after Close.
func New(ctx context.Context, opts Options) *Fanout {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.PartialInterval == 0 {
		opts.PartialInterval = DefaultPartialInterval
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	opts.ContextSentences = max(opts.ContextSentences, 0)
	f := &Fanout{opts: opts, ctx: ctx, log: opts.Logger}
	seen := map[string]bool{domain.SourceTrack: true, "": true}
	for _, lang := range opts.Targets {
		if seen[lang] {
			continue
		}
		seen[lang] = true
		t := &target{f: f, lang: lang, wake: make(chan struct{}, 1), done: make(chan struct{})}
		f.targets = append(f.targets, t)
		go t.loop()
	}
	if w, ok := opts.Translator.(Warmer); ok && len(f.targets) > 0 {
		go func() {
			if err := w.Warm(ctx); err != nil && ctx.Err() == nil {
				f.log.Warn("translator warm-up failed", "session", opts.SessionID, "provider", opts.Translator.Kind(), "err", err)
			}
		}()
	}
	return f
}

// Targets are the target languages being translated, in order.
func (f *Fanout) Targets() []domain.LanguageCode {
	out := make([]domain.LanguageCode, len(f.targets))
	for i, t := range f.targets {
		out[i] = t.lang
	}
	return out
}

// Push queues a source caption for every target. Call it from one
// goroutine, in caption order.
func (f *Fanout) Push(c api.Caption) {
	if c.Text == "" {
		return
	}
	it := item{c: c, context: f.context(c)}
	for _, t := range f.targets {
		t.push(it)
	}
}

// Close lets the targets finish their queued finals and waits for them.
// Pending interims are dropped. Cancel ctx to abort instead.
func (f *Fanout) Close() {
	for _, t := range f.targets {
		t.close()
	}
	for _, t := range f.targets {
		<-t.done
	}
}

// context returns the context for c and, when c is final, adds it to the
// history.
func (f *Fanout) context(c api.Caption) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx := slices.Clone(f.history)
	if c.Final && f.opts.ContextSentences > 0 {
		f.history = append(f.history, c.Text)
		if n := len(f.history) - f.opts.ContextSentences; n > 0 {
			f.history = slices.Delete(f.history, 0, n)
		}
	}
	return ctx
}

// item is a source caption waiting for translation, with the final
// sentences before it as context.
type item struct {
	c       api.Caption
	context []string
}

// target translates the source track into one language.
type target struct {
	f    *Fanout
	lang domain.LanguageCode

	mu      sync.Mutex
	finals  []item
	interim *item
	closed  bool
	wake    chan struct{}
	done    chan struct{}

	// The interim shown for the current segment, so a streamed final
	// never shrinks it.
	shownSeg  string
	shownText string
}

func (t *target) push(it item) {
	t.mu.Lock()
	if it.c.Final {
		t.finals = append(t.finals, it)
		if t.interim != nil && t.interim.c.SegmentId == it.c.SegmentId {
			t.interim = nil // superseded by its final
		}
	} else {
		t.interim = &it
	}
	t.mu.Unlock()
	t.signal()
}

func (t *target) close() {
	t.mu.Lock()
	t.closed = true
	t.interim = nil // pending interims are moot once the stream has ended
	t.mu.Unlock()
	t.signal()
}

func (t *target) signal() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *target) next() (it item, ok, closed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case len(t.finals) > 0:
		it = t.finals[0]
		t.finals = slices.Delete(t.finals, 0, 1)
		return it, true, false
	case t.interim != nil:
		it = *t.interim
		t.interim = nil
		return it, true, false
	}
	return it, false, t.closed
}

func (t *target) loop() {
	defer close(t.done)
	for {
		it, ok, closed := t.next()
		if closed {
			return
		}
		if !ok {
			select {
			case <-t.wake:
			case <-t.f.ctx.Done():
				return
			}
			continue
		}
		if t.f.ctx.Err() != nil {
			return
		}
		t.translate(it)
	}
}

func (t *target) translate(it item) {
	f, src := t.f, it.c
	if src.SourceLang == t.lang {
		t.emit(src, src.Text)
		return
	}
	req := domain.TranslateRequest{
		SessionID: f.opts.SessionID,
		SegmentID: src.SegmentId,
		Text:      src.Text,
		From:      src.SourceLang,
		To:        t.lang,
		Final:     src.Final,
		Context:   it.context,
		Glossary:  f.opts.Glossary,
	}
	ctx, cancel := context.WithTimeout(f.ctx, f.opts.Timeout)
	var res domain.TranslateResult
	var err error
	if st, ok := f.opts.Translator.(domain.StreamingTranslator); ok && src.Final {
		res, err = st.TranslateStream(ctx, req, t.partial(src))
	} else {
		res, err = f.opts.Translator.Translate(ctx, req)
	}
	cancel()
	if err != nil {
		if errors.Is(err, context.Canceled) && f.ctx.Err() != nil {
			return // the session is going away
		}
		f.log.Warn("translation failed", "session", f.opts.SessionID, "lang", t.lang, "segment", src.SegmentId, "final", src.Final, "err", err)
		if src.Final && f.opts.Failed != nil {
			f.opts.Failed(t.lang, src, err)
		}
		return
	}
	if f.opts.Usage != nil {
		f.opts.Usage(res.Usage)
	}
	t.emit(src, t.keepTerms(req, res.Text))
}

// partial publishes a final's streamed output as an interim of its
// segment, throttled and never shorter than what's shown.
func (t *target) partial(src api.Caption) func(string) {
	var last time.Time
	return func(text string) {
		text = CleanOutput(text)
		if text == "" || t.f.ctx.Err() != nil {
			return
		}
		if iv := t.f.opts.PartialInterval; iv > 0 {
			now := time.Now()
			if now.Sub(last) < iv {
				return
			}
			last = now
		}
		t.mu.Lock()
		stale := src.SegmentId == t.shownSeg &&
			(text == t.shownText || utf8.RuneCountInString(text) < utf8.RuneCountInString(t.shownText))
		t.mu.Unlock()
		if stale {
			return
		}
		text, _ = EnforceDoNotTranslate(t.f.opts.Glossary, src.Text, text)
		c := src
		c.Final = false
		t.emit(c, text)
	}
}

func (t *target) emit(src api.Caption, text string) {
	if text == "" {
		return
	}
	t.mu.Lock()
	if src.Final {
		t.shownSeg, t.shownText = "", ""
	} else {
		t.shownSeg, t.shownText = src.SegmentId, text
	}
	t.mu.Unlock()
	out := src
	out.Lang, out.Text = t.lang, text
	t.f.opts.Publish(out)
}
