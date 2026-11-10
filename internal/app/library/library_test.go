// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package library_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

func sample(c *porttest.Cloud) {
	c.Add(
		porttest.Doc{ID: "f1", Name: "Books", Folder: true},
		porttest.Doc{ID: "d1", Name: "Paper", Parent: "f1", Ext: "pdf", Tags: []string{"work", "work", ""}},
		porttest.Doc{ID: "d2", Name: "Marked", Ext: "pdf", RM: true},
		porttest.Doc{ID: "d3", Name: "Novel", Ext: "epub"},
		porttest.Doc{ID: "d4", Name: "Notes"},
		porttest.Doc{ID: "t1", Name: "Template", Template: true},
		porttest.Doc{ID: "x1", Name: "No meta", NoMetadata: true},
		porttest.Doc{ID: "x2", Name: "Bad meta", BadMetadata: true},
	)
}

func newLib(c *porttest.Cloud, opts ...library.Option) *library.Library {
	return library.New(c, c, c, opts...)
}

func TestLoadBuildsItems(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	sample(c)
	tree, state, err := newLib(c, library.WithLogger(slog.New(slog.DiscardHandler)), library.WithConcurrency(0), nil).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.Generation != c.Generation() || tree.Len() != 5 {
		t.Fatalf("generation %d, %d items", state.Generation, tree.Len())
	}
	want := map[domain.ItemID]domain.Kind{
		"f1": domain.KindFolder, "d1": domain.KindPDF, "d2": domain.KindAnnotatedPDF,
		"d3": domain.KindEPUB, "d4": domain.KindNotebook,
	}
	for id, kind := range want {
		it, ok := tree.Get(id)
		if !ok || it.Kind != kind {
			t.Errorf("%s: %+v", id, it)
		}
	}
	d1, _ := tree.Get("d1")
	if !slices.Equal(d1.Tags, []string{"work"}) || d1.Parent != "f1" {
		t.Errorf("d1 %+v", d1)
	}
}

func TestLoadReusesUnchangedItems(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	sample(c)
	cache := porttest.NewMemCache()
	lib := newLib(c, library.WithCache(cache))
	if _, _, err := lib.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	files := c.Count("Files")
	if _, _, err := lib.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.Count("Files") != files {
		t.Fatalf("a second load read %d indexes again", c.Count("Files")-files)
	}
	// A fresh library over the same cache reads indexes but not files.
	reads := c.Count("Read")
	if _, _, err := newLib(c, library.WithCache(cache)).Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.Count("Read") != reads || cache.Hits == 0 {
		t.Fatalf("reads %d → %d, hits %d", reads, c.Count("Read"), cache.Hits)
	}
}

