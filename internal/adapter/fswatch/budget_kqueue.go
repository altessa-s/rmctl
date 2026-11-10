//go:build darwin || dragonfly || freebsd || netbsd || openbsd

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fswatch

import "syscall"

// handleBudget is how many directories and files kqueue may watch: half
// the descriptors the process may open, the other half left for its files
// and connections. A launchd agent starts with few.
func handleBudget() int {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil || lim.Cur == 0 {
		return minBudget
	}
	return int(max(lim.Cur/shareOfLimit, minBudget)) // #nosec G115 -- half a descriptor limit fits an int
}

// shareOfLimit divides the descriptor limit for the watcher's part, and
// minBudget is the budget when the limit cannot be read, or is tiny.
const (
	shareOfLimit = 2
	minBudget    = 128
)
