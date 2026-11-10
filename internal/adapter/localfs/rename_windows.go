//go:build windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"os"

	"golang.org/x/sys/windows"
)

// renameNoReplace renames from to to with MoveFileEx without
// MOVEFILE_REPLACE_EXISTING: a file at to fails it with
// ERROR_ALREADY_EXISTS, which matches fs.ErrExist.
func renameNoReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err == nil {
		var t *uint16
		if t, err = windows.UTF16PtrFromString(to); err == nil {
			err = windows.MoveFileEx(f, t, 0)
		}
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
