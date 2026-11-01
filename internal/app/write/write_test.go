// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

// hookRoots runs before on the second Root call: between a use case's first
// look at the library and its commit.
type hookRoots struct {
	*porttest.Cloud
	mu     sync.Mutex
	n      int
	before func()
}

func (h *hookRoots) Root(ctx context.Context) (port.RootState, error) {
	h.mu.Lock()
	h.n++
	fire := h.n == 2 && h.before != nil
	h.mu.Unlock()
	if fire {
		h.before()
	}
	return h.Cloud.Root(ctx)
}

type fixture struct {
	s     *write.Service
	c     *porttest.Cloud
	files *porttest.MemFiles
	pdf   *porttest.PDF
	roots *hookRoots
}

func setup(t testing.TB) fixture {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "f1aaaaaa", Name: "Books", Folder: true},
		porttest.Doc{ID: "f2aaaaaa", Name: "Fiction", Folder: true, Parent: "f1aaaaaa"},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Ext: "pdf"},
		porttest.Doc{ID: "d2aaaaaa", Name: "Novel", Parent: "f2aaaaaa", Ext: "epub"},
		porttest.Doc{ID: "d3aaaaaa", Name: "Old", Ext: "pdf", Trashed: true},
		porttest.Doc{ID: "f3aaaaaa", Name: "Gone", Folder: true, Trashed: true},
		porttest.Doc{ID: "d4aaaaaa", Name: "Inside", Parent: "f3aaaaaa", Ext: "pdf"},
	)
	files := porttest.NewMemFiles()
	c.ReadLocal = func(p string) ([]byte, error) {
		b, ok := files.File(p)
		if !ok {
			return nil, porttest.ErrNoFile
		}
		return b, nil
	}
	pdf := &porttest.PDF{Counts: map[string]int{}}
	roots := &hookRoots{Cloud: c}
	lib := library.New(roots, c, c)
	s := write.New(lib, c, c, files, pdfInspector{pdf}, porttest.NewArchiver(files), porttest.NewIDs("new"), porttest.NewClock(porttest.Epoch))
	return fixture{s: s, c: c, files: files, pdf: pdf, roots: roots}
}

// pdfInspector reads the fixture's table at call time.
type pdfInspector struct{ p *porttest.PDF }

func (i pdfInspector) Pages(path string) (int, error) { return i.p.Pages(path) }

