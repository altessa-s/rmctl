// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/clock"
)

func removals(t *testing.T, c *blobcache.Cache) string {
	t.Helper()
	g, ok := c.Removals()
	if !ok {
		t.Fatal("the removal generation cannot be told")
	}
	return g
}

// The removal generation changes on every eviction and on every drop that
// removed a blob or an index, and on nothing else: reads, writes, misses
// and the state leave it.
func TestRemovalsChangeOnEveryRemoval(t *testing.T) {
	t.Parallel()
	c := blobcache.New(t.TempDir(), 100, clock.Real(), nil)
	if g := removals(t, c); g != blobcache.NoRemovals {
		t.Fatalf("a fresh cache: %q", g)
	}
	hashes := fill(t, c, 2, 30)
	index := []byte("an index")
	c.PutEntry(blobcache.SpaceIndexes, hashOf(index), index)
	c.PutEntry(blobcache.SpaceState, "root", []byte("pointer"))
	c.Get(hashes[0])
	c.Entry(blobcache.SpaceIndexes, hashOf(index))
	c.Size(blobcache.SpaceBlobs, hashes[1])
	if r, _, ok := c.Open(hashes[1]); ok {
		_, _ = io.Copy(io.Discard, r)
		_ = r.Close()
	}
	c.Usage()
	c.Reconcile()
	c.Drop(blobcache.SpaceBlobs, hashOf([]byte("never kept")))
	c.Drop(blobcache.SpaceState, "root")
	c.Drop(blobcache.SpaceState, blobcache.KeyCovered)
	if g := removals(t, c); g != blobcache.NoRemovals {
		t.Fatalf("no removal changed the generation: %q", g)
	}

	seen := map[string]bool{blobcache.NoRemovals: true}
	bump := func(what string, count string) {
		t.Helper()
		g := removals(t, c)
		if seen[g] || !strings.HasPrefix(g, count+"-") {
			t.Fatalf("%s: generation %q", what, g)
		}
		seen[g] = true
	}
	c.Drop(blobcache.SpaceBlobs, hashes[0])
	bump("a dropped blob", "1")
	c.Drop(blobcache.SpaceIndexes, hashOf(index))
	bump("a dropped index", "2")
	big := bytes.Repeat([]byte("z"), 90)
	c.Put(hashOf(big), big)
	if _, ok := c.Get(hashes[1]); ok {
		t.Fatal("nothing was evicted")
	}
	bump("an eviction", "3")
	// Another process reads what this one wrote.
	if g := removals(t, blobcache.New(c.Dir(), 100, clock.Real(), nil)); !seen[g] || g == blobcache.NoRemovals {
		t.Fatalf("another process: %q", g)
	}
}

// Two processes that read the same generation and both bump it write
// different values: a lost increment never brings back a generation a sync
// recorded.
func TestRemovalsNeverRepeat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, blobcache.SpaceState, blobcache.KeyRemovals)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	var got []string
	for range 2 {
		c := blobcache.New(dir, 0, clock.Real(), nil)
		data := []byte("dropped")
		c.Put(hashOf(data), data)
		if err := os.WriteFile(file, []byte("5-0000000000000000"), 0o600); err != nil {
			t.Fatal(err)
		}
		c.Drop(blobcache.SpaceBlobs, hashOf(data))
		got = append(got, removals(t, c))
	}
	if got[0] == got[1] || !strings.HasPrefix(got[0], "6-") || !strings.HasPrefix(got[1], "6-") {
		t.Fatalf("%q", got)
	}
}

// A damaged generation cannot be told, so no mark rests on it; the next
// removal writes a sound one. One that cannot be read at all is the same.
func TestRemovalsDamaged(t *testing.T) {
	t.Parallel()
	c := blobcache.New(t.TempDir(), 0, clock.Real(), nil)
	file := filepath.Join(c.Dir(), blobcache.SpaceState, blobcache.KeyRemovals)
	c.PutEntry(blobcache.SpaceState, blobcache.KeyRemovals, []byte("not a generation"))
	if g, ok := c.Removals(); ok {
		t.Fatalf("a damaged generation was told: %q", g)
	}
	data := []byte("dropped")
	c.Put(hashOf(data), data)
	c.Drop(blobcache.SpaceBlobs, hashOf(data))
	if g := removals(t, c); !strings.HasPrefix(g, "1-") {
		t.Fatalf("the removal after the damage wrote %q", g)
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0o700); err != nil {
		t.Fatal(err)
	}
	if g, ok := c.Removals(); ok {
		t.Fatalf("an unreadable generation was told: %q", g)
	}
	// A bump that cannot be written still forgets the mark.
	c.PutEntry(blobcache.SpaceState, blobcache.KeyCovered, []byte("mark"))
	c.Put(hashOf(data), data)
	c.Drop(blobcache.SpaceBlobs, hashOf(data))
	if _, ok := c.Entry(blobcache.SpaceState, blobcache.KeyCovered); ok {
		t.Fatal("a removal kept the mark")
	}
	if _, ok := c.Removals(); ok {
		t.Fatal("an unreadable generation was told")
	}
}

// The mark goes before the entry: a drop that cannot remove its entry, as a
// process stopped between the two would leave it, has already taken the
// mark, so nothing vouches for the cache on the old generation.
func TestDropForgetsTheMarkBeforeTheEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := blobcache.New(dir, 1<<20, clock.Real(), nil)
	hashes := fill(t, c, 1, 30)
	c.PutEntry(blobcache.SpaceState, blobcache.KeyCovered, []byte("mark"))
	// A blob that will not go: a directory in its place with a file in it.
	path := filepath.Join(dir, blobcache.SpaceBlobs, hashes[0])
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "held"), 0o700); err != nil {
		t.Fatal(err)
	}
	c.Drop(blobcache.SpaceBlobs, hashes[0])
	if _, ok := c.Entry(blobcache.SpaceState, blobcache.KeyCovered); ok {
		t.Fatal("the mark outlived a drop that did not finish")
	}
	if g := removals(t, c); g != blobcache.NoRemovals {
		t.Fatalf("a removal that did not happen bumped the generation: %q", g)
	}
}
