// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clock

import (
	"context"
	"time"

	"github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/rmctl/internal/port"
)

// System is port.Clock and port.Timer over a go-atlas-core clock.
type System struct {
	clock clock.Clock
}

var _ port.Timer = System{}

// New returns port.Clock over c. A nil c is a programming error and panics.
func New(c clock.Clock) System {
	if c == nil {
		panic("clock: a clock is required")
	}
	return System{clock: c}
}

// Real is the system clock.
func Real() System { return New(clock.Real()) }

// Now is the current time.
func (s System) Now() time.Time { return s.clock.Now() }

// Sleep waits d, or until ctx ends, which is its error.
func (s System) Sleep(ctx context.Context, d time.Duration) error { return s.clock.Sleep(ctx, d) }
