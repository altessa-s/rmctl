// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package version implements the "rmctl version" command, which prints
// build and version metadata sourced from [appinfo].
//
// Two output formats are supported:
//
//   - text (default) — human-readable single line or multi-line with --full.
//   - json — machine-readable object, useful for scripts.
package version
