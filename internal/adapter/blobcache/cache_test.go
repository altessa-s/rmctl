// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/clock"
)

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestPutGet(t *testing.T) {
	t.Parallel()
	c := blobcache.New(filepath.Join(t.TempDir(), "blobs"), 1<<20, clock.Real(), nil)
	data := []byte("hello")
	h := hashOf(data)
	if _, ok := c.Get(h); ok {
		t.Fatal("hit on an empty cache")
	}
	c.Put(h, data)
	got, ok := c.Get(h)
	if !ok || string(got) != "hello" {
		t.Fatalf("%q %v", got, ok)
	}
}

func TestRefusesBadInput(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "blobs")
	c := blobcache.New(dir, 1<<20, clock.Real(), nil)
	data := []byte("hello")
	for _, h := range []string{"", "../etc/passwd", hashOf(data)[:63], "ABCDEF" + hashOf(data)[6:], hashOf(data)[:63] + "g"} {
		c.Put(h, data)
		if _, ok := c.Get(h); ok {
			t.Fatalf("%q was used as a name", h)
		}
	}
	c.Put(hashOf([]byte("other")), data)
	if _, ok := c.Get(hashOf([]byte("other"))); ok {
		t.Fatal("stored bytes under a hash they do not have")
	}
	big := blobcache.New(filepath.Join(t.TempDir(), "small"), 3, clock.Real(), nil)
	big.Put(hashOf(data), data)
	if _, ok := big.Get(hashOf(data)); ok {
		t.Fatal("stored a file larger than the cache")
	}
}

func TestDropsADamagedFile(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "blobs")
	c := blobcache.New(dir, 1<<20, clock.Real(), nil)
	data := []byte("hello")
	h := hashOf(data)
	c.Put(h, data)
	if err := os.WriteFile(filepath.Join(dir, h), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(h); ok {
		t.Fatal("served a damaged file")
	}
	if _, err := os.Stat(filepath.Join(dir, h)); !os.IsNotExist(err) {
		t.Fatalf("the damaged file is still there: %v", err)
	}
}

func TestEvictsTheLeastRecentlyUsed(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "blobs")
	c := blobcache.New(dir, 25, clock.Real(), nil)
	var hashes []string
	for i := range 3 {
		data := fmt.Appendf(nil, "blob-number-%d", i)
		h := hashOf(data)
		hashes = append(hashes, h)
		c.Put(h, data)
		// Distinct access times, oldest first.
		at := time.Now().Add(time.Duration(i-10) * time.Minute)
		if err := os.Chtimes(filepath.Join(dir, h), at, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := c.Get(hashes[0]); ok {
		t.Fatal("the oldest file survived")
	}
	if _, ok := c.Get(hashes[2]); !ok {
		t.Fatal("the newest file was evicted")
	}
}

func TestNeverFails(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := blobcache.New(filepath.Join(blocker, "blobs"), 1<<20, clock.Real(), nil)
	data := []byte("hello")
	c.Put(hashOf(data), data)
	if _, ok := c.Get(hashOf(data)); ok {
		t.Fatal("hit in a cache that cannot exist")
	}
}

func TestNewNeedsItsDependencies(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func(){
		"directory": func() { blobcache.New("", 1, clock.Real(), nil) },
		"clock":     func() { blobcache.New(t.TempDir(), 1, nil, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("a missing %s did not panic", name)
				}
			}()
			build()
		}()
	}
}

func BenchmarkGet(b *testing.B) {
	c := blobcache.New(filepath.Join(b.TempDir(), "blobs"), 1<<30, clock.Real(), nil)
	data := make([]byte, 64<<10)
	h := hashOf(data)
	c.Put(h, data)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := c.Get(h); !ok {
			b.Fatal("miss")
		}
	}
}

func BenchmarkPut(b *testing.B) {
	c := blobcache.New(filepath.Join(b.TempDir(), "blobs"), 1<<30, clock.Real(), nil)
	data := make([]byte, 64<<10)
	h := hashOf(data)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		c.Put(h, data)
	}
}

func TestEvictionIgnoresStrangers(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "blobs")
	c := blobcache.New(dir, 10, clock.Real(), nil)
	if err := os.MkdirAll(filepath.Join(dir, hashOf([]byte("a directory"))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	data := []byte("0123456789")
	c.Put(hashOf(data), data)
	if _, ok := c.Get(hashOf(data)); !ok {
		t.Fatal("a stranger in the directory evicted a cached file")
	}
	if _, err := os.Stat(filepath.Join(dir, "README")); err != nil {
		t.Fatalf("eviction removed a file it does not own: %v", err)
	}
}
