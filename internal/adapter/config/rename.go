// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// checkThenRename renames the file at from to the path to, unless something
// is at to already.
func checkThenRename(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
}

// pathErr names the source path in a rename's error, and keeps a missing source
// recognizable as fs.ErrNotExist.
func pathErr(from string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ENOENT) {
		return &fs.PathError{Op: "rename", Path: from, Err: fs.ErrNotExist}
	}
	return &fs.PathError{Op: "rename", Path: from, Err: err}
}