func TestLoadFailures(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"Root", "Files", "Read"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			c := porttest.NewCloud()
			sample(c)
			c.Fail[method] = errBoom
			if _, _, err := newLib(c).Load(t.Context()); !errors.Is(err, errBoom) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("content unreadable", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		recs := c.Add(porttest.Doc{ID: "d1", Name: "A", Ext: "pdf"})
		files, _ := c.Files(t.Context(), recs[0])
		for _, f := range files {
			if f.Extension() == "content" {
				c.DropBlob(f.Hash)
			}
		}
		if _, _, err := newLib(c).Load(t.Context()); !errors.Is(err, porttest.ErrNoBlob) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestEmptyLibrary(t *testing.T) {
	t.Parallel()
	tree, state, err := newLib(porttest.NewCloud()).Load(t.Context())
	if err != nil || tree.Len() != 0 || state.Generation != 0 {
		t.Fatalf("%v %d %d", err, tree.Len(), state.Generation)
	}
}

func TestMetadata(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	sample(c)
	lib := newLib(c)
	tree, _, err := lib.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	it, _ := tree.Get("d1")
	meta, f, err := lib.Metadata(t.Context(), it)
	if err != nil || meta.VisibleName() != "Paper" || f.Name != "d1.metadata" {
		t.Fatalf("%v %q %q", err, meta.VisibleName(), f.Name)
	}
	if _, _, err := lib.Metadata(t.Context(), domain.Item{ID: "z"}); err == nil {
		t.Fatal("an item without metadata decoded")
	}
	c.Fail["Read"] = errBoom
	if _, _, err := newLib(c).Metadata(t.Context(), it); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func appendFolder(c *porttest.Cloud) library.Change {
	return func(_ context.Context, _ *domain.Tree, state port.RootState) ([]domain.Record, error) {
		return append(slices.Clone(state.Records), c.Record(porttest.Doc{ID: "new", Name: "New", Folder: true})), nil
	}
}

func TestCommit(t *testing.T) {
	t.Parallel()
	t.Run("writes", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		before := c.Generation()
		state, err := newLib(c).Commit(t.Context(), appendFolder(c))
		if err != nil || state.Generation != before+1 || len(c.Records()) != 9 {
			t.Fatalf("%v %+v", err, state)
		}
	})
	t.Run("unchanged writes nothing", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		_, err := newLib(c).Commit(t.Context(), func(_ context.Context, _ *domain.Tree, s port.RootState) ([]domain.Record, error) {
			return s.Records, nil
		})
		if err != nil || c.Count("CommitRoot") != 0 {
			t.Fatalf("%v, %d commits", err, c.Count("CommitRoot"))
		}
	})
	t.Run("replays a conflict", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		c.Conflicts = 2
		runs := 0
		change := appendFolder(c)
		root, err := newLib(c).Commit(t.Context(), func(ctx context.Context, tree *domain.Tree, s port.RootState) ([]domain.Record, error) {
			runs++
			return change(ctx, tree, s)
		})
		if err != nil || runs != 3 || c.Count("CommitRoot") != 3 {
			t.Fatalf("%v, %d runs", err, runs)
		}
		if now, _ := c.Root(t.Context()); root.Hash == "" || root.Hash != now.Hash || root.Generation != now.Generation ||
			len(root.Records) != len(now.Records) {
			t.Fatalf("committed %+v, the root is %+v", root, now)
		}
	})
	t.Run("gives up", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		c.Conflicts = 10
		_, err := newLib(c, library.WithAttempts(2)).Commit(t.Context(), appendFolder(c))
		if !errors.Is(err, port.ErrRootConflict) || c.Count("CommitRoot") != 2 {
			t.Fatalf("%v, %d commits", err, c.Count("CommitRoot"))
		}
	})
	t.Run("change fails", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		_, err := newLib(c).Commit(t.Context(), func(context.Context, *domain.Tree, port.RootState) ([]domain.Record, error) {
			return nil, errBoom
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("commit fails", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		c.Fail["CommitRoot"] = errBoom
		if _, err := newLib(c).Commit(t.Context(), appendFolder(c)); !errors.Is(err, errBoom) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("root fails", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		c.Fail["Root"] = errBoom
		if _, err := newLib(c).Commit(t.Context(), appendFolder(c)); !errors.Is(err, errBoom) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("canceled before", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		c := porttest.NewCloud()
		if _, err := newLib(c).Commit(ctx, appendFolder(c)); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("canceled during the change", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		c := porttest.NewCloud()
		change := appendFolder(c)
		_, err := newLib(c).Commit(ctx, func(ctx context.Context, tree *domain.Tree, s port.RootState) ([]domain.Record, error) {
			cancel()
			return change(ctx, tree, s)
		})
		if !errors.Is(err, context.Canceled) || c.Count("CommitRoot") != 0 {
			t.Fatalf("%v, %d commits", err, c.Count("CommitRoot"))
		}
	})
}

func TestReplace(t *testing.T) {
	t.Parallel()
	recs := []domain.Record{{ID: "a", Hash: "1"}, {ID: "b", Hash: "2"}}
	out, ok := library.Replace(recs, domain.Record{ID: "b", Hash: "3"})
	if !ok || out[1].Hash != "3" || recs[1].Hash != "2" {
		t.Fatalf("%v %v", out, ok)
	}
	if _, ok := library.Replace(recs, domain.Record{ID: "z"}); ok {
		t.Fatal("replaced a missing record")
	}
	files := []domain.File{{Name: "x.metadata", Hash: "1"}}
	if got := library.ReplaceFile(files, domain.File{Name: "x.metadata", Hash: "2"}); len(got) != 1 || got[0].Hash != "2" {
		t.Fatalf("%v", got)
	}
	if got := library.ReplaceFile(files, domain.File{Name: "x.content"}); len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	for i, args := range [][3]any{{nil, c, c}, {c, nil, c}, {c, c, nil}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("case %d did not panic", i)
				}
			}()
			r, _ := args[0].(port.RootStore)
			d, _ := args[1].(port.DocumentStore)
			b, _ := args[2].(port.BlobStore)
			library.New(r, d, b)
		}()
	}
}

// listings records the listings kept.
type listings struct {
	hashes []string
	trees  []*domain.Tree
}

func (l *listings) PutListing(hash string, tree *domain.Tree) {
	l.hashes = append(l.hashes, hash)
	l.trees = append(l.trees, tree)
}

func TestListings(t *testing.T) {
	t.Parallel()
	t.Run("a load keeps its tree", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		kept := &listings{}
		tree, state, err := newLib(c, library.WithListings(kept)).Load(t.Context())
		if err != nil || len(kept.hashes) != 1 || kept.hashes[0] != state.Hash || kept.trees[0] != tree {
			t.Fatalf("%v %+v", err, kept)
		}
	})
	t.Run("a commit keeps the tree it wrote", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		kept := &listings{}
		root, err := newLib(c, library.WithListings(kept)).Commit(t.Context(), appendFolder(c))
		if err != nil || len(kept.hashes) != 2 || kept.hashes[1] != root.Hash {
			t.Fatalf("%v %+v", err, kept.hashes)
		}
		if _, ok := kept.trees[1].Get("new"); !ok {
			t.Fatal("the committed listing lacks the new folder")
		}
	})
	t.Run("a committed tree that does not build keeps nothing", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		kept := &listings{}
		change := func(_ context.Context, _ *domain.Tree, state port.RootState) ([]domain.Record, error) {
			return append(slices.Clone(state.Records), domain.Record{ID: "ghost", Hash: "missing"}), nil
		}
		if _, err := newLib(c, library.WithListings(kept)).Commit(t.Context(), change); err != nil || len(kept.hashes) != 1 {
			t.Fatalf("%v %+v", err, kept.hashes)
		}
	})
	t.Run("a load that fails keeps nothing", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		c.Fail = map[string]error{"Files": errBoom}
		kept := &listings{}
		if _, _, err := newLib(c, library.WithListings(kept)).Load(t.Context()); err == nil || len(kept.hashes) != 0 {
			t.Fatalf("%v %+v", err, kept.hashes)
		}
	})
}

