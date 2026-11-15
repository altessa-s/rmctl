// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/altessa-s/rmctl/internal/port"
)

// percent is a whole.
const percent = 100

// Progress is a percent line on the error stream for a person at a
// terminal, rewritten in place; it is nil, and prints nothing, anywhere
// else. done finishes the line.
func Progress(streams Streams, label string) (progress port.Progress, done func()) {
	if !streams.IsTerminal {
		return nil, func() {}
	}
	var mu sync.Mutex
	w := streams.Err
	line := newStatusLine(streams)
	last := -1
	printed := false
	progress = func(n, total int64) {
		mu.Lock()
		defer mu.Unlock()
		pct := -1
		if total > 0 {
			pct = int(min(n*percent/total, percent))
		}
		if pct == last && pct >= 0 {
			return
		}
		last = pct
		printed = true
		if streams.eraseProgress {
			text := fmt.Sprintf("%s %3d%%", label, pct)
			if pct < 0 {
				text = label + " " + Size(n)
			}
			line.draw(text)
		} else {
			writeProgress(w, label, n, pct)
		}
	}
	return progress, func() {
		mu.Lock()
		defer mu.Unlock()
		if streams.eraseProgress {
			line.erase()
			return
		}
		if printed {
			_, _ = fmt.Fprintln(w) //nolint:errcheck // progress
		}
	}
}

func writeProgress(w io.Writer, label string, n int64, pct int) {
	if pct < 0 {
		_, _ = fmt.Fprintf(w, "\r%s %s", label, Size(n)) //nolint:errcheck // progress
		return
	}
	_, _ = fmt.Fprintf(w, "\r%s %3d%%", label, pct) //nolint:errcheck // progress
}

// CountProgress keeps page and document counts separate from transfer byte rates.
func CountProgress(streams Streams, label string) (port.Progress, func()) {
	if !streams.IsTerminal {
		return nil, func() {}
	}
	var mu sync.Mutex
	line := newStatusLine(streams)
	update := func(n, total int64) {
		mu.Lock()
		defer mu.Unlock()
		pct := int64(0)
		if total > 0 {
			pct = min(max(n, 0)*percent/total, percent)
		}
		filled := int(pct) * barWidth / percent
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		line.draw(fmt.Sprintf("%s [%s] %d/%d", label, bar, n, total))
	}
	return update, func() {
		mu.Lock()
		defer mu.Unlock()
		if streams.eraseProgress {
			line.erase()
		} else {
			line.finish()
		}
	}
}

// RenderProgress keeps the latest page and download counts on one line, even
// when folder workers draw pages while others are still downloading. A phase
// appears after its first callback; local rendering shows only page counts.
func RenderProgress(streams Streams) (pages, download port.Progress, done func()) {
	if !streams.IsTerminal {
		return nil, nil, func() {}
	}
	var mu sync.Mutex
	line := newStatusLine(streams)
	var pageText, byteText string
	update := func(text *string, format func(int64, int64) string) port.Progress {
		return func(n, total int64) {
			mu.Lock()
			defer mu.Unlock()
			*text = format(n, total)
			parts := []string{}
			if pageText != "" {
				parts = append(parts, pageText)
			}
			if byteText != "" {
				parts = append(parts, byteText)
			}
			line.draw(strings.Join(parts, " · "))
		}
	}
	pages = update(&pageText, func(n, total int64) string { return fmt.Sprintf("drawing pages %d/%d", n, total) })
	download = update(&byteText, func(n, total int64) string { return fmt.Sprintf("downloading %s/%s", Size(n), Size(total)) })
	return pages, download, func() {
		mu.Lock()
		defer mu.Unlock()
		if streams.eraseProgress {
			line.erase()
		} else {
			line.finish()
		}
	}
}
