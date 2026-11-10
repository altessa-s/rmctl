// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/altessa-s/rmctl/internal/adapter/flock"
	"github.com/altessa-s/rmctl/internal/port"
)

// lockMode keeps a lock file to its owner.
const lockMode fs.FileMode = 0o600

var _ port.MirrorFiles = FS{}

// Scan implements port.MirrorFiles: the regular files below dir, by their
// paths relative to it with slashes. Symbolic links and other special
// files are left out and never followed. A file that vanishes during the
// walk is left out; a directory that cannot be read fails the scan.
func (FS) Scan(dir string, prune func(rel string) bool) ([]port.LocalEntry, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s: not a directory", dir)
	}
	var out []port.LocalEntry
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Gone since its directory was read.
			return nil
		case err != nil:
			return err
		case p == dir:
			return nil
		}
		rel, _ := filepath.Rel(dir, p) //nolint:errcheck // p is below dir: the walk found it there
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if prune != nil && prune(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		// A file gone since its directory was read is left out.
		if fi, ierr := d.Info(); ierr == nil {
			out = append(out, port.LocalEntry{Path: rel, Size: fi.Size(), ModTime: fi.ModTime()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b port.LocalEntry) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

// Lock implements port.MirrorFiles with the system's advisory lock on the
// file at path, created when missing and kept after: it is released when
// the holder exits, however it exits.
func (FS) Lock(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, lockMode) // #nosec G304 -- the lock file of a directory the user mirrors
	if err != nil {
		return nil, err
	}
	busy, err := flock.TryLock(f)
	if err != nil || busy {
		_ = f.Close()
		if busy {
			return nil, port.ErrMirrorBusy
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() error { return errors.Join(flock.Unlock(f), f.Close()) }, nil
}
