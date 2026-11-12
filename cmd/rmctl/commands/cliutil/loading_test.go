// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// timers are the timers a line asked for; a test fires them by hand.
type timers struct {
	delays  []time.Duration
	fire    []func()
	stopped int
}

func (t *timers) after(d time.Duration, f func()) func() {
	t.delays = append(t.delays, d)
	t.fire = append(t.fire, f)
	return func() { t.stopped++ }
}

// loadingAt is a loading line on a terminal with a clock and timers the
// test moves.
func loadingAt(buf *bytes.Buffer, now *time.Time, tm *timers, getenv func(string) string) Streams {
	return Streams{Err: buf, IsTerminal: true, Now: func() time.Time { return *now }, After: tm.after, Getenv: getenv}
}

func noEnv(string) string { return "" }

func TestLoadingOffATerminal(t *testing.T) {
	t.Parallel()
	if obs := Loading(Streams{Err: &bytes.Buffer{}}); obs != nil {
		t.Fatalf("a loading line off a terminal: %v", obs)
	}
}

// A load that ends within the delay prints nothing, and its timer, stopped
// or fired late, shows nothing either.
func TestLoadingStaysSilentForAQuickLoad(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tm := &timers{}
	obs := Loading(loadingAt(&buf, &now, tm, noEnv))
	obs.LoadDone() // no load to end
	for done := range 4 {
		obs.LoadProgress(done, 3)
	}
	obs.LoadDone()
	if len(tm.delays) != 1 || tm.delays[0] != LoadingDelay || tm.stopped != 1 {
		t.Fatalf("timers %+v", tm)
	}
	tm.fire[0]()
	if buf.Len() != 0 {
		t.Fatalf("a quick load printed %q", buf.String())
	}
}

// Past the delay the line shows what is read, is redrawn at most every
// tenth of a second, and is erased when the load ends; the next load waits
// for its own delay.
func TestLoadingShowsASlowLoadAndErasesIt(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tm := &timers{}
	obs := Loading(loadingAt(&buf, &now, tm, noEnv))
	obs.LoadProgress(0, 509)
	obs.LoadProgress(1, 509)
	if buf.Len() != 0 {
		t.Fatalf("printed before the delay: %q", buf.String())
	}
	now = now.Add(LoadingDelay)
	tm.fire[0]()
	tm.fire[0]() // a timer fires once, and twice changes nothing
	if got, want := buf.String(), "\r\x1b[Kreading the library  1/509 items"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	buf.Reset()
	obs.LoadProgress(2, 509) // within the tenth: skipped
	now = now.Add(200 * time.Millisecond)
	obs.LoadProgress(123, 509)
	if got, want := buf.String(), "\r\x1b[Kreading the library  123/509 items"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	buf.Reset()
	obs.LoadDone()
	obs.LoadDone()
	if got := buf.String(); got != "\r\x1b[K" {
		t.Fatalf("the line is not erased once: %q", got)
	}
	buf.Reset()
	// The first load's timer is spent; the second waits again.
	obs.LoadProgress(0, 2)
	tm.fire[0]()
	if buf.Len() != 0 || len(tm.delays) != 2 {
		t.Fatalf("the next load showed at once: %q, timers %+v", buf.String(), tm.delays)
	}
	tm.fire[1]()
	obs.LoadDone()
	if got, want := buf.String(), "\r\x1b[Kreading the library  0/2 items\r\x1b[K"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	if strings.Contains(buf.String(), "\n") {
		t.Fatalf("the line left a line behind: %q", buf.String())
	}
}

// A dumb terminal cannot clear a line: it is erased with spaces.
func TestLoadingOnADumbTerminal(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tm := &timers{}
	getenv := func(name string) string {
		if name == "TERM" {
			return "dumb"
		}
		return ""
	}
	obs := Loading(loadingAt(&buf, &now, tm, getenv))
	obs.LoadProgress(10, 20)
	tm.fire[0]()
	obs.LoadDone()
	line := LoadingLine(10, 20)
	if got, want := buf.String(), "\r"+line+"\r"+strings.Repeat(" ", len(line))+"\r"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

// A timer that fires as it is set shows the line from the first call, and
// with no timer given the machine's is used and stopped.
func TestLoadingTimers(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	at := func(_ time.Duration, f func()) func() {
		f()
		return func() {}
	}
	obs := Loading(Streams{Err: &buf, IsTerminal: true, After: at, Getenv: noEnv})
	obs.LoadProgress(0, 7)
	obs.LoadDone()
	if got, want := buf.String(), "\r\x1b[Kreading the library  0/7 items\r\x1b[K"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	buf.Reset()
	obs = Loading(Streams{Err: &buf, IsTerminal: true, Getenv: noEnv})
	obs.LoadProgress(0, 7)
	obs.LoadDone()
	if buf.Len() != 0 {
		t.Fatalf("the machine's timer fired at once: %q", buf.String())
	}
}