func tree(t *testing.T, f fixture) *domain.Tree {
	t.Helper()
	tr, _, err := library.New(f.c, f.c, f.c).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func metadata(t *testing.T, f fixture, id domain.ItemID) map[string]any {
	t.Helper()
	it, ok := tree(t, f).Get(id)
	if !ok {
		t.Fatalf("%s is gone", id)
	}
	mf, _ := it.File("metadata")
	data, _ := f.c.Blob(mf.Hash)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMkdir(t *testing.T) {
	t.Parallel()
	f := setup(t)
	it, err := f.s.Mkdir(t.Context(), "/Books/New", false)
	if err != nil || it.Name != "New" || it.Parent != "f1aaaaaa" || it.Kind != domain.KindFolder {
		t.Fatalf("%v %+v", err, it)
	}
	if got := tree(t, f).Path(it.ID); got != "/Books/New" {
		t.Fatalf("path %q", got)
	}
	commits := f.c.Count("CommitRoot")
	deep, err := f.s.Mkdir(t.Context(), "A/B/C", true)
	if err != nil || f.c.Count("CommitRoot") != commits+1 {
		t.Fatalf("%v, %d commits", err, f.c.Count("CommitRoot")-commits)
	}
	if got := tree(t, f).Path(deep.ID); got != "/A/B/C" {
		t.Fatalf("path %q", got)
	}
	again, err := f.s.Mkdir(t.Context(), "/A/B", true)
	if err != nil || again.Name != "B" {
		t.Fatalf("%v %+v", err, again)
	}
	for _, tc := range []struct {
		path    string
		parents bool
		want    error
	}{
		{"/Books", false, write.ErrExists},
		{"/X/Y", false, domain.ErrNotFound},
		{"/Books/Paper/Z", true, domain.ErrNotAFolder},
		{"//", false, domain.ErrEmptyName},
	} {
		if _, err := f.s.Mkdir(t.Context(), tc.path, tc.parents); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", tc.path, err)
		}
	}
}

func TestMkdirAmbiguousAndFailures(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.c.Add(porttest.Doc{ID: "f9aaaaaa", Name: "Books", Folder: true})
	var amb *domain.AmbiguousError
	if _, err := f.s.Mkdir(t.Context(), "/Books/X", false); !errors.As(err, &amb) {
		t.Fatalf("got %v", err)
	}
	for _, method := range []string{"Root", "Upload", "PutIndex", "CommitRoot"} {
		f := setup(t)
		f.c.Fail[method] = errBoom
		if _, err := f.s.Mkdir(t.Context(), "/New", false); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
	}
}

func TestMkdirDestinationGone(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.roots.before = func() {
		recs := f.c.Records()
		f.c.SetRoot(recs[1:])
	}
	if _, err := f.s.Mkdir(t.Context(), "/Books/New", false); !errors.Is(err, domain.ErrDestinationGone) {
		t.Fatalf("got %v", err)
	}
}

func TestMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, dst string
		id, parent     domain.ItemID
		newName        string
		bump           float64
	}{
		{name: "into folder", src: "d1", id: "d1aaaaaa", dst: "/Books/Fiction", parent: "f2aaaaaa", newName: "Paper", bump: 1},
		{name: "rename", src: "d1", id: "d1aaaaaa", dst: "/Books/Essay", parent: "f1aaaaaa", newName: "Essay", bump: 1},
		{name: "move and rename", src: "d2", id: "d2aaaaaa", dst: "/Books/Story", parent: "f1aaaaaa", newName: "Story", bump: 2},
		{name: "to the top", src: "/Books/Paper", id: "d1aaaaaa", dst: "/", newName: "Paper", bump: 1},
		{name: "folder", src: "f2", id: "f2aaaaaa", dst: "", newName: "Fiction", bump: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			before := metadata(t, f, tc.id)
			it, err := f.s.Move(t.Context(), tc.src, tc.dst)
			if err != nil || it.ID != tc.id || it.Parent != tc.parent || it.Name != tc.newName {
				t.Fatalf("%v %+v", err, it)
			}
			after := metadata(t, f, tc.id)
			if after["version"].(float64)-before["version"].(float64) != tc.bump || after["metadatamodified"] != true {
				t.Fatalf("version %v → %v", before["version"], after["version"])
			}
		})
	}
}

func TestMoveRefusals(t *testing.T) {
	t.Parallel()
	f := setup(t)
	for _, tc := range []struct {
		src, dst string
		want     error
	}{
		{"f1", "/Books/Fiction", domain.ErrIntoOwnSubtree},
		{"f1", "/Books", domain.ErrIntoOwnSubtree},
		{"d3", "/Books", domain.ErrInTrash},
		{"d4", "/Books", domain.ErrInTrash},
		{"d1", "/Books/Fiction/Novel", write.ErrExists},
		{"d1", "/Books/Paper/x", domain.ErrNotAFolder},
		{"d1", "/Nowhere/x", domain.ErrNotFound},
		{"/Nope", "/", domain.ErrNotFound},
	} {
		if _, err := f.s.Move(t.Context(), tc.src, tc.dst); !errors.Is(err, tc.want) {
			t.Errorf("%s → %s: %v", tc.src, tc.dst, err)
		}
	}
	if _, err := f.s.Move(t.Context(), "d1", "/Books"); err != nil {
		t.Fatal(err)
	}
	if f.c.Count("CommitRoot") != 0 {
		t.Fatal("a move to where the item is wrote a root")
	}
}

