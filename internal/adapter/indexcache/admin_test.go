// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/indexcache"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// profiles opens blobcache's profiles for the admin, as wiring does.
type profiles struct{ root *blobcache.Root }

func (p profiles) Dir() string     { return p.root.Dir() }
func (p profiles) Names() []string { return p.root.Profiles() }
func (p profiles) Open(name string) (indexcache.ProfileStore, error) {
	return p.root.Profile(name)
}

func TestAdmin(t *testing.T) {
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
	admin := indexcache.NewAdmin(profiles{root}, indexcache.Settings{Profile: "work", TTL: time.Minute}, nil)
	one, err := admin.Status(context.Background(), false)
	if err != nil || len(one.Profiles) != 1 || one.Profiles[0].Profile != "work" || one.TTL != time.Minute {
		t.Fatalf("%+v %v", one, err)
	}
	p := one.Profiles[0]
	if p.Entries["indexes"] != 3 || p.Bytes == 0 || p.Budget != 1<<20 || p.RootHash == "" || !p.Confirmed.Equal(clk.Now()) {
		t.Fatalf("%+v", p)
	}
	all, _ := admin.Status(context.Background(), true)
	if len(all.Profiles) != 2 || all.Profiles[0].Profile != "home" {
		t.Fatalf("%+v", all)
	}
	// A profile with no cache yet is still reported, empty.
	fresh := indexcache.NewAdmin(profiles{root}, indexcache.Settings{Profile: "new"}, nil)
	if r, _ := fresh.Status(context.Background(), true); len(r.Profiles) != 3 || r.Profiles[2].Profile != "new" || !r.Profiles[2].Confirmed.IsZero() {
		t.Fatalf("%+v", r)
	}
	cleared, err := admin.Clear(context.Background(), false)
	if err != nil || !slices.Equal(cleared, []string{"work"}) || !slices.Equal(root.Profiles(), []string{"home"}) {
		t.Fatalf("%v %v %v", cleared, err, root.Profiles())
	}
	if cleared, err = admin.Clear(context.Background(), true); err != nil || !slices.Equal(cleared, []string{"home", "work"}) || len(root.Profiles()) != 0 {
		t.Fatalf("%v %v %v", cleared, err, root.Profiles())
	}
	bad := indexcache.NewAdmin(profiles{root}, indexcache.Settings{Profile: "../x"}, nil)
	if _, err := bad.Status(context.Background(), false); !errors.Is(err, blobcache.ErrProfileName) {
		t.Fatal(err)
	}
	if _, err := bad.Clear(context.Background(), false); !errors.Is(err, blobcache.ErrProfileName) {
		t.Fatal(err)
	}
}

// registry is a registry that only counts.
type registry struct {
	port.Registry
	fail error
}

func (r registry) Register(context.Context, string) error { return r.fail }
func (r registry) Forget(context.Context) error           { return r.fail }

func TestRegistryClearsTheProfile(t *testing.T) {
	t.Parallel()
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	root := blobcache.NewRoot(t.TempDir(), blobcache.Fixed(1<<20), clk, nil)
	fill := func() *blobcache.Cache {
		st, _ := root.Profile("work")
		read(t, newCache(newSource(1), st, clk, indexcache.Live, 0, nil))
		return st
	}
	other, _ := root.Profile("home")
	read(t, newCache(newSource(1), other, clk, indexcache.Live, 0, nil))
	for name, call := range map[string]func(*indexcache.Registry) error{
		"register": func(r *indexcache.Registry) error { return r.Register(context.Background(), "code") },
		"forget":   func(r *indexcache.Registry) error { return r.Forget(context.Background()) },
	} {
		st := fill()
		if err := call(indexcache.NewRegistry(registry{fail: errors.New("refused")}, st)); err == nil || len(root.Profiles()) != 2 {
			t.Fatalf("%s: a failure cleared the cache: %v", name, root.Profiles())
		}
		if err := call(indexcache.NewRegistry(registry{}, st)); err != nil || !slices.Equal(root.Profiles(), []string{"home"}) {
			t.Fatalf("%s: %v %v", name, err, root.Profiles())
		}
	}
}

func TestBlobs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cache := store(t)
	blobs := indexcache.NewBlobs(porttest.NewCloud(), cache)
	f, err := blobs.Upload(ctx, "a.metadata", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := cache.Get(f.Hash); !ok || string(got) != "{}" {
		t.Fatal("an upload was not kept")
	}
	var off indexcache.OfflineBlobs
	for name, err := range map[string]error{
		"read":     second(off.Read(ctx, domain.File{Name: "a.pdf"})),
		"download": off.Download(ctx, domain.File{Name: "a.pdf"}, nil, nil),
		"upload":   second(off.Upload(ctx, "a.pdf", nil)),
		"file":     second(off.UploadFile(ctx, "/a", "a.pdf", nil)),
		"raw":      second(off.ReadRaw(ctx, "h", "a.pdf")),
		"missing":  second(off.Missing(ctx, nil)),
	} {
		if !errors.Is(err, port.ErrOffline) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := off.Read(ctx, domain.File{Name: "a.pdf"}); err.Error() != "a.pdf is not in the cache: "+port.ErrOffline.Error() {
		t.Fatal(err)
	}
}

func second[T any](_ T, err error) error { return err }

func TestDecoratorsNeedTheirParts(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func(){
		"admin":    func() { indexcache.NewAdmin(nil, indexcache.Settings{}, nil) },
		"registry": func() { indexcache.NewRegistry(nil, nil) },
		"blobs":    func() { indexcache.NewBlobs(nil, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			build()
		}()
	}
}
