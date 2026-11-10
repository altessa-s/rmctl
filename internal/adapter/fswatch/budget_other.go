//go:build !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fswatch

// handleBudget is 0, no limit: inotify and ReadDirectoryChangesW watch a
// directory, not each file in it, and inotify's own limit fails the watch.
func handleBudget() int { return 0 }
