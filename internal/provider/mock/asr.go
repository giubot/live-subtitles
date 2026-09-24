// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// ASR is a domain.ASRProvider that ignores the audio content and "hears"
// a script, paced by the audio clock: one word every WordEvery of audio,
// interim events per word and a final event per sentence.
type ASR struct {
	// Script defaults to DefaultScript.
	Script []Line
	// WordEvery is the audio time per word; default 300 ms.
	WordEvery time.Duration
	// Latency delays every event by this much wall time, to mimic a real provider.
	Latency time.Duration
}

var _ domain.ASRProvider = (*ASR)(nil)

func (a *ASR) Kind() domain.ProviderKind { return api.ProviderKindMock }

func (a *ASR) Start(ctx context.Context, cfg domain.ASRConfig) (chan<- domain.AudioFrame, <-chan domain.ASREvent, error) {
	script := a.Script
	if len(script) == 0 {
		script = DefaultScript
	}
	every := a.WordEvery
	if every <= 0 {
		every = 300 * time.Millisecond
	}
	in := make(chan domain.AudioFrame, 64)
	out := make(chan domain.ASREvent, 64)
	events := out
	if a.Latency > 0 {
		events = make(chan domain.ASREvent, 64)
		go delay(ctx, events, out, a.Latency)
	}
	go func() {
		defer close(events)
		s := speaker{script: script, every: every, lang: cfg.SourceLanguage}
		for {
			select {
			case f, ok := <-in:
				if !ok {
					if ev, ok := s.flush(); ok {
						send(ctx, events, ev)
					}
					return
				}
				for _, ev := range s.advance(f.End()) {
					if !send(ctx, events, ev) {
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return in, out, nil
}

// speaker tracks where in the script the audio clock is.
type speaker struct {
	script []Line
	every  time.Duration
	lang   domain.SourceLanguage

	line     int           // lines finished so far (keeps counting past the end; the script loops)
	words    []string      // current line, nil between lines
	said     int           // words of the current line emitted
	start    time.Duration // audio offset of the current line's first word
	nextWord time.Duration // audio offset at which the next word is due
}

// advance emits every event due up to audio offset now.
func (s *speaker) advance(now time.Duration) []domain.ASREvent {
	var evs []domain.ASREvent
	for now >= s.nextWord {
		if s.words == nil {
			s.words = strings.Fields(s.text())
			s.said = 0
			s.start = s.nextWord
		}
		s.said++
		final := s.said == len(s.words)
		evs = append(evs, s.event(s.nextWord+s.every, final))
		s.nextWord += s.every
		if final {
			s.words = nil
			s.line++
			s.nextWord += 2 * s.every // pause between sentences
		}
	}
	return evs
}

// flush finalizes a sentence cut short by the end of the audio.
func (s *speaker) flush() (domain.ASREvent, bool) {
	if s.words == nil || s.said == 0 {
		return domain.ASREvent{}, false
	}
	ev := s.event(s.nextWord, true)
	s.words = nil
	s.line++
	return ev, true
}

func (s *speaker) event(end time.Duration, final bool) domain.ASREvent {
	return domain.ASREvent{
		SegmentID: fmt.Sprintf("s-%06d", s.line+1),
		Text:      strings.Join(s.words[:s.said], " "),
		Final:     final,
		Start:     s.start,
		End:       end,
		Lang:      s.language(),
	}
}

func (s *speaker) text() string {
	l := s.script[s.line%len(s.script)]
	if s.language() == "es" {
		return l.ES
	}
	return l.EN
}

// language is the pinned language, or with `auto` alternates every three lines.
func (s *speaker) language() domain.LanguageCode {
	switch s.lang {
	case api.En, api.Es:
		return string(s.lang)
	}
	if (s.line/3)%2 == 1 {
		return "es"
	}
	return "en"
}

func send[T any](ctx context.Context, ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	case <-ctx.Done():
		return false
	}
}

// delay forwards events from in to out, each d after it arrived.
func delay(ctx context.Context, in <-chan domain.ASREvent, out chan<- domain.ASREvent, d time.Duration) {
	defer close(out)
	type due struct {
		ev domain.ASREvent
		at time.Time
	}
	queue := make(chan due, cap(in))
	go func() {
		defer close(queue)
		for ev := range in {
			if !send(ctx, queue, due{ev, time.Now().Add(d)}) {
				return
			}
		}
	}()
	for q := range queue {
		t := time.NewTimer(time.Until(q.at))
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return
		}
		if !send(ctx, out, q.ev) {
			return
		}
	}
}
