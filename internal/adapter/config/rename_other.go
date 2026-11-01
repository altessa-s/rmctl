//go:build !darwin && !linux && !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

// renameNoReplace checks that the path to is free, then renames: no atomic
// no-replace rename here. The advisory lock makes it safe against other
// rmctl processes, the only ones naming set-aside copies.
func renameNoReplace(from, to string) error { return checkThenRename(from, to) }
