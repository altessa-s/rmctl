// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/localfs"
	"github.com/altessa-s/rmctl/internal/port"
)

// Scan lists the regular files below a directory with their sizes and
// times, skips what prune names, and follows no link.
func TestScan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mtime := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for rel, body := range map[string]string{"b.pdf": "bb", "Sub/a.pdf": "a", "skip/c.pdf": "c"} {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(dir, "b.pdf"), filepath.Join(dir, "link.pdf")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := localfs.New().Scan(dir, func(rel string) bool { return rel == "skip" })
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0] != (port.LocalEntry{Path: "Sub/a.pdf", Size: 1, ModTime: got[0].ModTime}) || !got[0].ModTime.Equal(mtime) || got[1].Path != "b.pdf" || got[1].Size != 2 {
		t.Fatalf("%+v", got)
	}
	if got, err = localfs.New().Scan(dir, nil); err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = localfs.New().Scan(filepath.Join(dir, "missing"), nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err = localfs.New().Scan(filepath.Join(dir, "b.pdf"), nil); err == nil {
		t.Fatal("a file scanned as a directory")
	}
}

// A directory below that cannot be read fails the scan: what it holds is
// not known, and a mirror must not take it for deleted.
func TestScanUnreadable(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permissions do not stop this user")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if _, err := localfs.New().Scan(dir, nil); !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
}

// A lock is the process's until released; a second taker is told it is
// busy.
func TestLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".lock")
	fs := localfs.New()
	release, err := fs.Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fs.Lock(path); !errors.Is(err, port.ErrMirrorBusy) {
		t.Fatal(err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	again, err := fs.Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = again()
	if _, err = fs.Lock(filepath.Join(path, "below", "a file")); err == nil {
		t.Fatal("a lock in no directory")
	}
}
