// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"fmt"
	"io"

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
	w := streams.Err
	last := -1
	printed := false
	progress = func(n, total int64) {
		pct := -1
		if total > 0 {
			pct = int(min(n*percent/total, percent))
		}
		if pct == last && pct >= 0 {
			return
		}
		last = pct
		printed = true
		writeProgress(w, label, n, pct)
	}
	return progress, func() {
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
