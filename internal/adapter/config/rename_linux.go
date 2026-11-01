//go:build linux

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"errors"
	"io/fs"

	"golang.org/x/sys/unix"
)

// renameNoReplace renames the file at from to the path to, failing with fs.ErrExist rather than
// replacing a file at to. A kernel or file system without
// RENAME_NOREPLACE falls back to checking first, which the advisory lock
// makes safe against other rmctl processes, the only ones naming set-aside
// copies.
func renameNoReplace(from, to string) error {
	err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
	switch {
	case errors.Is(err, unix.EEXIST):
		return fs.ErrExist
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL):
		return checkThenRename(from, to)
	}
	return pathErr(from, err)
}
