// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"slices"
	"sync"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// item is a source caption waiting for translation, with the final
// sentences before it as context.
type item struct {
	c       api.Caption
	context []string
}

// worker translates the source track into one target language (AI-5,
// AI-6). Finals are translated in order and never dropped. Of the interims,
// only the newest waits: while a translation is in flight, newer interims
// replace older ones, which debounces them to the translator's pace. A
// target equal to the caption's source language passes the text through.
//
// P2-02 moves this into internal/translate with glossary prompts.
type worker struct {
	r    *run
	lang domain.LanguageCode

	mu      sync.Mutex
	finals  []item
	interim *item
	closed  bool
	wake    chan struct{}
	done    chan struct{}
}

func newWorker(r *run, lang domain.LanguageCode) *worker {
	return &worker{r: r, lang: lang, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func (w *worker) push(it item) {
	w.mu.Lock()
	if it.c.Final {
		w.finals = append(w.finals, it)
		if w.interim != nil && w.interim.c.SegmentId == it.c.SegmentId {
			w.interim = nil // superseded by its final
		}
	} else {
		w.interim = &it
	}
	w.mu.Unlock()
	w.signal()
}

// close lets the worker finish the queued finals and exit.
func (w *worker) close() {
	w.mu.Lock()
	w.closed = true
	w.interim = nil // pending interims are moot once the stream has ended
	w.mu.Unlock()
	w.signal()
}

func (w *worker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *worker) next() (it item, ok, closed bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case len(w.finals) > 0:
		it = w.finals[0]
		w.finals = slices.Delete(w.finals, 0, 1)
		return it, true, false
	case w.interim != nil:
		it = *w.interim
		w.interim = nil
		return it, true, false
	}
	return it, false, w.closed
}

func (w *worker) loop() {
	defer close(w.done)
	for {
		it, ok, closed := w.next()
		if closed {
			return
		}
		if !ok {
			select {
			case <-w.wake:
			case <-w.r.ctx.Done():
				return
			}
			continue
		}
		w.translate(it)
	}
}

func (w *worker) translate(it item) {
	r := w.r
	src := it.c
	text := src.Text
	if src.SourceLang != w.lang {
		ctx, cancel := context.WithTimeout(r.ctx, r.m.opts.TranslateTimeout)
		res, err := r.translator.Translate(ctx, domain.TranslateRequest{
			SessionID: r.id,
			SegmentID: src.SegmentId,
			Text:      src.Text,
			From:      src.SourceLang,
			To:        w.lang,
			Final:     src.Final,
			Context:   it.context,
		})
		cancel()
		if err != nil {
			if r.errCanceled(err) {
				return
			}
			r.m.log.Warn("translation failed", "session", r.id, "lang", w.lang, "segment", src.SegmentId, "final", src.Final, "err", err)
			if src.Final {
				r.fail(api.Error{Code: CodeTranslationFailed, Message: err.Error(), Params: &map[string]any{"lang": w.lang}})
				r.m.logEvent(api.AdminEventLogLevelWarn, CodeTranslationFailed, r.id, map[string]any{"lang": w.lang})
			}
			return
		}
		r.addUsage(res.Usage)
		text = res.Text
	}
	if text == "" {
		return
	}
	out := src
	out.Lang, out.Text = w.lang, text
	out.LatencyMs = r.latencyMs(secondsToDuration(src.End))
	r.publish(out)
}
