// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !windows

package main

import "os"

// enableColor reports whether the terminal f shows ANSI color: every
// terminal outside Windows does.
func enableColor(*os.File) bool { return true }
