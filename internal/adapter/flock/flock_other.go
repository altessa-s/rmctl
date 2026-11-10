//go:build !unix && !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package flock

import "os"

// TryLock has no advisory lock to take on this system: every caller gets
// the lock.
func TryLock(*os.File) (bool, error) { return false, nil }

// Unlock has nothing to let go.
func Unlock(*os.File) error { return nil }
