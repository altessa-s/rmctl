// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func fileOf(t *testing.T, f fixture, id domain.ItemID, ext string) []byte {
	t.Helper()
	tree, _, err := library.New(f.c, f.c, f.c).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	it, ok := tree.Get(id)
	if !ok {
		t.Fatalf("%s is gone", id)
	}
	file, ok := it.File(ext)
	if !ok {
		t.Fatalf("%s has no .%s", id, ext)
	}
	data, _ := f.c.Blob(file.Hash)
	return data
}

func TestPutCover(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("a.pdf", []byte("%PDF"))
	f.pdf.Counts["a.pdf"] = 2
	f.files.Write("b.epub", []byte(epub))
	for path, cover := range map[string]port.Cover{"a.pdf": port.CoverFirst, "b.epub": port.CoverFirst} {
		it, err := f.s.Put(t.Context(), path, "", port.PutOptions{Cover: cover}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if c := fileOf(t, f, it.ID, "content"); !strings.Contains(string(c), `"coverPageNumber":0`) {
			t.Fatalf("%s: %s", path, c)
		}
	}
	it, err := f.s.Put(t.Context(), "a.pdf", "", port.PutOptions{Name: "default"}, nil)
	if err != nil || !strings.Contains(string(fileOf(t, f, it.ID, "content")), `"coverPageNumber":-1`) {
		t.Fatal(err)
	}
}

func TestPutDryRun(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("a.pdf", []byte("%PDF"))
	f.pdf.Counts["a.pdf"] = 1
	before := len(f.c.Records())
	it, err := f.s.Put(t.Context(), "a.pdf", "/Books", port.PutOptions{DryRun: true, Tags: []string{"t"}}, nil)
	if err != nil || it.ID != "" || it.Name != "a" || it.Parent != "f1aaaaaa" || it.Kind != domain.KindPDF || it.Size != 4 {
		t.Fatalf("%+v %v", it, err)
	}
	if len(f.c.Records()) != before || f.c.Count("UploadFile") != 0 || f.c.Count("CommitRoot") != 0 {
		t.Fatal("a dry run changed something")
	}
	// A dry run still refuses what a real one would.
	f.files.Write("empty.pdf", nil)
	if _, err := f.s.Put(t.Context(), "empty.pdf", "", port.PutOptions{DryRun: true}, nil); !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatalf("an empty file: %v", err)
	}
	f.files.Write("b.epub", []byte(epub))
	if it, err = f.s.Put(t.Context(), "b.epub", "/Nope", port.PutOptions{DryRun: true}, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a missing folder: %+v %v", it, err)
	}
	if it, err = f.s.Put(t.Context(), "b.epub", "", port.PutOptions{DryRun: true}, nil); err != nil || it.Kind != domain.KindEPUB {
		t.Fatalf("%+v %v", it, err)
	}
}

