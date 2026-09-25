// SPDX-License-Identifier: Apache-2.0

package session

import (
	"sort"
	"time"
)

// arrivalWindow bounds the frame arrivals a run keeps for latency: at
// 20 ms frames, about 80 s of audio, far more than any caption waits.
const arrivalWindow = 4096

type arrival struct {
	end time.Duration // session clock at the end of the frame
	at  time.Time     // wall time the frame reached the pipeline
}

// arrivals maps the session clock to wall time: when the audio up to a
// point had reached the server. A caption's latency is its emit time
// minus the arrival of the end of its audio (AI-9), which holds for paced
// and unpaced sources, clock drift and reconnect gaps alike.
type arrivals struct {
	ring []arrival
	next int // index of the oldest entry once the ring is full
}

func (a *arrivals) len() int { return len(a.ring) }

// get returns the i-th oldest entry.
func (a *arrivals) get(i int) arrival { return a.ring[(a.next+i)%len(a.ring)] }

// add records that the audio up to end arrived at at. Entries must move
// forward on the session clock; others are ignored.
func (a *arrivals) add(end time.Duration, at time.Time) {
	if n := a.len(); n > 0 && end <= a.get(n-1).end {
		return
	}
	if len(a.ring) < arrivalWindow {
		a.ring = append(a.ring, arrival{end, at})
		return
	}
	a.ring[a.next] = arrival{end, at}
	a.next = (a.next + 1) % arrivalWindow
}

// at is when the audio up to end had arrived: the arrival of the first
// frame that ends at or after it. Audio past the newest frame maps to the
// newest arrival; audio older than the window is extrapolated back from
// the oldest one.
func (a *arrivals) at(end time.Duration) (time.Time, bool) {
	n := a.len()
	if n == 0 {
		return time.Time{}, false
	}
	i := sort.Search(n, func(i int) bool { return a.get(i).end >= end })
	switch {
	case i == n:
		return a.get(n - 1).at, true
	case i == 0 && n == arrivalWindow:
		first := a.get(0)
		return first.at.Add(end - first.end), true
	}
	return a.get(i).at, true
}

func secondsToDuration(s float32) time.Duration {
	return time.Duration(float64(s) * float64(time.Second))
}
