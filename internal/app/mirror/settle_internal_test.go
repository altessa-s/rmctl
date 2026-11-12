// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"testing"
	"time"
)

// busyTimer is a clock on which a change comes during every sleep, as a
// file written to all the time makes them.
type busyTimer struct {
	now    time.Time
	wake   chan struct{}
	sleeps int
}

func (b *busyTimer) Now() time.Time { return b.now }

func (b *busyTimer) Sleep(_ context.Context, d time.Duration) error {
	b.sleeps++
	b.now = b.now.Add(d)
	select {
	case b.wake <- struct{}{}:
	default:
	}
	return nil
}

// Changes that never stop hold a pass back maxQuiet at the most; changes
// that stop make it wait one debounce.
func TestSettleEndsWhileChangesGoOn(t *testing.T) {
	t.Parallel()
	busy := &busyTimer{wake: make(chan struct{}, 1)}
	e := &Engine{timer: busy}
	busy.wake <- struct{}{}
	if !e.settle(t.Context(), busy.wake, 10*time.Second) || busy.sleeps != 3 {
		t.Fatalf("%d sleeps", busy.sleeps)
	}
	quiet := &busyTimer{wake: make(chan struct{}, 1)}
	e = &Engine{timer: quietTimer{quiet}}
	quiet.wake <- struct{}{}
	if !e.settle(t.Context(), quiet.wake, time.Second) {
		t.Fatal("no pass")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if e.settle(ctx, make(chan struct{}), time.Second) {
		t.Fatal("a pass after the context ended")
	}
}

// quietTimer sleeps without a change coming.
type quietTimer struct{ *busyTimer }

func (q quietTimer) Sleep(_ context.Context, d time.Duration) error {
	q.now = q.now.Add(d)
	return nil
}
