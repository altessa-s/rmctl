// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// The shape of a transfer line.
const (
	// barWidth is the cells of the bar between its brackets.
	barWidth = 20
	// redraw is the least time between two drawings of the line, so a fast
	// transfer does not flood the terminal.
	redraw = 100 * time.Millisecond
	// warmup is how long a transfer runs before its rate and time left
	// mean anything.
	warmup = time.Second
)

// Transfer is a progress bar on the error stream for a person at a
// terminal: a bar, bytes and files done of their totals, the rate and the
// time left, rewritten in place at most every tenth of a second. It is nil,
// and prints nothing, anywhere else: a log gets no carriage returns. On a
// terminal that takes no escape sequences (TERM=dumb) the line is padded
// with spaces instead of cleared. done finishes the line. update may be
// called from any goroutine.
func Transfer(streams Streams, label string) (update func(port.SyncProgress), done func()) {
	if !streams.IsTerminal {
		return nil, func() {}
	}
	t := &transfer{line: newStatusLine(streams), label: label, now: streams.clock()}
	return t.update, t.done
}

// clock is the wall clock of the progress lines: Now, or the machine's.
func (s Streams) clock() func() time.Time {
	if s.Now != nil {
		return s.Now
	}
	return time.Now //nolint:forbidigo // a progress line's time is wall time, not the library's clock
}

type transfer struct {
	mu    sync.Mutex
	line  statusLine
	label string
	now   func() time.Time
	start time.Time
	drawn time.Time
	last  port.SyncProgress
}

// statusLine is one line of the error stream rewritten in place. On a
// terminal that takes no escape sequences (TERM=dumb) it is padded with
// spaces instead of cleared.
type statusLine struct {
	w      io.Writer
	dumb   bool
	width  int
	inLine bool
}

func newStatusLine(streams Streams) statusLine {
	return statusLine{w: streams.Err, dumb: streams.getenv("TERM") == "dumb"}
}

// draw writes text over what the line showed.
func (l *statusLine) draw(text string) {
	if l.dumb {
		pad := max(l.width-len(text), 0)
		_, _ = fmt.Fprintf(l.w, "\r%s%s", text, strings.Repeat(" ", pad)) //nolint:errcheck // progress
	} else {
		_, _ = fmt.Fprintf(l.w, "\r\x1b[K%s", text) //nolint:errcheck // progress
	}
	l.width = len(text)
	l.inLine = true
}

// finish keeps what the line shows and moves to the next one.
func (l *statusLine) finish() {
	if l.inLine {
		_, _ = fmt.Fprintln(l.w) //nolint:errcheck // progress
		l.inLine = false
	}
}

// erase takes what the line shows away and leaves the cursor at its start,
// so what is written next stands where the line was.
func (l *statusLine) erase() {
	if !l.inLine {
		return
	}
	if l.dumb {
		_, _ = fmt.Fprintf(l.w, "\r%s\r", strings.Repeat(" ", l.width)) //nolint:errcheck // progress
	} else {
		_, _ = fmt.Fprint(l.w, "\r\x1b[K") //nolint:errcheck // progress
	}
	l.width = 0
	l.inLine = false
}

func (t *transfer) update(p port.SyncProgress) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if t.start.IsZero() {
		t.start = now
	}
	t.last = p
	finished := p.FilesTotal > 0 && p.Files >= p.FilesTotal
	if !finished && t.line.inLine && now.Sub(t.drawn) < redraw {
		return
	}
	t.drawn = now
	t.line.draw(TransferLine(t.label, p, now.Sub(t.start)))
}

func (t *transfer) done() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.line.finish()
}

// TransferLine is one line of a transfer's progress after elapsed:
//
//	syncing [=========>          ]  45%  12.3 MiB/27.4 MiB  120/310 files  2.1 MiB/s  0:07 left
//
// The rate and the time left wait for a second of transfer.
func TransferLine(label string, p port.SyncProgress, elapsed time.Duration) string {
	pct := percent
	if p.BytesTotal > 0 {
		pct = int(min(max(p.Bytes, 0)*percent/p.BytesTotal, percent))
	}
	filled := pct * barWidth / percent
	bar := strings.Repeat("=", filled)
	if filled < barWidth {
		bar += ">" + strings.Repeat(" ", barWidth-filled-1)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s] %3d%%  %s/%s  %d/%d files", label, bar, pct,
		Size(max(p.Bytes, 0)), Size(p.BytesTotal), p.Files, p.FilesTotal)
	if elapsed >= warmup && p.Bytes > 0 {
		rate := float64(p.Bytes) / elapsed.Seconds()
		fmt.Fprintf(&b, "  %s/s", Size(int64(rate)))
		if left := p.BytesTotal - p.Bytes; left > 0 {
			fmt.Fprintf(&b, "  %s left", clockTime(time.Duration(float64(left)/rate*float64(time.Second))))
		}
	}
	return b.String()
}

// clockTime is d as m:ss, or h:mm:ss from an hour.
func clockTime(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int64(d/time.Hour), int64(d%time.Hour/time.Minute), int64(d%time.Minute/time.Second)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
