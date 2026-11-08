// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/clock"
	"github.com/altessa-s/rmctl/internal/adapter/config"
	"github.com/altessa-s/rmctl/internal/adapter/indexcache"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
	"github.com/altessa-s/rmctl/internal/wiring"
)

// list keeps a listing of n items in the cache of profile under opts, as
// a command that read the library would.
func list(t testing.TB, opts wiring.Options, profile string, n int) {
	t.Helper()
	store, err := blobcache.NewRoot(filepath.Join(opts.Dirs.Cache, "rmctl"), blobcache.Fixed(0), clock.Real(), nil).Profile(profile)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]domain.Item, 0, n)
	for i := range n {
		if i%50 == 0 {
			items = append(items, domain.NewListedItem(domain.ItemID(fmt.Sprintf("f%05d", i)), fmt.Sprintf("Folder %d", i/50), domain.KindFolder, "", false))
			continue
		}
		items = append(items, domain.NewListedItem(domain.ItemID(fmt.Sprintf("d%05d-0000-4000-8000-000000000000", i)),
			fmt.Sprintf("A document with a long name %d", i), domain.KindPDF, domain.ItemID(fmt.Sprintf("f%05d", i-i%50)), false))
	}
	indexcache.NewListings(store, nil).PutListing("root", domain.NewTree(1, items))
}

func TestCompletion(t *testing.T) {
	t.Parallel()
	opts := options(t)
	opts.Profile = "work"
	c, err := wiring.Completion(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Listing(t.Context()); !errors.Is(err, port.ErrNoListing) {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(opts.Dirs.Cache, "rmctl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completion made the cache directory")
	}
	list(t, opts, "work", 120)
	tree, err := c.Listing(t.Context())
	if err != nil || tree.Len() != 120 {
		t.Fatalf("%v", err)
	}
	// Another profile, and --no-cache, list nothing.
	opts.Profile = "home"
	if c, err = wiring.Completion(opts); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Listing(t.Context()); !errors.Is(err, port.ErrNoListing) {
		t.Fatal(err)
	}
	opts.Profile, opts.NoCache = "work", true
	if c, err = wiring.Completion(opts); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Listing(t.Context()); !errors.Is(err, port.ErrNoListing) {
		t.Fatal(err)
	}
	// RMCTL_CACHE_DIR names the cache.
	moved := options(t)
	moved.Env = map[string]string{config.EnvCacheDir: filepath.Join(opts.Dirs.Cache, "rmctl")}
	if c, err = wiring.Completion(moved); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Listing(t.Context()); !errors.Is(err, port.ErrNoListing) {
		t.Fatal("the default profile has no listing:", err)
	}
	moved.Profile = "work"
	if c, err = wiring.Completion(moved); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Listing(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionRefuses(t *testing.T) {
	t.Parallel()
	opts := options(t)
	opts.Profile = "../x"
	if _, err := wiring.Completion(opts); err == nil {
		t.Fatal("a bad profile name was taken")
	}
	opts = options(t)
	opts.ReadConfig = func(string) ([]byte, bool, error) { return nil, false, errors.New("unreadable") }
	if _, err := wiring.Completion(opts); err == nil {
		t.Fatal("an unreadable configuration was taken")
	}
}

// A command that loads the library leaves the listing completion reads,
// and open draws into the profile's cache with the opener it was given.
func TestBuildListsAndOpens(t *testing.T) {
	t.Parallel()
	opts := options(t)
	opened := &porttest.Opener{}
	opts.Opener = opened
	svc, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	if svc.Viewing == nil {
		t.Fatal("no open")
	}
	_, err = svc.Viewing.OpenFile(t.Context(), filepath.Join(t.TempDir(), "missing.rmdoc"), port.RenderOptions{})
	if err == nil || len(opened.Opened()) != 0 {
		t.Fatalf("%v %v", err, opened.Opened())
	}
}

func BenchmarkCompletion(b *testing.B) {
	for _, n := range []int{500, 5000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			opts := options(b)
			list(b, opts, "default", n)
			b.ResetTimer()
			for b.Loop() {
				c, err := wiring.Completion(opts)
				if err != nil {
					b.Fatal(err)
				}
				if _, err = c.Listing(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
