// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/indexcache"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestCaches(t *testing.T) {
	t.Parallel()
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	root := blobcache.NewRoot(t.TempDir(), blobcache.Fixed(1<<20), clk, nil)
	for _, name := range []string{"work", "home"} {
		st, err := root.Profile(name)
		if err != nil {
			t.Fatal(err)
		}
		read(t, newCache(newSource(2), st, clk, indexcache.Live, 0, nil))
	}
	c := indexcache.NewCaches(profiles{root}, nil)
	if !slices.Equal(c.Names(), []string{"home", "work"}) {
		t.Fatal(c.Names())
	}
	st, err := c.Status("work")
	if err != nil || st.Profile != "work" || st.Bytes == 0 || st.Budget != 1<<20 || st.RootHash == "" || st.Dir != filepath.Join(root.Dir(), "work") {
		t.Fatalf("%+v %v", st, err)
	}
	if st, err = c.Status("new"); err != nil || st.Bytes != 0 || !st.Confirmed.IsZero() {
		t.Fatalf("%+v %v", st, err)
	}
	removed, err := c.Remove("work")
	if err != nil || !removed.Existed || removed.Kept || removed.Path != filepath.Join(root.Dir(), "work") {
		t.Fatalf("%+v %v", removed, err)
	}
	if !slices.Equal(c.Names(), []string{"home"}) {
		t.Fatal(c.Names())
	}
	if removed, err = c.Remove("work"); err != nil || removed.Existed {
		t.Fatalf("again: %+v %v", removed, err)
	}
	// A file the cache did not write stays, and its directory with it.
	foreign := filepath.Join(root.Dir(), "home", "mine.txt")
	if err := os.WriteFile(foreign, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if removed, err = c.Remove("home"); err != nil || !removed.Existed || !removed.Kept {
		t.Fatalf("%+v %v", removed, err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status("../x"); !errors.Is(err, blobcache.ErrProfileName) {
		t.Fatal(err)
	}
	if _, err := c.Remove("../x"); !errors.Is(err, blobcache.ErrProfileName) {
		t.Fatal(err)
	}
}

func TestCachesRemoveFailure(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permissions do not stop this user")
	}
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	dir := t.TempDir()
	root := blobcache.NewRoot(dir, blobcache.Fixed(1<<20), clk, nil)
	st, err := root.Profile("work")
	if err != nil {
		t.Fatal(err)
	}
	read(t, newCache(newSource(2), st, clk, indexcache.Live, 0, nil))
	// A directory that cannot be searched cannot be looked at, nor emptied.
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := indexcache.NewCaches(profiles{root}, nil).Remove("work"); err == nil {
		t.Fatal("an unreadable cache went unnoticed")
	}
}

func TestNewCachesPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	indexcache.NewCaches(nil, nil)
}
