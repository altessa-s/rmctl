//go:build darwin

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// renameNoReplace renames from to to with renamex_np and RENAME_EXCL: a
// file at to fails it with EEXIST. A file system without the flag (an SMB
// share, FAT) falls back to a look first.
func renameNoReplace(from, to string) error {
	err := unix.RenamexNp(from, to, unix.RENAME_EXCL)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EINVAL):
		return renameChecked(from, to)
	default:
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
}
