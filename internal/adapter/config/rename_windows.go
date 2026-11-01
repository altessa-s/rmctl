//go:build windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"errors"
	"io/fs"

	"golang.org/x/sys/windows"
)

// renameNoReplace renames the file at from to the path to, failing with fs.ErrExist rather than
// replacing a file at to: MoveFileEx without MOVEFILE_REPLACE_EXISTING.
func renameNoReplace(from, to string) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(src, dst, 0)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return fs.ErrExist
	}
	return pathErr(from, err)
}