func TestMoveDuringCommit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.roots.before = func() {
		recs := f.c.Records()
		f.c.SetRoot(recs[:2])
	}
	if _, err := f.s.Move(t.Context(), "d1", "/"); !errors.Is(err, domain.ErrVanished) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	f.roots.before = func() {
		recs := f.c.Records()
		rec := f.c.Record(porttest.Doc{ID: "f2aaaaaa", Name: "Fiction", Folder: true, Parent: "f1aaaaaa", Trashed: true})
		recs[1] = rec
		f.c.SetRoot(recs)
	}
	if _, err := f.s.Move(t.Context(), "d1", "/Books/Fiction"); !errors.Is(err, domain.ErrDestinationGone) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	f.roots.before = func() {
		recs := f.c.Records()
		recs[2] = f.c.Record(porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Ext: "pdf", Trashed: true})
		f.c.SetRoot(recs)
	}
	if _, err := f.s.Move(t.Context(), "d1", "/"); !errors.Is(err, domain.ErrInTrash) {
		t.Fatalf("got %v", err)
	}
	for _, method := range []string{"Upload", "PutIndex", "Read"} {
		f := setup(t)
		f.roots.before = func() { f.c.Fail[method] = errBoom }
		if _, err := f.s.Move(t.Context(), "d1", "/"); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	f = setup(t)
	f.c.Fail["Root"] = errBoom
	if _, err := f.s.Move(t.Context(), "d1", "/"); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	f.c.Add(porttest.Doc{ID: "f8aaaaaa", Name: "Books", Folder: true})
	var amb *domain.AmbiguousError
	if _, err := f.s.Move(t.Context(), "d1", "/Books"); !errors.As(err, &amb) {
		t.Fatalf("got %v", err)
	}
}

