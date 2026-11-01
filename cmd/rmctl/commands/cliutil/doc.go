// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package cliutil holds what the rmctl commands share: the services and
// streams they run against, the persistent flags, exit codes, the two-stage
// Ctrl-C, confirmation of writes, progress, the choice of the output format
// and the answers shared by several commands, the masking of names in
// diagnostic output, and the completion of library paths.
//
// Answers go to the output stream and commentary to the error stream, so a
// pipe gets the answer and not the progress.
package cliutil
