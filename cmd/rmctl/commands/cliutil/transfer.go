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
	now := streams.Now
	if now == nil {
		now = time.Now //nolint:forbidigo // a progress bar's rate is wall time, not the library's clock
	}
	t := &transfer{w: streams.Err, label: label, now: now, dumb: streams.getenv("TERM") == "dumb"}
	return t.update, t.done
}

type transfer struct {
	mu     sync.Mutex
	w      io.Writer
	label  string
	now    func() time.Time
	dumb   bool
	start  time.Time
	drawn  time.Time
	last   port.SyncProgress
	width  int
	inLine bool
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
	if !finished && t.inLine && now.Sub(t.drawn) < redraw {
		return
	}
	t.drawn = now
	t.draw(TransferLine(t.label, p, now.Sub(t.start)))
}

func (t *transfer) draw(line string) {
	if t.dumb {
		pad := max(t.width-len(line), 0)
		_, _ = fmt.Fprintf(t.w, "\r%s%s", line, strings.Repeat(" ", pad)) //nolint:errcheck // progress
	} else {
		_, _ = fmt.Fprintf(t.w, "\r\x1b[K%s", line) //nolint:errcheck // progress
	}
	t.width = len(line)
	t.inLine = true
}

func (t *transfer) done() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inLine {
		_, _ = fmt.Fprintln(t.w) //nolint:errcheck // progress
		t.inLine = false
	}
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
