//go:build darwin

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
// replacing a file at to.
func renameNoReplace(from, to string) error {
	err := unix.RenamexNp(from, to, unix.RENAME_EXCL)
	if errors.Is(err, unix.EEXIST) {
		return fs.ErrExist
	}
	return pathErr(from, err)
}