func TestTrashAndRestore(t *testing.T) {
	t.Parallel()
	f := setup(t)
	items, err := f.s.Trash(t.Context(), []string{"d1", "d3"})
	if err != nil || len(items) != 2 {
		t.Fatalf("%v %v", err, items)
	}
	tr := tree(t, f)
	if !tr.IsTrashed("d1aaaaaa") || metadata(t, f, "d1aaaaaa")["parent"] != "trash" {
		t.Fatal("d1 is not in the trash")
	}
	if _, err := f.s.Trash(t.Context(), []string{"/Nope"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	commits := f.c.Count("CommitRoot")
	if _, err := f.s.Trash(t.Context(), []string{"d3"}); err != nil || f.c.Count("CommitRoot") != commits {
		t.Fatalf("%v; trashing a trashed item wrote", err)
	}

	items, err = f.s.Restore(t.Context(), []string{"d1", "d4"}, "")
	if err != nil || len(items) != 2 {
		t.Fatalf("%v %v", err, items)
	}
	for _, id := range []domain.ItemID{"d1aaaaaa", "d4aaaaaa"} {
		m := metadata(t, f, id)
		if m["parent"] != "" || m["deleted"] != false {
			t.Fatalf("%s: %v", id, m)
		}
	}
	if _, err := f.s.Restore(t.Context(), []string{"d1"}, ""); !errors.Is(err, domain.ErrNotInTrash) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.s.Restore(t.Context(), []string{"/Nope"}, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestRestoreIntoAFolder(t *testing.T) {
	t.Parallel()
	f := setup(t)
	items, err := f.s.Restore(t.Context(), []string{"d3"}, "/Books/Fiction")
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %v", err, items)
	}
	if m := metadata(t, f, "d3aaaaaa"); m["parent"] != "f2aaaaaa" || m["deleted"] != false {
		t.Fatalf("%v", m)
	}
	tests := []struct {
		name, dest string
		want       error
	}{
		{name: "a document", dest: "/Books/Paper", want: domain.ErrNotAFolder},
		{name: "missing", dest: "/Nope", want: domain.ErrNotFound},
	}
	for _, tt := range tests {
		f := setup(t)
		if _, err := f.s.Restore(t.Context(), []string{"d3"}, tt.dest); !errors.Is(err, tt.want) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
	// A trashed folder is no destination: it resolves only through its id.
	f = setup(t)
	if _, err := f.s.Move(t.Context(), "d1", "f3aaaaaa"); !errors.Is(err, domain.ErrDestinationGone) {
		t.Fatalf("move into the trash: %v", err)
	}
	if _, err := f.s.Move(t.Context(), "d1", "f3aaaaaa/Name"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("move below the trash: %v", err)
	}
	if _, err := f.s.Restore(t.Context(), []string{"d3"}, "f3aaaaaa"); !errors.Is(err, domain.ErrDestinationGone) {
		t.Fatalf("into the trash: %v", err)
	}
	// Nor is the restored folder itself.
	f = setup(t)
	if _, err := f.s.Restore(t.Context(), []string{"f3"}, "f3aaaaaa"); !errors.Is(err, domain.ErrDestinationGone) &&
		!errors.Is(err, domain.ErrIntoOwnSubtree) {
		t.Fatalf("into itself: %v", err)
	}
	if f.c.Count("CommitRoot") != 0 {
		t.Fatal("a refused restore wrote")
	}
}

func TestEditFailures(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.c.Fail["Root"] = errBoom
	if _, err := f.s.Restore(t.Context(), []string{"d3"}, ""); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.s.Trash(t.Context(), []string{"d1"}); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	f.roots.before = func() { f.c.SetRoot(f.c.Records()[:1]) }
	if _, err := f.s.Trash(t.Context(), []string{"d1"}); !errors.Is(err, domain.ErrVanished) {
		t.Fatalf("got %v", err)
	}
	for _, method := range []string{"Upload", "Read"} {
		f := setup(t)
		f.roots.before = func() { f.c.Fail[method] = errBoom }
		if _, err := f.s.Trash(t.Context(), []string{"d1"}); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	f = setup(t)
	f.roots.before = func() {
		recs := f.c.Records()
		recs[4] = f.c.Record(porttest.Doc{ID: "d3aaaaaa", Name: "Old", Ext: "pdf"})
		f.c.SetRoot(recs)
	}
	items, err := f.s.Restore(t.Context(), []string{"d3"}, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("restoring an item restored meanwhile: %v %v", err, items)
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	f := setup(t)
	removed, err := f.s.Remove(t.Context(), []string{"/Books", "f2", "d3"})
	if err != nil || len(removed) != 5 {
		t.Fatalf("%v %v", err, removed)
	}
	if got := len(f.c.Records()); got != 2 {
		t.Fatalf("%d records left", got)
	}
	if _, err := f.s.Remove(t.Context(), []string{"/Nope"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	f.roots.before = func() { f.c.SetRoot(f.c.Records()[1:]) }
	removed, err = f.s.Remove(t.Context(), []string{"f1"})
	if err != nil || len(removed) != 0 {
		t.Fatalf("%v %v", err, removed)
	}
	f = setup(t)
	f.c.Fail["Root"] = errBoom
	if _, err := f.s.Remove(t.Context(), []string{"f1"}); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	lib := library.New(c, c, c)
	files := porttest.NewMemFiles()
	pdf := porttest.PDF{}
	ids := porttest.NewIDs("x")
	clk := porttest.NewClock(porttest.Epoch)
	arc := porttest.NewArchiver(files)
	for i, fn := range []func(){
		func() { write.New(nil, c, c, files, pdf, arc, ids, clk) },
		func() { write.New(lib, nil, c, files, pdf, arc, ids, clk) },
		func() { write.New(lib, c, nil, files, pdf, arc, ids, clk) },
		func() { write.New(lib, c, c, nil, pdf, arc, ids, clk) },
		func() { write.New(lib, c, c, files, nil, arc, ids, clk) },
		func() { write.New(lib, c, c, files, pdf, arc, nil, clk) },
		func() { write.New(lib, c, c, files, pdf, arc, ids, nil) },
		func() { write.New(lib, c, c, files, pdf, nil, ids, clk) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("case %d did not panic", i)
				}
			}()
			fn()
		}()
	}
}

func libraryOf(c *porttest.Cloud) *library.Library { return library.New(c, c, c) }
