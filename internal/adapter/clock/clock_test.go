// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clock_test

import (
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/clock"

	coreclock "github.com/altessa-s/go-atlas-core/time/clock"
)

func TestNowIsTheClocks(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if got := clock.New(coreclock.NewFake(at)).Now(); !got.Equal(at) {
		t.Fatalf("got %v", got)
	}
	if real := clock.Real().Now(); time.Since(real) > time.Minute || real.IsZero() {
		t.Fatalf("real clock says %v", real)
	}
}

func TestNewNeedsAClock(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("a nil clock did not panic")
		}
	}()
	clock.New(nil)
}

func TestSleepIsTheClocks(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	fake := coreclock.NewFake(at)
	c := clock.New(fake)
	if err := c.Sleep(t.Context(), time.Minute); err != nil || !c.Now().Equal(at.Add(time.Minute)) {
		t.Fatalf("%v %v", c.Now(), err)
	}
}
