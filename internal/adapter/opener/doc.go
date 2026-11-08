// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package opener implements port.Opener: a file shown in the viewer the
// system picks for it, without waiting for the viewer. On macOS it runs
// open(1); on Windows it asks the shell through ShellExecute, which takes
// any file name as it is, with no command line to quote; everywhere else it
// runs xdg-open(1) in a session of its own, so the viewer outlives rmctl
// and never reads its terminal.
package opener
