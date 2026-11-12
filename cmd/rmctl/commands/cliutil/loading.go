// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"fmt"
	"sync"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// LoadingDelay is how long a load of the library runs in silence. A load
// from the cache takes a fraction of a second and a small library on a good
// line about one, so a line shown sooner would only flash; past two seconds
// a person starts to wonder whether the command hangs.
const LoadingDelay = 2 * time.Second

// Loading is a line on the error stream for a person at a terminal while a
// load of the library runs longer than [LoadingDelay], as it does with a
// cold cache on a slow line or while the cloud makes the tool wait: the
// items read of the items in the library, rewritten in place at most every
// tenth of a second. The line is erased when the load ends, so what the
// command writes next stands where it was. It is nil, and prints nothing,
// anywhere else.
func Loading(streams Streams) port.LoadObserver {
	if !streams.IsTerminal {
		return nil
	}
	after := streams.After
	if after == nil {
		after = func(d time.Duration, f func()) func() {
			timer := time.AfterFunc(d, f)
			return func() { timer.Stop() }
		}
	}
	return &loading{line: newStatusLine(streams), now: streams.clock(), after: after}
}

type loading struct {
	mu    sync.Mutex
	line  statusLine
	now   func() time.Time
	after func(time.Duration, func()) (stop func())
	// load numbers the loads, so a timer that fires after its load ended
	// shows nothing.
	load        int
	active      bool
	shown       bool
	stop        func()
	drawn       time.Time
	done, total int
}

func (l *loading) LoadProgress(done, total int) {
	l.mu.Lock()
	l.done, l.total = done, total
	if l.active {
		if l.shown && l.now().Sub(l.drawn) >= redraw {
			l.draw()
		}
		l.mu.Unlock()
		return
	}
	l.active = true
	l.load++
	load := l.load
	l.mu.Unlock()
	// The timer is set outside the lock: one that fires at once shows the
	// line from here.
	stop := l.after(LoadingDelay, func() { l.show(load) })
	l.mu.Lock()
	l.stop = stop
	l.mu.Unlock()
}

// show draws the line once the load numbered load has run for the delay.
func (l *loading) show(load int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active || l.load != load || l.shown {
		return
	}
	l.shown = true
	l.draw()
}

func (l *loading) draw() {
	l.drawn = l.now()
	l.line.draw(LoadingLine(l.done, l.total))
}

func (l *loading) LoadDone() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stop != nil {
		l.stop()
		l.stop = nil
	}
	l.line.erase()
	l.active, l.shown = false, false
}

// LoadingLine is the line of a load that has read done of total items:
//
//	reading the library  123/509 items
func LoadingLine(done, total int) string {
	return fmt.Sprintf("reading the library  %d/%d items", done, total)
}
