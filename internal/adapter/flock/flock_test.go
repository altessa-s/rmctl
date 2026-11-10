// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package flock_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/flock"
)

// A lock taken through one open file is busy through another until it is
// let go.
func TestTryLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".lock")
	open := func() *os.File {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	first, second := open(), open()
	if busy, err := flock.TryLock(first); busy || err != nil {
		t.Fatal(busy, err)
	}
	if busy, err := flock.TryLock(second); !busy || err != nil {
		t.Fatal(busy, err)
	}
	if err := flock.Unlock(first); err != nil {
		t.Fatal(err)
	}
	if busy, err := flock.TryLock(second); busy || err != nil {
		t.Fatal(busy, err)
	}
	_ = first.Close()
	if _, err := flock.TryLock(first); err == nil {
		t.Fatal("a closed file was locked")
	}
}
