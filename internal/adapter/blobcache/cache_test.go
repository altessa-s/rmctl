// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
	if err := os.WriteFile(filepath.Join(dir, "blobs", h), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(h); ok {
		t.Fatal("served a damaged file")
	}
	if _, err := os.Stat(filepath.Join(dir, "blobs", h)); !os.IsNotExist(err) {
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
		if err := os.Chtimes(filepath.Join(dir, "blobs", h), at, at); err != nil {
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
	if err := os.MkdirAll(filepath.Join(dir, "blobs", hashOf([]byte("a directory"))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blobs", "README"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	data := []byte("0123456789")
	c.Put(hashOf(data), data)
	if _, ok := c.Get(hashOf(data)); !ok {
		t.Fatal("a stranger in the directory evicted a cached file")
	}
	if _, err := os.Stat(filepath.Join(dir, "blobs", "README")); err != nil {
		t.Fatalf("eviction removed a file it does not own: %v", err)
	}
}

func TestSpacesShareTheBudget(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	c := blobcache.New(dir, 30, clock.Real(), nil)
	idx := hashOf([]byte("index"))
	c.PutEntry(blobcache.SpaceIndexes, idx, []byte("0123456789"))
	if err := os.Chtimes(filepath.Join(dir, "indexes", idx), time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	c.PutEntry(blobcache.SpaceState, "root", make([]byte, 100))
	blob := []byte("blob-number-0123456789")
	c.Put(hashOf(blob), blob)
	if _, ok := c.Entry(blobcache.SpaceIndexes, idx); ok {
		t.Fatal("the older index survived a blob over the budget")
	}
	if _, ok := c.Entry(blobcache.SpaceState, "root"); !ok {
		t.Fatal("the state was evicted")
	}
	bytes, entries := c.Usage()
	if bytes != int64(len(blob)) || entries["blobs"] != 1 || entries["indexes"] != 0 || entries["state"] != 1 {
		t.Fatalf("%d %v", bytes, entries)
	}
	for _, bad := range [][2]string{{"blobs", "x"}, {"state", "../x"}, {"state", ".hidden"}, {"other", "root"}} {
		c.PutEntry(bad[0], bad[1], []byte("x"))
		if _, ok := c.Entry(bad[0], bad[1]); ok {
			t.Fatalf("%v was used as a name", bad)
		}
		c.Drop(bad[0], bad[1])
	}
	c.Drop(blobcache.SpaceState, "root")
	if _, ok := c.Entry(blobcache.SpaceState, "root"); ok {
		t.Fatal("dropped and still there")
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the cleared profile's directory is still there: %v", err)
	}
	if c.Dir() != dir || c.Budget() != 30 {
		t.Fatal("dir or budget")
	}
}

func TestRootKeepsProfilesApart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := blobcache.NewRoot(dir, blobcache.Fixed(1<<20), clock.Real(), nil)
	for _, bad := range []string{"", "..", "../x", "a/b", "A", ".x"} {
		if _, err := root.Profile(bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
	work, _ := root.Profile("work")
	home, _ := root.Profile("home")
	data := []byte("work's document")
	work.Put(hashOf(data), data)
	if _, ok := home.Get(hashOf(data)); ok {
		t.Fatal("home read work's blob")
	}
	if err := os.MkdirAll(filepath.Join(dir, "not-a-cache"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := root.Profiles(); len(got) != 1 || got[0] != "work" || root.Dir() != dir {
		t.Fatalf("%v", got)
	}
	if blobcache.NewRoot(filepath.Join(dir, "missing"), blobcache.Fixed(1), clock.Real(), nil).Profiles() != nil {
		t.Fatal("profiles in a missing directory")
	}
}

// The blobs every profile once shared go: at the top of the directory and
// in its blobs directory, and nothing else does.
func TestRemoveLegacy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := hashOf([]byte("old"))
	for _, p := range []string{filepath.Join(dir, h), filepath.Join(dir, "blobs", h)} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	root := blobcache.NewRoot(dir, blobcache.Fixed(1<<20), clock.Real(), nil)
	work, _ := root.Profile("work")
	work.Put(h, []byte("old"))
	root.RemoveLegacy()
	for _, gone := range []string{filepath.Join(dir, h), filepath.Join(dir, "blobs")} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s survived: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	if _, ok := work.Get(h); !ok {
		t.Fatal("a profile's own blob went")
	}
	root.RemoveLegacy()
}

// A budget of 0 is no limit: every entry stays, however many bytes they
// take together, and a file of any size is kept.
func TestUnlimitedBudgetNeverEvicts(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	c := blobcache.New(dir, 0, clock.Real(), nil)
	var hashes []string
	for i := range 20 {
		data := bytes.Repeat([]byte{byte('a' + i)}, 1000)
		c.Put(hashOf(data), data)
		hashes = append(hashes, hashOf(data))
	}
	idx := hashOf([]byte("index"))
	c.PutEntry(blobcache.SpaceIndexes, idx, make([]byte, 5000))
	big := bytes.Repeat([]byte("x"), 1<<16)
	if err := c.Fill(hashOf(big), func(w io.Writer) error { _, err := w.Write(big); return err }); err != nil {
		t.Fatal(err)
	}
	for _, h := range append(hashes, hashOf(big)) {
		if _, ok := c.Get(h); !ok {
			t.Fatalf("%s was evicted under no limit", h)
		}
	}
	used, entries := c.Usage()
	if used != 20*1000+5000+1<<16 || entries["blobs"] != 21 || entries["indexes"] != 1 || c.Budget() != 0 {
		t.Fatalf("%d %v %d", used, entries, c.Budget())
	}
	if blobcache.New(dir, -1, clock.Real(), nil).Budget() != 0 {
		t.Fatal("a negative budget is not no limit")
	}
}

// Each profile of a root is bounded by its own budget, and reports what
// set it.
func TestRootBoundsEachProfileByItsOwnBudget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := blobcache.NewRoot(dir, func(profile string) blobcache.Limit {
		if profile == "small" {
			return blobcache.Limit{Bytes: 10, Source: "profile"}
		}
		return blobcache.Limit{Source: "config"}
	}, clock.Real(), nil)
	small, _ := root.Profile("small")
	big, _ := root.Profile("big")
	if small.Budget() != 10 || small.BudgetSource() != "profile" || big.Budget() != 0 || big.BudgetSource() != "config" {
		t.Fatalf("%d %q %d %q", small.Budget(), small.BudgetSource(), big.Budget(), big.BudgetSource())
	}
	data := []byte("more than ten bytes")
	small.Put(hashOf(data), data)
	big.Put(hashOf(data), data)
	if _, ok := small.Get(hashOf(data)); ok {
		t.Fatal("the small profile kept more than its budget")
	}
	if _, ok := big.Get(hashOf(data)); !ok {
		t.Fatal("the unlimited profile did not keep it")
	}
	if blobcache.New(dir, 1, clock.Real(), nil).BudgetSource() != "" {
		t.Fatal("a cache made by New has a source")
	}
}

func TestNewRootNeedsItsDependencies(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func(){
		"budgets":   func() { blobcache.NewRoot(t.TempDir(), nil, clock.Real(), nil) },
		"directory": func() { blobcache.NewRoot("", blobcache.Fixed(1), clock.Real(), nil) },
		"clock":     func() { blobcache.NewRoot(t.TempDir(), blobcache.Fixed(1), nil, nil) },
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
