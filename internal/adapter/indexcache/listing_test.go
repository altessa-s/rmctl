// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/indexcache"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// listed is a small library: a folder, a document in it, and one in the
// trash.
func listed() *domain.Tree {
	return domain.NewTree(7, []domain.Item{
		domain.NewListedItem("f", "Books", domain.KindFolder, "", false),
		domain.NewListedItem("d", "Novel", domain.KindEPUB, "f", false),
		domain.NewListedItem("t", "Old", domain.KindPDF, domain.TrashID, true),
	})
}

// counting is a store that counts the entries written.
type counting struct {
	*blobcache.Cache
	puts int
}

func (c *counting) PutEntry(space, key string, data []byte) {
	c.puts++
	c.Cache.PutEntry(space, key, data)
}

func TestListingRoundTrip(t *testing.T) {
	t.Parallel()
	store := &counting{Cache: newStore(t)}
	l := indexcache.NewListings(store, nil)
	l.PutListing("h1", listed())
	l.PutListing("h1", listed())
	if store.puts != 1 {
		t.Fatalf("the same root was listed %d times", store.puts)
	}
	// Another process finds the listing of the root it loads already there.
	indexcache.NewListings(store, nil).PutListing("h1", listed())
	if store.puts != 1 {
		t.Fatalf("a listing on disk was written again: %d", store.puts)
	}
	l.PutListing("h2", domain.NewTree(8, nil))
	l.PutListing("h2", nil)
	if store.puts != 2 {
		t.Fatalf("%d writes", store.puts)
	}
	l.PutListing("h1", listed())
	tree, err := indexcache.NewCompletion(store.Cache).Listing(t.Context())
	if err != nil || tree.Generation != 7 || tree.Len() != 3 {
		t.Fatalf("%v %+v", err, tree)
	}
	d, _ := tree.Get("d")
	if d.Name != "Novel" || d.Kind != domain.KindEPUB || d.Parent != "f" || tree.Path("d") != "/Books/Novel" {
		t.Fatalf("%+v", d)
	}
	if !tree.IsTrashed("t") || tree.IsTrashed("d") {
		t.Fatal("the trash is lost")
	}
}

func TestCompletionWritesNothing(t *testing.T) {
	t.Parallel()
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	dir := filepath.Join(t.TempDir(), "default")
	indexcache.NewListings(blobcache.New(dir, 0, clk, nil), nil).PutListing("h", listed())
	path := filepath.Join(dir, "state", "listing")
	old := time.Unix(1_600_000_000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Hour)
	if _, err := indexcache.NewCompletion(blobcache.New(dir, 0, clk, nil)).Listing(t.Context()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("the read touched the listing: %v", err)
	}
}

func TestCompletionWithoutAListing(t *testing.T) {
	t.Parallel()
	if _, err := indexcache.NewCompletion(nil).Listing(t.Context()); !errors.Is(err, port.ErrNoListing) {
		t.Fatal(err)
	}
	store := newStore(t)
	if _, err := indexcache.NewCompletion(store).Listing(t.Context()); !errors.Is(err, port.ErrNoListing) {
		t.Fatal(err)
	}
	for _, data := range []string{"{", `{"v":2,"hash":"h","items":[]}`} {
		store.PutEntry("state", "listing", []byte(data))
		if _, err := indexcache.NewCompletion(store).Listing(t.Context()); !errors.Is(err, port.ErrNoListing) {
			t.Fatalf("%s: %v", data, err)
		}
	}
}

func TestNewListingsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("no store did not panic")
		}
	}()
	indexcache.NewListings(nil, nil)
}

func BenchmarkCompletionListing(b *testing.B) {
	for _, n := range []int{500, 5000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			store := newStore(b)
			indexcache.NewListings(store, nil).PutListing("h", wideTree(n))
			c := indexcache.NewCompletion(store)
			b.ResetTimer()
			for b.Loop() {
				if _, err := c.Listing(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// wideTree is n items: folders of 50 documents each.
func wideTree(n int) *domain.Tree {
	items := make([]domain.Item, 0, n)
	for i := range n {
		if i%50 == 0 {
			items = append(items, domain.NewListedItem(domain.ItemID(fmt.Sprintf("f%05d", i)), fmt.Sprintf("Folder %d", i/50), domain.KindFolder, "", false))
			continue
		}
		parent := domain.ItemID(fmt.Sprintf("f%05d", i-i%50))
		items = append(items, domain.NewListedItem(domain.ItemID(fmt.Sprintf("d%05d-0000-4000-8000-000000000000", i)),
			fmt.Sprintf("A document with a long name %d", i), domain.KindPDF, parent, i%97 == 0))
	}
	return domain.NewTree(1, items)
}

// newStore is a profile's cache without a budget in a temporary directory.
func newStore(t testing.TB) *blobcache.Cache {
	t.Helper()
	return blobcache.New(filepath.Join(t.TempDir(), "default"), 0, porttest.NewClock(time.Unix(1_700_000_000, 0)), nil)
}
