//go:build !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The rename of a file system without a rename that refuses looks first:
// a file there is fs.ErrExist, and stays.
func TestRenameChecked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte(p), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := renameChecked(a, b); !errors.Is(err, fs.ErrExist) {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(b); string(got) != b {
		t.Fatalf("replaced: %q", got)
	}
	c := filepath.Join(dir, "c")
	if err := renameChecked(a, c); err != nil {
		t.Fatal(err)
	}
	if err := renameChecked(c, filepath.Join(b, "d")); err == nil {
		t.Fatal("renamed below a file")
	}
}