// loads records what a library tells of its loads, and whether two calls
// ever ran at once.
type loads struct {
	mu      sync.Mutex
	inside  atomic.Int32
	overlap atomic.Bool
	calls   [][2]int
	ended   int
}

func (l *loads) LoadProgress(done, total int) {
	if l.inside.Add(1) != 1 {
		l.overlap.Store(true)
	}
	defer l.inside.Add(-1)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, [2]int{done, total})
}

func (l *loads) LoadDone() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ended++
}

// take returns the calls since the last take and how many loads ended.
func (l *loads) take() ([][2]int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	calls, ended := l.calls, l.ended
	l.calls, l.ended = nil, 0
	return calls, ended
}

// counted says calls are one load of total records from 0 up to last, one
// record at a time.
func counted(calls [][2]int, total, last int) bool {
	if len(calls) != last+1 {
		return false
	}
	for i, c := range calls {
		if c != [2]int{i, total} {
			return false
		}
	}
	return true
}

func TestLoadObserver(t *testing.T) {
	t.Parallel()
	t.Run("a load that reads counts every record", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		seen := &loads{}
		lib := newLib(c, library.WithLoadObserver(seen))
		if _, _, err := lib.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		// Templates and records that are no item are records all the same.
		if calls, ended := seen.take(); !counted(calls, 8, 8) || ended != 1 {
			t.Fatalf("%v, %d ended", calls, ended)
		}
		// Every item is known now: the next load says nothing.
		if _, _, err := lib.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		if calls, ended := seen.take(); len(calls) != 0 || ended != 0 {
			t.Fatalf("a load of known items told %v, %d ended", calls, ended)
		}
		// One new record makes it a load again, known items counted too.
		c.Add(porttest.Doc{ID: "d9", Name: "Late", Ext: "pdf"})
		if _, _, err := lib.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		if calls, ended := seen.take(); !counted(calls, 9, 9) || ended != 1 {
			t.Fatalf("%v, %d ended", calls, ended)
		}
	})
	t.Run("an empty library and no observer say nothing", func(t *testing.T) {
		t.Parallel()
		seen := &loads{}
		if _, _, err := newLib(porttest.NewCloud(), library.WithLoadObserver(seen)).Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		if calls, ended := seen.take(); len(calls) != 0 || ended != 0 {
			t.Fatalf("%v, %d ended", calls, ended)
		}
		c := porttest.NewCloud()
		sample(c)
		if _, _, err := newLib(c, library.WithLoadObserver(nil)).Load(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a load that fails ends short of the total", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		c.Fail["Files"] = errBoom
		seen := &loads{}
		if _, _, err := newLib(c, library.WithLoadObserver(seen)).Load(t.Context()); !errors.Is(err, errBoom) {
			t.Fatalf("got %v", err)
		}
		if calls, ended := seen.take(); !counted(calls, 8, 0) || ended != 1 {
			t.Fatalf("%v, %d ended", calls, ended)
		}
	})
	t.Run("a root that fails is no load", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		c.Fail["Root"] = errBoom
		seen := &loads{}
		if _, _, err := newLib(c, library.WithLoadObserver(seen)).Load(t.Context()); !errors.Is(err, errBoom) {
			t.Fatalf("got %v", err)
		}
		if calls, ended := seen.take(); len(calls) != 0 || ended != 0 {
			t.Fatalf("%v, %d ended", calls, ended)
		}
	})
	t.Run("the calls of many loaders come one at a time and in order", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		const n = 200
		for i := range n {
			c.Add(porttest.Doc{ID: domain.ItemID(fmt.Sprintf("d%03d", i)), Name: fmt.Sprintf("Doc %d", i), Ext: "pdf"})
		}
		seen := &loads{}
		if _, _, err := newLib(c, library.WithLoadObserver(seen), library.WithConcurrency(16)).Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		if calls, ended := seen.take(); !counted(calls, n, n) || ended != 1 || seen.overlap.Load() {
			t.Fatalf("%d calls, %d ended, overlap %v", len(calls), ended, seen.overlap.Load())
		}
	})
	t.Run("a commit after a load tells only of what it wrote", func(t *testing.T) {
		t.Parallel()
		c := porttest.NewCloud()
		sample(c)
		seen := &loads{}
		lib := newLib(c, library.WithLoadObserver(seen), library.WithListings(&listings{}))
		if _, _, err := lib.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		seen.take()
		// The load of the commit knows every item; the listing of the root
		// it wrote reads the one new record.
		if _, err := lib.Commit(t.Context(), appendFolder(c)); err != nil {
			t.Fatal(err)
		}
		if calls, ended := seen.take(); !counted(calls, 9, 9) || ended != 1 {
			t.Fatalf("%v, %d ended", calls, ended)
		}
		// Without listings a commit over known items says nothing.
		quiet := &loads{}
		plain := newLib(c, library.WithLoadObserver(quiet))
		if _, _, err := plain.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		quiet.take()
		_, err := plain.Commit(t.Context(), func(_ context.Context, _ *domain.Tree, s port.RootState) ([]domain.Record, error) {
			return s.Records[1:], nil
		})
		if calls, ended := quiet.take(); err != nil || len(calls) != 0 || ended != 0 {
			t.Fatalf("%v: %v, %d ended", err, calls, ended)
		}
	})
}
