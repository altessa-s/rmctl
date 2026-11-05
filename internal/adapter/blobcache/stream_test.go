// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreclock "github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/clock"
)

var errWrite = errors.New("write failed")

func TestFillOpenSize(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	c := blobcache.New(dir, 1<<20, clock.Real(), nil)
	data := []byte(strings.Repeat("streamed ", 1000))
	h := hashOf(data)
	if _, ok := c.Size(blobcache.SpaceBlobs, h); ok {
		t.Fatal("a size on an empty cache")
	}
	if err := c.Fill(h, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n, ok := c.Size(blobcache.SpaceBlobs, h); !ok || n != int64(len(data)) {
		t.Fatalf("%d %v", n, ok)
	}
	r, size, ok := c.Open(h)
	if !ok || size != int64(len(data)) {
		t.Fatal("not opened")
	}
	got, err := io.ReadAll(r)
	if err != nil || string(got) != string(data) {
		t.Fatalf("%v", err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes, entries := c.Usage(); bytes != int64(len(data)) || entries[blobcache.SpaceBlobs] != 1 {
		t.Fatalf("usage %d %v", bytes, entries)
	}
	// No staging file is left behind.
	files, _ := os.ReadDir(filepath.Join(dir, blobcache.SpaceBlobs))
	if len(files) != 1 {
		t.Fatalf("%d files", len(files))
	}
}

func TestFillRefuses(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	c := blobcache.New(dir, 8, clock.Real(), nil)
	data := []byte("right")
	write := func(b []byte) func(io.Writer) error {
		return func(w io.Writer) error { _, err := w.Write(b); return err }
	}
	if err := c.Fill("../x", write(data)); err == nil {
		t.Fatal("a name that is not a hash was filled")
	}
	if err := c.Fill(hashOf(data), write([]byte("wrong"))); !errors.Is(err, blobcache.ErrMismatch) {
		t.Fatal(err)
	}
	if err := c.Fill(hashOf(data), func(io.Writer) error { return errWrite }); !errors.Is(err, errWrite) {
		t.Fatal(err)
	}
	big := []byte("larger than eight")
	if err := c.Fill(hashOf(big), write(big)); !errors.Is(err, blobcache.ErrTooLarge) {
		t.Fatal(err)
	}
	if bytes, entries := c.Usage(); bytes != 0 || entries[blobcache.SpaceBlobs] != 0 {
		t.Fatalf("a refused fill left %d bytes", bytes)
	}
	files, _ := os.ReadDir(filepath.Join(dir, blobcache.SpaceBlobs))
	if len(files) != 0 {
		t.Fatalf("staging files left: %v", files)
	}
}

func TestOpenChecksTheHash(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	c := blobcache.New(dir, 1<<20, clock.Real(), nil)
	data := []byte("good")
	h := hashOf(data)
	c.Put(h, data)
	if err := os.WriteFile(filepath.Join(dir, blobcache.SpaceBlobs, h), []byte("evil"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, _, ok := c.Open(h)
	if !ok {
		t.Fatal("not opened")
	}
	if _, err := io.ReadAll(r); !errors.Is(err, blobcache.ErrMismatch) {
		t.Fatal(err)
	}
	_ = r.Close()
	if _, _, ok := c.Open(h); ok {
		t.Fatal("a damaged blob was kept")
	}
	if _, _, ok := c.Open("nothash"); ok {
		t.Fatal("opened a name that is not a hash")
	}
	if _, ok := c.Size(blobcache.SpaceBlobs, "../x"); ok {
		t.Fatal("a size for a name that is not a hash")
	}
}

func TestFillEvictsTheOldestFirst(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	clk := coreclock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c := blobcache.New(dir, 10, clk, nil)
	a, b, d := []byte("aaaa"), []byte("bbbb"), []byte("dddd")
	fill := func(data []byte) {
		t.Helper()
		clk.Advance(time.Minute)
		if err := c.Fill(hashOf(data), func(w io.Writer) error { _, err := w.Write(data); return err }); err != nil {
			t.Fatal(err)
		}
	}
	fill(a)
	fill(b)
	// a is used again: b is now the oldest.
	clk.Advance(time.Minute)
	if _, ok := c.Size(blobcache.SpaceBlobs, hashOf(a)); !ok {
		t.Fatal("a is gone")
	}
	fill(d)
	if _, ok := c.Size(blobcache.SpaceBlobs, hashOf(b)); ok {
		t.Fatal("the least recently used blob was kept")
	}
	for _, kept := range [][]byte{a, d} {
		if _, ok := c.Size(blobcache.SpaceBlobs, hashOf(kept)); !ok {
			t.Fatalf("%s was evicted", kept)
		}
	}
}

// Open misses, rather than fails, when there is no blob to read: no blobs
// directory yet, or something other than a file under the hash's name.
func TestOpenMisses(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	c := blobcache.New(dir, 1<<20, clock.Real(), nil)
	h := hashOf([]byte("absent"))
	if _, _, ok := c.Open(h); ok {
		t.Fatal("a blob in a cache with no blobs directory")
	}
	if err := os.MkdirAll(filepath.Join(dir, blobcache.SpaceBlobs, h), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Open(h); ok {
		t.Fatal("a directory read as a blob")
	}
	if _, _, ok := c.Open("not-a-hash"); ok {
		t.Fatal("a name that is not a hash was opened")
	}
}

// A fill reports what keeps it from the disk, as the command asked for it.
func TestFillReportsADirectoryItCannotMake(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The blobs directory is taken by a file.
	if err := os.WriteFile(filepath.Join(dir, blobcache.SpaceBlobs), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := blobcache.New(dir, 1<<20, clock.Real(), nil)
	data := []byte("x")
	if err := c.Fill(hashOf(data), func(w io.Writer) error { _, err := w.Write(data); return err }); err == nil {
		t.Fatal("a fill with nowhere to go succeeded")
	}
}
