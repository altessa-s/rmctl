// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/clock"
)

// drawing writes n bytes of b, counting its calls.
func drawing(calls *int, b byte, n int) func(io.Writer) error {
	return func(w io.Writer) error {
		*calls++
		_, err := w.Write(bytes.Repeat([]byte{b}, n))
		return err
	}
}

func TestPreviewDrawsOnceAndKeeps(t *testing.T) {
	t.Parallel()
	c := blobcache.New(t.TempDir(), 1<<20, clock.Real(), nil)
	key := hashOf([]byte("doc v1, pdf"))
	calls := 0
	path, drawn, err := c.Preview(t.Context(), key, "Paper-rendered.pdf", drawing(&calls, 'p', 10))
	if err != nil || !drawn || calls != 1 {
		t.Fatalf("%v %v %d", err, drawn, calls)
	}
	if want := filepath.Join(c.Dir(), blobcache.SpaceRendered, key, "Paper-rendered.pdf"); path != want {
		t.Fatalf("%s, want %s", path, want)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != strings.Repeat("p", 10) {
		t.Fatalf("%q %v", data, err)
	}
	again, drawn, err := c.Preview(t.Context(), key, "Paper-rendered.pdf", drawing(&calls, 'q', 10))
	if err != nil || drawn || again != path || calls != 1 {
		t.Fatalf("a kept drawing was drawn again: %v %v %d", err, drawn, calls)
	}
	// The same drawing under another name replaces the one kept.
	renamed, drawn, err := c.Preview(t.Context(), key, "Renamed-rendered.pdf", drawing(&calls, 'r', 10))
	if err != nil || !drawn || calls != 2 || filepath.Base(renamed) != "Renamed-rendered.pdf" {
		t.Fatalf("%v %v %d %s", err, drawn, calls, renamed)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the drawing under the old name stayed")
	}
	bytes, entries := c.Usage()
	if bytes != 10 || entries[blobcache.SpaceRendered] != 1 {
		t.Fatalf("%d bytes, %v", bytes, entries)
	}
}

func TestPreviewRefusesAndFails(t *testing.T) {
	t.Parallel()
	c := blobcache.New(t.TempDir(), 1<<20, clock.Real(), nil)
	key := hashOf([]byte("k"))
	calls := 0
	for _, name := range []string{"", ".hidden.pdf", "../up.pdf", "a/b.pdf", `a\b.pdf`} {
		if _, _, err := c.Preview(t.Context(), key, name, drawing(&calls, 'x', 1)); err == nil {
			t.Fatalf("%q was taken as a name", name)
		}
	}
	if _, _, err := c.Preview(t.Context(), "../etc", "a.pdf", drawing(&calls, 'x', 1)); err == nil {
		t.Fatal("a key that is no hash was taken")
	}
	boom := errors.New("boom")
	if _, _, err := c.Preview(t.Context(), key, "a.pdf", func(io.Writer) error { return boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := c.Preview(ctx, key, "a.pdf", drawing(&calls, 'x', 1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("drew %d times", calls)
	}
	if entries, _ := os.ReadDir(filepath.Join(c.Dir(), blobcache.SpaceRendered)); len(entries) != 0 {
		t.Fatalf("a failed drawing left %d entries", len(entries))
	}
}

func TestPreviewWhereNothingCanBeWritten(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if _, _, err := blobcache.New(file, 0, clock.Real(), nil).Preview(t.Context(), hashOf([]byte("k")), "a.pdf", drawing(&calls, 'x', 1)); err == nil {
		t.Fatal("a cache under a file drew")
	}
}

// A drawing counts against the budget: the least recently used entries
// go, never the drawing just made, even when it alone is over the budget,
// and a drawing evicted never takes the record of the last sync.
func TestPreviewEviction(t *testing.T) {
	t.Parallel()
	c := blobcache.New(t.TempDir(), 100, clock.Real(), nil)
	hashes := fill(t, c, 2, 30)
	c.PutEntry(blobcache.SpaceState, blobcache.KeyCovered, []byte("mark"))
	calls := 0
	first, _, err := c.Preview(t.Context(), hashOf([]byte("one")), "one.pdf", drawing(&calls, '1', 30))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err = os.Chtimes(filepath.Dir(first), old, old); err != nil {
		t.Fatal(err)
	}
	// 90 bytes kept; 30 more pass the budget: the oldest, the drawing, goes,
	// and the mark stays.
	second, _, err := c.Preview(t.Context(), hashOf([]byte("two")), "two.pdf", drawing(&calls, '2', 30))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the oldest drawing stayed")
	}
	if _, ok := c.Get(hashes[0]); !ok {
		t.Fatal("a newer blob went before an older drawing")
	}
	if _, ok := c.Entry(blobcache.SpaceState, blobcache.KeyCovered); !ok {
		t.Fatal("evicting a drawing took the record of the last sync")
	}
	// A drawing larger than the whole budget stays; everything else goes.
	huge, _, err := c.Preview(t.Context(), hashOf([]byte("huge")), "huge.pdf", drawing(&calls, 'h', 500))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(huge); err != nil {
		t.Fatal("the drawing just made was evicted")
	}
	if _, err = os.Stat(second); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an older drawing stayed")
	}
	if _, ok := c.Entry(blobcache.SpaceState, blobcache.KeyCovered); ok {
		t.Fatal("evicting the blobs kept the record of the last sync")
	}
}

func TestDrawingsDropAndClear(t *testing.T) {
	t.Parallel()
	c := blobcache.New(t.TempDir(), 0, clock.Real(), nil)
	key := hashOf([]byte("k"))
	calls := 0
	path, _, err := c.Preview(t.Context(), key, "a.pdf", drawing(&calls, 'a', 3))
	if err != nil {
		t.Fatal(err)
	}
	c.Drop(blobcache.SpaceRendered, key)
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a dropped drawing stayed")
	}
	c.Drop(blobcache.SpaceRendered, key)
	if _, _, err = c.Preview(t.Context(), key, "a.pdf", drawing(&calls, 'a', 3)); err != nil {
		t.Fatal(err)
	}
	// A stranger in the drawings is neither counted nor removed.
	stranger := filepath.Join(c.Dir(), blobcache.SpaceRendered, "notes.txt")
	if err = os.WriteFile(stranger, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if bytes, entries := c.Usage(); bytes != 3 || entries[blobcache.SpaceRendered] != 1 {
		t.Fatalf("%d %v", bytes, entries)
	}
	if err = c.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(stranger); err != nil {
		t.Fatal("clear removed a file the cache did not write")
	}
	if _, entries := c.Usage(); entries[blobcache.SpaceRendered] != 0 {
		t.Fatal("clear kept a drawing")
	}
}

func TestPeekChangesNothing(t *testing.T) {
	t.Parallel()
	c := blobcache.New(t.TempDir(), 0, clock.Real(), nil)
	c.PutEntry(blobcache.SpaceState, "listing", []byte("data"))
	path := filepath.Join(c.Dir(), blobcache.SpaceState, "listing")
	old := time.Unix(1_600_000_000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if data, ok := c.Peek(blobcache.SpaceState, "listing"); !ok || string(data) != "data" {
		t.Fatalf("%q %v", data, ok)
	}
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(old) {
		t.Fatal("a peek counted as a use")
	}
	for _, key := range []string{"missing", "../x", "Listing"} {
		if _, ok := c.Peek(blobcache.SpaceState, key); ok {
			t.Fatalf("%q was read", key)
		}
	}
	if _, ok := c.Peek(blobcache.SpaceBlobs, hashOf([]byte("x"))); ok {
		t.Fatal("a peek of a space never made hit")
	}
}

func TestRootPeekCreatesAndEvictsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := blobcache.NewRoot(dir, blobcache.Fixed(10), clock.Real(), nil)
	full, err := blobcache.NewRoot(dir, blobcache.Fixed(0), clock.Real(), nil).Profile("work")
	if err != nil {
		t.Fatal(err)
	}
	full.PutEntry(blobcache.SpaceIndexes, hashOf([]byte("big")), bytes.Repeat([]byte("b"), 100))
	c, err := root.Peek("work")
	if err != nil {
		t.Fatal(err)
	}
	if bytes, _ := c.Usage(); bytes != 100 {
		t.Fatalf("a peek evicted down to the budget: %d", bytes)
	}
	if _, err = root.Peek("home"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "home")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a peek made a directory")
	}
	if _, err = root.Peek("../x"); !errors.Is(err, blobcache.ErrProfileName) {
		t.Fatal(err)
	}
}
