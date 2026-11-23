// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"io"
	"strings"
	"sync"
)

// ProgressOutput serializes stderr writes with progress drawings. Commentary
// (including transport logs) erases the active status line before it writes.
// Wrapping the same output again returns it unchanged.
func ProgressOutput(w io.Writer) io.Writer {
	if _, ok := w.(*progressOutput); ok {
		return w
	}
	return &progressOutput{w: w}
}

type progressOutput struct {
	mu           sync.Mutex
	w            io.Writer
	active, dumb bool
	width        int
}

func (p *progressOutput) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active {
		eraseText := "\r\x1b[K"
		if p.dumb {
			eraseText = "\r" + strings.Repeat(" ", p.width) + "\r"
		}
		if _, err := io.WriteString(p.w, eraseText); err != nil {
			return 0, err
		}
		p.active = false
	}
	return p.w.Write(b)
}

func (p *progressOutput) status(text string, width int, active, dumb bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = io.WriteString(p.w, text) //nolint:errcheck // progress is best effort
	p.width, p.active, p.dumb = width, active, dumb
}
