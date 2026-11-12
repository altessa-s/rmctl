//go:build linux

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// renameNoReplace renames from to to with renameat2 and RENAME_NOREPLACE: a
// file at to fails it with EEXIST. A file system without the flag (EINVAL)
// or a kernel without the call falls back to a look first.
func renameNoReplace(from, to string) error {
	err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EINVAL), errors.Is(err, unix.ENOSYS), errors.Is(err, unix.ENOTSUP):
		return renameChecked(from, to)
	default:
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
}
