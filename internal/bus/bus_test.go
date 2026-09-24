// SPDX-License-Identifier: Apache-2.0

package bus

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func capt(track, seg, text string, final bool, start float32) domain.BusMessage {
	return domain.BusMessage{Type: api.CaptionsServerMessageTypeCaption, Caption: &domain.CaptionEvent{
		SessionId: "main", Lang: track, SegmentId: seg, Text: text, Final: final, Start: start, End: start + 1,
	}}
}

// summary renders captions as "seg:text" (interims as "seg:text~").
func summary(cs []domain.CaptionEvent) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		s := c.SegmentId + ":" + c.Text
		if !c.Final {
			s += "~"
		}
		out = append(out, s)
	}
	return out
}

func recv(t *testing.T, ch <-chan domain.BusMessage) domain.BusMessage {
	t.Helper()
	select {
	case m, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message")
	}
	return domain.BusMessage{}
}

func historyOf(t *testing.T, ch <-chan domain.BusMessage) []string {
	t.Helper()
	m := recv(t, ch)
	if m.Type != api.CaptionsServerMessageTypeHistory || m.Captions == nil {
		t.Fatalf("first message = %+v, want history", m)
	}
	return summary(*m.Captions)
}

func TestHistory(t *testing.T) {
	tests := []struct {
		name    string
		size    int // history size; default 500
		publish []domain.BusMessage
		tracks  []string
		n       int
		want    []string
	}{
		{name: "empty", tracks: []string{"es"}, n: 50, want: []string{}},
		{
			name: "interims replaced by segment then final",
			publish: []domain.BusMessage{
				capt("es", "s1", "Hola", false, 0),
				capt("es", "s1", "Hola a", false, 0),
				capt("es", "s1", "Hola a todos.", true, 0),
			},
			tracks: []string{"es"}, n: 50,
			want: []string{"s1:Hola a todos."},
		},
		{
			name: "current interim after finals",
			publish: []domain.BusMessage{
				capt("es", "s1", "Uno.", true, 0),
				capt("es", "s2", "Do", false, 1),
				capt("es", "s2", "Dos", false, 1),
			},
			tracks: []string{"es"}, n: 50,
			want: []string{"s1:Uno.", "s2:Dos~"},
		},
		{
			name: "newer interim of another segment replaces the interim",
			publish: []domain.BusMessage{
				capt("es", "s1", "Uno", false, 0),
				capt("es", "s2", "Dos", false, 1),
			},
			tracks: []string{"es"}, n: 50,
			want: []string{"s2:Dos~"},
		},
		{
			name: "final of another segment keeps the interim",
			publish: []domain.BusMessage{
				capt("es", "s2", "Dos", false, 1),
				capt("es", "s1", "Uno.", true, 0),
			},
			tracks: []string{"es"}, n: 50,
			want: []string{"s1:Uno.", "s2:Dos~"},
		},
		{
			name: "late interim after final is dropped",
			publish: []domain.BusMessage{
				capt("es", "s1", "Uno.", true, 0),
				capt("es", "s1", "Un", false, 0),
			},
			tracks: []string{"es"}, n: 50,
			want: []string{"s1:Uno."},
		},
		{
			name: "final correction replaces in place",
			publish: []domain.BusMessage{
				capt("es", "s1", "Uno.", true, 0),
				capt("es", "s2", "Dos.", true, 1),
				capt("es", "s1", "Uno!", true, 0),
			},
			tracks: []string{"es"}, n: 50,
			want: []string{"s1:Uno!", "s2:Dos."},
		},
		{
			name: "ring keeps the last size finals",
			size: 3,
			publish: []domain.BusMessage{
				capt("es", "s1", "1", true, 1), capt("es", "s2", "2", true, 2), capt("es", "s3", "3", true, 3),
				capt("es", "s4", "4", true, 4), capt("es", "s5", "5", true, 5),
			},
			tracks: []string{"es"}, n: 50,
			want: []string{"s3:3", "s4:4", "s5:5"},
		},
		{
			name: "evicted segment can be finalized again",
			size: 2,
			publish: []domain.BusMessage{
				capt("es", "s1", "1", true, 1), capt("es", "s2", "2", true, 2), capt("es", "s3", "3", true, 3),
				capt("es", "s1", "1b", false, 1), // s1 left the ring, so this interim is kept
				capt("es", "s2", "2b", true, 2),  // s2 is still in the ring: replaced in place
			},
			tracks: []string{"es"}, n: 50,
			want: []string{"s2:2b", "s3:3", "s1:1b~"},
		},
		{
			name: "n limits finals per track",
			publish: []domain.BusMessage{
				capt("es", "s1", "1", true, 1), capt("es", "s2", "2", true, 2), capt("es", "s3", "3", true, 3),
				capt("es", "s4", "4", false, 4),
			},
			tracks: []string{"es"}, n: 2,
			want: []string{"s2:2", "s3:3", "s4:4~"},
		},
		{
			name: "n zero sends only the interim",
			publish: []domain.BusMessage{
				capt("es", "s1", "1", true, 1), capt("es", "s2", "2", false, 2),
			},
			tracks: []string{"es"}, n: 0,
			want: []string{"s2:2~"},
		},
		{
			name: "tracks merged by start time, other tracks excluded",
			publish: []domain.BusMessage{
				capt("es", "s1", "Uno.", true, 1), capt("source", "s1", "One.", true, 1),
				capt("es", "s2", "Dos.", true, 2), capt("source", "s2", "Two.", true, 2),
				capt("en", "s1", "One!", true, 1), capt("es", "s3", "Tr", false, 3),
			},
			tracks: []string{"source", "es", "es"}, n: 50,
			want: []string{"s1:One.", "s1:Uno.", "s2:Two.", "s2:Dos.", "s3:Tr~"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(WithHistorySize(tt.size))
			for _, m := range tt.publish {
				b.Publish("main", m)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			got := historyOf(t, b.SubscribeHistory(ctx, "main", tt.tracks, tt.n))
			if !slices.Equal(got, tt.want) {
				t.Errorf("history = %q, want %q", got, tt.want)
			}
		})
	}
}

// recvNext returns the next message that isn't a `viewers` message.
func recvNext(t *testing.T, ch <-chan domain.BusMessage) domain.BusMessage {
	t.Helper()
	for {
		if m := recv(t, ch); m.Type != api.CaptionsServerMessageTypeViewers {
			return m
		}
	}
}

func TestRouting(t *testing.T) {
	b := New(WithViewersInterval(time.Hour))
	ctx := t.Context()
	es := b.Subscribe(ctx, "main", []string{"es"})
	both := b.Subscribe(ctx, "main", []string{"es", "source"})
	other := b.Subscribe(ctx, "other", []string{"es"})
	for _, ch := range []<-chan domain.BusMessage{es, both, other} {
		historyOf(t, ch)
	}

	b.Publish("main", capt("source", "s1", "Hi", false, 0))
	b.Publish("main", capt("source", "s1", "Hi", true, 0)) // delivered, not a late interim
	b.Publish("main", capt("source", "s1", "H", false, 0)) // late interim: dropped
	b.Publish("main", capt("es", "s1", "Hola", true, 0))
	b.Publish("main", domain.BusMessage{Type: api.CaptionsServerMessageTypeCaption}) // no caption: dropped
	live := api.SessionState("live")
	b.Publish("main", domain.BusMessage{Type: api.CaptionsServerMessageTypeState, State: &live})

	want := map[string][]string{
		"es":   {"caption s1:Hola", "state"},
		"both": {"caption s1:Hi~", "caption s1:Hi", "caption s1:Hola", "state"},
	}
	for name, ch := range map[string]<-chan domain.BusMessage{"es": es, "both": both} {
		var got []string
		for range want[name] {
			m := recvNext(t, ch)
			s := string(m.Type)
			if m.Caption != nil {
				s += " " + summary([]domain.CaptionEvent{*m.Caption})[0]
			}
			got = append(got, s)
		}
		if !slices.Equal(got, want[name]) {
			t.Errorf("%s got %q, want %q", name, got, want[name])
		}
	}
	for len(other) > 0 {
		if m := <-other; m.Type != api.CaptionsServerMessageTypeViewers {
			t.Errorf("other session got %+v", m)
		}
	}
}

func TestSlowConsumerDropped(t *testing.T) {
	const buffer = 8
	b := New(WithSubscriberBuffer(buffer), WithViewersInterval(time.Hour))
	ctx := t.Context()
	slow := b.Subscribe(ctx, "main", []string{"es"}) // not read until the end
	fast := b.Subscribe(ctx, "main", []string{"es"})
	historyOf(t, fast)

	const total = 1000
	done := make(chan error)
	go func() {
		for i := range total {
			seg := fmt.Sprintf("s%d", i)
			b.Publish("main", capt("es", seg, "x", true, float32(i)))
			// The fast subscriber reads each caption before the next one.
			var m domain.BusMessage
			for m = range fast {
				if m.Type != api.CaptionsServerMessageTypeViewers {
					break
				}
			}
			if m.Caption == nil || m.Caption.SegmentId != seg {
				done <- fmt.Errorf("fast got %+v, want caption %s", m, seg)
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}

	// The slow one got at most its buffer, then its channel was closed.
	n := 0
	for range slow {
		n++
	}
	if n > buffer {
		t.Errorf("slow subscriber got %d messages, buffer is %d", n, buffer)
	}
	if v := b.Viewers("main"); v != 1 {
		t.Errorf("Viewers = %d, want 1", v)
	}
}

func TestPublishNeverBlocks(t *testing.T) {
	b := New(WithSubscriberBuffer(4))
	for range 50 {
		_ = b.Subscribe(t.Context(), "main", []string{"es"}) // never read
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 10000 {
			b.Publish("main", capt("es", fmt.Sprintf("s%d", i), "x", i%2 == 0, 0))
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked")
	}
	if v := b.Viewers("main"); v != 0 {
		t.Errorf("Viewers = %d, want 0 (all dropped)", v)
	}
}

func TestCancelAndViewers(t *testing.T) {
	b := New(WithViewersInterval(time.Hour))
	if v := b.Viewers("main"); v != 0 {
		t.Fatalf("Viewers of unknown session = %d", v)
	}
	ctx1, cancel1 := context.WithCancel(t.Context())
	ch1 := b.Subscribe(ctx1, "main", []string{"es"})
	ch2 := b.Subscribe(t.Context(), "main", []string{"es"})
	if v := b.Viewers("main"); v != 2 {
		t.Fatalf("Viewers = %d, want 2", v)
	}
	cancel1()
	historyOf(t, ch1)
	for m := range ch1 {
		if m.Type != api.CaptionsServerMessageTypeViewers {
			t.Errorf("unexpected %+v after history", m)
		}
	}
	if v := b.Viewers("main"); v != 1 {
		t.Errorf("Viewers after cancel = %d, want 1", v)
	}

	// Already-cancelled context: history, then closed, not counted.
	ch3 := b.Subscribe(ctx1, "main", []string{"es"})
	historyOf(t, ch3)
	if _, ok := <-ch3; ok {
		t.Error("channel of a cancelled subscription not closed")
	}

	b.CloseSession("main")
	historyOf(t, ch2)
	for range ch2 {
	}
	if v := b.Viewers("main"); v != 0 {
		t.Errorf("Viewers after CloseSession = %d, want 0", v)
	}
}

func TestViewersThrottled(t *testing.T) {
	const interval = 200 * time.Millisecond
	b := New(WithViewersInterval(interval))
	ctx := t.Context()
	watcher := b.Subscribe(ctx, "main", []string{"es"})
	historyOf(t, watcher)
	if m := recv(t, watcher); m.Viewers == nil || *m.Viewers != 1 {
		t.Fatalf("got %+v, want viewers=1 (leading edge)", m)
	}

	// A join storm inside one interval yields a single trailing message.
	for range 100 {
		_ = b.Subscribe(ctx, "main", []string{"en"})
	}
	start := time.Now()
	m := recv(t, watcher)
	if m.Viewers == nil || *m.Viewers != 101 {
		t.Fatalf("got %+v, want viewers=101", m)
	}
	if time.Since(start) > 2*interval {
		t.Errorf("trailing viewers message took %v", time.Since(start))
	}
	select {
	case m := <-watcher:
		t.Errorf("extra message %+v", m)
	case <-time.After(2 * interval):
	}
}

func BenchmarkPublish500(b *testing.B) {
	bus := New()
	ctx := b.Context()
	for range 500 {
		ch := bus.Subscribe(ctx, "main", []string{"es"})
		go func() {
			for range ch {
			}
		}()
	}
	m := capt("es", "s1", "Hola a todos", false, 0)
	for b.Loop() {
		bus.Publish("main", m)
	}
}
