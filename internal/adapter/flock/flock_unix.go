//go:build unix

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package flock

import (
	"os"

	"golang.org/x/sys/unix"
)

// TryLock takes an exclusive advisory lock on f without waiting; busy is
// true when another process holds it.
func TryLock(f *os.File) (busy bool, err error) {
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // a descriptor fits an int
	if err == unix.EWOULDBLOCK {
		return true, nil
	}
	return false, err
}

// Unlock lets the lock on f go.
func Unlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:gosec // a descriptor fits an int
}