func TestPutReplace(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("Paper.pdf", []byte("%PDF new"))
	f.pdf.Counts["Paper.pdf"] = 2
	var replaced []domain.Item
	opts := port.PutOptions{Replace: true, Tags: []string{"new"}, OnReplace: func(old domain.Item) { replaced = append(replaced, old) }}
	// A dry run names what it would replace, and replaces nothing.
	dry := opts
	dry.DryRun = true
	if it, err := f.s.Put(t.Context(), "Paper.pdf", "/Books", dry, nil); err != nil || it.ID != "d1aaaaaa" || len(replaced) != 1 {
		t.Fatalf("%+v %v %v", it, err, replaced)
	}
	before := len(f.c.Records())
	beforeMeta := metadata(t, f, "d1aaaaaa")
	it, err := f.s.Put(t.Context(), "Paper.pdf", "/Books", opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "d1aaaaaa" || len(f.c.Records()) != before || len(replaced) != 2 || replaced[1].ID != "d1aaaaaa" {
		t.Fatalf("%+v records %d", it, len(f.c.Records()))
	}
	if !slices.Equal(it.Tags, []string{"new"}) {
		t.Fatalf("tags %v", it.Tags)
	}
	if body := fileOf(t, f, it.ID, "pdf"); string(body) != "%PDF new" {
		t.Fatalf("original %q", body)
	}
	after := metadata(t, f, "d1aaaaaa")
	if after["version"].(float64) != beforeMeta["version"].(float64)+1 || after["visibleName"] != "Paper" || after["parent"] != "f1aaaaaa" {
		t.Fatalf("%v", after)
	}
	var content map[string]any
	if err := json.Unmarshal(fileOf(t, f, it.ID, "content"), &content); err != nil || content["pageCount"] != float64(2) {
		t.Fatalf("%v %v", content, err)
	}
	if string(fileOf(t, f, it.ID, "pagedata")) != "Blank\nBlank\n" {
		t.Fatal("pagedata")
	}
	// No document of the name: the file is added as usual.
	f.files.Write("Fresh.pdf", []byte("%PDF"))
	f.pdf.Counts["Fresh.pdf"] = 1
	if it, err = f.s.Put(t.Context(), "Fresh.pdf", "/Books", opts, nil); err != nil || it.ID == "d1aaaaaa" || len(f.c.Records()) != before+1 {
		t.Fatalf("%+v %v", it, err)
	}
}

func TestPutReplaceKeepsTagsAndDropsPages(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("Novel.epub", []byte(epub))
	if _, err := f.s.Put(t.Context(), "Novel.epub", "/Books/Fiction", port.PutOptions{Replace: true, Tags: []string{"work"}}, nil); err != nil {
		t.Fatal(err)
	}
	it, err := f.s.Put(t.Context(), "Novel.epub", "/Books/Fiction", port.PutOptions{Replace: true, Tags: []string{"more"}}, nil)
	if err != nil || it.ID != "d2aaaaaa" {
		t.Fatalf("%+v %v", it, err)
	}
	if !slices.Equal(it.Tags, []string{"work", "more"}) {
		t.Fatalf("tags %v", it.Tags)
	}
	for _, file := range it.Files {
		if file.Extension() == "rm" || file.Extension() == "pagedata" {
			t.Fatalf("%s survived", file.Name)
		}
	}
}

func TestPutReplaceRefuses(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("Paper.pdf", []byte("%PDF"))
	f.pdf.Counts["Paper.pdf"] = 1
	// Two documents of the name: which one is meant is not guessed.
	if _, err := f.s.Put(t.Context(), "Paper.pdf", "/Books", port.PutOptions{Name: "Paper"}, nil); err != nil {
		t.Fatal(err)
	}
	var amb *domain.AmbiguousError
	if _, err := f.s.Put(t.Context(), "Paper.pdf", "/Books", port.PutOptions{Replace: true}, nil); !errors.As(err, &amb) {
		t.Fatalf("%v", err)
	}
	// The document trashed between the read and the commit is not replaced.
	g := setup(t)
	g.files.Write("Paper.pdf", []byte("%PDF"))
	g.pdf.Counts["Paper.pdf"] = 1
	g.roots.before = func() {
		g.c.SetRoot(slices.DeleteFunc(g.c.Records(), func(r domain.Record) bool { return r.ID == "d1aaaaaa" }))
	}
	if _, err := g.s.Put(t.Context(), "Paper.pdf", "/Books", port.PutOptions{Replace: true}, nil); !errors.Is(err, domain.ErrVanished) {
		t.Fatalf("%v", err)
	}
	for _, method := range []string{"UploadFile", "Upload", "PutIndex", "Read"} {
		h := setup(t)
		h.files.Write("Paper.pdf", []byte("%PDF"))
		h.pdf.Counts["Paper.pdf"] = 1
		h.c.Fail[method] = errBoom
		if _, err := h.s.Put(t.Context(), "Paper.pdf", "/Books", port.PutOptions{Replace: true}, nil); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
	}
}

func TestPutRefusesAnEmptyFile(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("empty.pdf", []byte{})
	if _, err := f.s.Put(t.Context(), "empty.pdf", "", port.PutOptions{}, nil); !errors.Is(err, domain.ErrUnsupportedFile) || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("%v", err)
	}
}

// The document a replacement chose is the one of that name in that folder:
// moved or renamed by another writer before the commit, it is not replaced
// in its new place, and nothing of it changes.
func TestPutReplaceRefusesATargetThatMoved(t *testing.T) {
	t.Parallel()
	for name, meanwhile := range map[string]string{"moved": "/", "renamed": "/Books/Elsewhere"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			f.files.Write("Paper.pdf", []byte("%PDF new"))
			f.pdf.Counts["Paper.pdf"] = 2
			before := fileOf(t, f, "d1aaaaaa", "pdf")
			// Another writer is another process: its own library, its own
			// commit lock, the same cloud.
			other := write.New(library.New(f.c, f.c, f.c), f.c, f.c, f.files, pdfInspector{f.pdf}, porttest.NewArchiver(f.files),
				porttest.NewIDs("other"), porttest.NewClock(porttest.Epoch))
			f.c.Conflicts = 1
			f.c.OnConflict = func() {
				if _, err := other.Move(t.Context(), "d1aaaaaa", meanwhile); err != nil {
					t.Error(err)
				}
			}
			_, err := f.s.Put(t.Context(), "Paper.pdf", "/Books", port.PutOptions{Replace: true}, nil)
			if !errors.Is(err, domain.ErrReplaceTargetChanged) {
				t.Fatalf("err %v", err)
			}
			if after := fileOf(t, f, "d1aaaaaa", "pdf"); !bytes.Equal(after, before) {
				t.Fatal("the moved document was replaced")
			}
		})
	}
}
