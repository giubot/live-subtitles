// SPDX-License-Identifier: Apache-2.0

package backoff

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDelay(t *testing.T) {
	tests := []struct {
		name     string
		p        Policy
		attempt  int
		min, max time.Duration
	}{
		{"first", Policy{Base: 100 * time.Millisecond, Max: time.Second}, 1, 100 * time.Millisecond, 100 * time.Millisecond},
		{"doubles", Policy{Base: 100 * time.Millisecond, Max: time.Second}, 3, 400 * time.Millisecond, 400 * time.Millisecond},
		{"capped", Policy{Base: 100 * time.Millisecond, Max: time.Second}, 6, time.Second, time.Second},
		{"no overflow", Policy{Base: time.Second, Max: time.Minute}, 1000, time.Minute, time.Minute},
		{"attempt 0 is the first", Policy{Base: time.Second}, 0, time.Second, time.Second},
		{"uncapped", Policy{Base: time.Millisecond}, 11, 1024 * time.Millisecond, 1024 * time.Millisecond},
		{"jitter", Policy{Base: time.Second, Max: time.Second, Jitter: 0.5}, 1, 500 * time.Millisecond, time.Second},
		{"jitter above 1 is full jitter", Policy{Base: time.Second, Jitter: 3}, 1, 0, time.Second},
		{"no base", Policy{Max: time.Second}, 4, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 100 {
				if d := tt.p.Delay(tt.attempt); d < tt.min || d > tt.max {
					t.Fatalf("Delay(%d) = %v, want %v..%v", tt.attempt, d, tt.min, tt.max)
				}
			}
		})
	}
}

func TestWait(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		d    time.Duration
		want error
	}{
		{"elapses", t.Context(), time.Millisecond, nil},
		{"zero", t.Context(), 0, nil},
		{"cancelled", cancelled, time.Hour, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Wait(tt.ctx, tt.d); !errors.Is(err, tt.want) {
				t.Errorf("Wait = %v, want %v", err, tt.want)
			}
		})
	}
}
