// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/altessa-s/go-atlas-core/io/files"

	"github.com/altessa-s/rmctl/internal/port"
)

// dirMode is the mode of the directories this adapter creates.
const dirMode fs.FileMode = 0o750

// ErrIsDirectory means a file was to be written where a directory is.
var ErrIsDirectory = errors.New("is a directory")

// FS is the machine's file system.
type FS struct{}

var _ port.LocalFiles = FS{}

// New returns the file system.
func New() FS { return FS{} }

// Produce writes path through a sibling staging file named
// .<name>.<random>.part. The staging file is installed by a rename only when
// write and the flush succeed and ctx is still live; it is removed on every
// other outcome, so a failed or interrupted run leaves nothing behind.
func (FS) Produce(ctx context.Context, path string, write func(w io.Writer) error) (err error) {
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		return fmt.Errorf("%s: %w", path, ErrIsDirectory)
	}
	dir := filepath.Dir(path)
	// Name the missing directory rather than the staging file inside it.
	switch info, statErr := os.Stat(dir); {
	case errors.Is(statErr, fs.ErrNotExist):
		return fmt.Errorf("create %s: no directory %s", path, dir)
	case statErr == nil && !info.IsDir():
		return fmt.Errorf("create %s: %s is not a directory", path, dir)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.part")
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	staged := tmp.Name()
	installed := false
	defer func() {
		if !installed {
			_ = tmp.Close()
			_ = os.Remove(staged)
		}
	}()
	if err := write(tmp); err != nil {
		return err
	}
	if err := errors.Join(tmp.Sync(), tmp.Close()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(staged, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	installed = true
	return nil
}

// Stat reports the size of path and whether it is a directory.
func (FS) Stat(path string) (size int64, isDir, exists bool, err error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, err
	}
	return info.Size(), info.IsDir(), true, nil
}

// Open opens the regular file at path for reading.
func (FS) Open(path string) (io.ReadCloser, error) {
	f, err := files.OpenRegular(path)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ReadHead reads at most n bytes from the start of the regular file path.
func (FS) ReadHead(path string, n int) ([]byte, error) {
	f, err := files.OpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	got, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:got], nil
}

// ReadTail reads at most n bytes from the end of the regular file path.
func (FS) ReadTail(path string, n int) ([]byte, error) {
	f, err := files.OpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	off := max(size-int64(n), 0)
	buf := make([]byte, size-off)
	got, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:got], nil
}

// MkdirAll creates path and its parents.
func (FS) MkdirAll(path string) error { return os.MkdirAll(path, dirMode) }

// Walk lists the regular files below dir, relative to dir and sorted.
func (FS) Walk(dir string) ([]string, error) {
	var out []string
	for entry, err := range files.Walk(dir, files.WithRecursive(), files.WithFileTypes(files.FileTypeRegular)) {
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(dir, entry.Path)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	slices.Sort(out)
	return out, nil
}

// Remove deletes the file at path.
func (FS) Remove(path string) error { return os.Remove(path) }

// Rename moves the file at from to to, replacing a file there.
func (FS) Rename(from, to string) error { return os.Rename(from, to) }

// Stage makes a directory of mode 0700 in the system's temporary
// directory. remove deletes it with everything in it.
func (FS) Stage() (dir string, remove func() error, err error) {
	dir, err = os.MkdirTemp("", "rmctl-stage-*")
	if err != nil {
		return "", nil, fmt.Errorf("create a staging directory: %w", err)
	}
	return dir, func() error { return os.RemoveAll(dir) }, nil
}
