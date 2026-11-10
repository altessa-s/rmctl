//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands"
)

// atATerminal makes the run one a person watches, with a timer that fires
// as it is set: a load shows its line from the first record on.
func atATerminal(s *commands.Streams) {
	s.IsTerminal = true
	s.Getenv = func(string) string { return "" }
	s.After = func(_ time.Duration, f func()) func() {
		f()
		return func() {}
	}
}

// A person at a terminal sees the line of a load that reads the cloud, and
// it is erased before the answer: standard output is the same with it and
// without, and standard error keeps no line. A run that is not at a
// terminal, an offline one and shell completion print none.
func TestE2E_LoadingLine(t *testing.T) {
	e := newE2E(t)
	e.register()
	seedThree(t, e.cloud, "a")

	code, piped, errOut := e.rmctl("ls", "--no-cache")
	if code != 0 || errOut != "" {
		t.Fatalf("ls in a pipe: exit %d, stderr %q", code, errOut)
	}

	e.streams = atATerminal
	const erase = "\r\x1b[K"
	for _, args := range [][]string{{"ls", "--no-cache"}, {"ls"}} {
		code, out, errOut := e.rmctl(args...)
		if code != 0 || out != piped {
			t.Fatalf("%v: exit %d\n%q\nwant %q", args, code, out, piped)
		}
		if !strings.HasPrefix(errOut, erase+"reading the library  0/3 items") || !strings.HasSuffix(errOut, erase) ||
			strings.Contains(errOut, "\n") {
			t.Fatalf("%v: stderr %q", args, errOut)
		}
	}
	// --json changes the answer, not the line, and the line stays off
	// standard output.
	code, out, errOut := e.rmctl("ls", "--json", "--no-cache")
	if code != 0 || !strings.HasPrefix(out, "[") || strings.Contains(out, "reading") || !strings.Contains(errOut, "reading the library") {
		t.Fatalf("ls --json: exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	for _, args := range [][]string{{"ls", "--offline"}, {"__complete", "ls", "/"}} {
		code, out, errOut := e.rmctl(args...)
		if code != 0 || out == "" || strings.Contains(errOut, "reading") || strings.Contains(errOut, "\r") {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
}
