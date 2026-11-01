// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package inspect_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/inspect"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

func setup(t testing.TB) (*inspect.Service, *porttest.Cloud) {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "f1aaaaaa", Name: "Books", Folder: true},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Ext: "pdf"},
		porttest.Doc{ID: "d2aaaaaa", Name: "Notes", RM: true},
	)
	return inspect.New(library.New(c, c, c), c, c, c, porttest.NewMemFiles()), c
}

func TestRootAndBlob(t *testing.T) {
	t.Parallel()
	s, c := setup(t)
	state, err := s.Root(t.Context())
	if err != nil || len(state.Records) != 3 {
		t.Fatalf("%v %+v", err, state)
	}
	f := c.Put("x", []byte("raw"))
	if b, err := s.Blob(t.Context(), f.Hash, "x"); err != nil || string(b) != "raw" {
		t.Fatalf("%v %q", err, b)
	}
}

func TestFiles(t *testing.T) {
	t.Parallel()
	s, c := setup(t)
	pagedata := c.Put("d1aaaaaa.pagedata", []byte("Blank\n"))
	binary := c.Put("d2aaaaaa.json", []byte{0xff, 0xfe})
	rec1 := c.Record(porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Ext: "pdf", Extra: []domain.File{pagedata}})
	rec2 := c.Record(porttest.Doc{ID: "d2aaaaaa", Name: "Notes", RM: true, Extra: []domain.File{binary}})
	recs := c.Records()
	recs[1], recs[2] = rec1, rec2
	c.SetRoot(recs)

	it, reports, err := s.Files(t.Context(), "/Books/Paper")
	if err != nil || it.ID != "d1aaaaaa" || len(reports) != 4 {
		t.Fatalf("%v %+v", err, reports)
	}
	texts := 0
	for _, r := range reports {
		if r.Text != nil {
			texts++
		}
	}
	if texts != 3 {
		t.Fatalf("%d text files", texts)
	}
	_, reports, err = s.Files(t.Context(), "d2")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reports {
		if r.File.Name == "d2aaaaaa.json" && r.Text != nil {
			t.Fatal("binary bytes reported as text")
		}
	}
	if _, _, err := s.Files(t.Context(), "/"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, _, err := s.Files(t.Context(), "/Nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	c.DropBlob(pagedata.Hash)
	if _, _, err := inspect.New(library.New(c, c, c), c, c, c, porttest.NewMemFiles()).Files(t.Context(), "d1"); !errors.Is(err, porttest.ErrNoBlob) {
		t.Fatalf("got %v", err)
	}
	c.Fail["Root"] = errBoom
	if _, _, err := s.Files(t.Context(), "d1"); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestHashes(t *testing.T) {
	t.Parallel()
	s, c := setup(t)
	recs := c.Records()
	// The first record points at its index by a node hash; the others by the
	// index file's own hash.
	recs[0] = c.Readdress(recs[0], porttest.Hash([]byte("node hash")))
	c.SetRoot(recs)
	reports, err := s.Hashes(t.Context())
	if err != nil || len(reports) != 3 {
		t.Fatalf("%v %v", err, reports)
	}
	for _, r := range reports {
		if want := r.Record.ID != recs[0].ID; r.ByIndexFile != want {
			t.Fatalf("%s: by index file %v", r.Record.ID, r.ByIndexFile)
		}
	}
	c.Fail["IndexByFile"] = errBoom
	if _, err := s.Hashes(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	c.Fail["Root"] = errBoom
	if _, err := s.Hashes(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestSaveBlob(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	files := porttest.NewMemFiles()
	s := inspect.New(library.New(c, c, c), c, c, c, files)
	f := c.Put("x.rm", []byte("raw bytes"))
	got, err := s.SaveBlob(t.Context(), f.Hash, "x.rm", "/out/x.rm")
	if err != nil || got != "/out/x.rm" {
		t.Fatalf("%q %v", got, err)
	}
	if data, ok := files.File("/out/x.rm"); !ok || string(data) != "raw bytes" {
		t.Fatalf("%q %v", data, ok)
	}
	if _, err := s.SaveBlob(t.Context(), "missing", "x", "/out/y"); !errors.Is(err, porttest.ErrNoBlob) {
		t.Fatalf("got %v", err)
	}
	if _, ok := files.File("/out/y"); ok {
		t.Fatal("a failed read left a file")
	}
	files.Fail["Produce"] = errBoom
	if _, err := s.SaveBlob(t.Context(), f.Hash, "x.rm", "/out/z"); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckFiles(t *testing.T) {
	t.Parallel()
	s, c := setup(t)
	missing, err := s.CheckFiles(t.Context())
	if err != nil || len(missing) != 0 {
		t.Fatalf("%v %v", err, missing)
	}
	recs := c.Records()
	files, _ := c.Files(t.Context(), recs[2])
	var rm domain.File
	for _, f := range files {
		if f.Extension() == "rm" {
			rm = f
		}
	}
	c.DropBlob(rm.Hash)
	missing, err = s.CheckFiles(t.Context())
	if err != nil || len(missing) != 1 || missing[0] != rm.Hash {
		t.Fatalf("%v %v", err, missing)
	}
	c.Fail["Missing"] = errBoom
	if _, err := s.CheckFiles(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	c.Fail["Root"] = errBoom
	if _, err := s.CheckFiles(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestSweep(t *testing.T) {
	t.Parallel()
	s, c := setup(t)
	counts, err := s.Sweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if counts["metadata.visibleName"] != 3 || counts["content.fileType"] != 2 {
		t.Fatalf("%v", counts)
	}
	bad := c.Put("d9.content", []byte("[1]"))
	c.Add(porttest.Doc{ID: "d9", Name: "Odd", Extra: []domain.File{bad}})
	counts, err = inspect.New(library.New(c, c, c), c, c, c, porttest.NewMemFiles()).Sweep(t.Context())
	if err != nil || counts["content.<malformed>"] != 1 {
		t.Fatalf("%v %v", err, counts)
	}
	c.Fail["Read"] = errBoom
	if _, err := inspect.New(library.New(c, c, c), c, c, c, porttest.NewMemFiles()).Sweep(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	c.Fail["Root"] = errBoom
	if _, err := s.Sweep(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	lib := library.New(c, c, c)
	files := porttest.NewMemFiles()
	for i, fn := range []func(){
		func() { inspect.New(nil, c, c, c, files) },
		func() { inspect.New(lib, nil, c, c, files) },
		func() { inspect.New(lib, c, nil, c, files) },
		func() { inspect.New(lib, c, c, nil, files) },
		func() { inspect.New(lib, c, c, c, nil) },
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
	var _ port.Inspector = (*inspect.Service)(nil)
}
