// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"io"
	"log/slog"

	"github.com/altessa-s/go-slogx/console"
)

// newLogger is a console logger on w: Warn and above, or Debug and above
// when verbose. It writes to the error stream, so logging never mixes into
// an answer on standard output.
func newLogger(w io.Writer, verbose bool) (*slog.Logger, error) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	h, err := console.New(w, console.WithLevel(level))
	if err != nil {
		return nil, err
	}
	return slog.New(h), nil
}
