// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package outdir_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/outdir"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

func library1(t *testing.T) (*library.Library, *porttest.Cloud) {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "f1aaaaaa", Name: "Books: Old", Folder: true},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper.pdf", Parent: "f1aaaaaa", Ext: "pdf"},
	)
	return library.New(c, c, c), c
}

func TestFolderAndTop(t *testing.T) {
	t.Parallel()
	lib, c := library1(t)
	for query, want := range map[string]bool{"": true, "/": true, "/Books: Old": true, "d1": false} {
		if got, err := outdir.Folder(t.Context(), lib, query); err != nil || got != want {
			t.Fatalf("%q: %v %v", query, got, err)
		}
	}
	if _, err := outdir.Folder(t.Context(), lib, "/Nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	tree, _, err := lib.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id, name, err := outdir.Top(tree, "/", errBoom); err != nil || id != domain.RootID || name != outdir.TopDir {
		t.Fatalf("%s %s %v", id, name, err)
	}
	if id, name, err := outdir.Top(tree, "/Books: Old", errBoom); err != nil || id != "f1aaaaaa" || name != "Books- Old" {
		t.Fatalf("%s %s %v", id, name, err)
	}
	if _, _, err := outdir.Top(tree, "d1", errBoom); !errors.Is(err, errBoom) || !strings.HasPrefix(err.Error(), "Paper.pdf: ") {
		t.Fatal(err)
	}
	if _, _, err := outdir.Top(tree, "/Nope", errBoom); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	c.Fail = map[string]error{"Root": errBoom}
	if _, err := outdir.Folder(t.Context(), lib, "/Books: Old"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// TestPlanCollisions checks that the first of a collision, by creation and
// then identifier, keeps its name, that names collide without case, that
// a folder and a document share one directory's names, and that the
// extension is the one asked for.
func TestPlanCollisions(t *testing.T) {
	t.Parallel()
	at := func(h int) time.Time { return porttest.Epoch.Add(time.Duration(h) * time.Hour) }
	items := []domain.Item{
		{ID: "zz000001", Name: "notes", Kind: domain.KindNotebook, Created: at(1)},
		{ID: "aa000002", Name: "Notes.epub", Kind: domain.KindEPUB, Created: at(2)},
		{ID: "cc000004", Name: "Notes.md", Kind: domain.KindFolder, Created: at(0)},
		{ID: "dd000005", Name: "x", Kind: domain.KindNotebook, Parent: "cc000004"},
	}
	plan := outdir.Plan(domain.NewTree(1, items), domain.RootID, "out", false, "md")
	got := make([]string, len(plan))
	for i, p := range plan {
		got[i] = string(p.Document.ID) + " " + p.LibraryPath + " " + p.Path
	}
	want := []string{
		"aa000002 /Notes.epub out/Notes (aa000002).md",
		"dd000005 /Notes.md/x out/Notes.md/x.md",
		"zz000001 /notes out/notes (zz000001).md",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}

func TestEach(t *testing.T) {
	t.Parallel()
	got, err := outdir.Each(t.Context(), 5, 0, func(_ context.Context, i int) (int, error) { return i * i, nil })
	if err != nil || !slices.Equal(got, []int{0, 1, 4, 9, 16}) {
		t.Fatalf("%v %v", got, err)
	}
	// A failure stops the rest; what was done comes back with it.
	got, err = outdir.Each(t.Context(), 5, 1, func(_ context.Context, i int) (int, error) {
		if i == 2 {
			return 0, errBoom
		}
		return i, nil
	})
	if !errors.Is(err, errBoom) || !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("%v %v", got, err)
	}
}

func TestRecord(t *testing.T) {
	t.Parallel()
	files := porttest.NewMemFiles()
	if got := outdir.Read(files, "out", ".rec.json"); len(got) != 0 {
		t.Fatal(got)
	}
	it := domain.Item{ID: "d1", Modified: porttest.Epoch}
	docs := map[string]outdir.Entry{
		outdir.Key("out", "out/Sub/A.md"): outdir.EntryOf(it, "opts"),
		"B.md":                            {ID: "d2", Modified: porttest.Epoch, Options: "opts", Empty: true},
	}
	if err := outdir.Write(t.Context(), files, "out", ".rec.json", docs); err != nil {
		t.Fatal(err)
	}
	data, _ := files.File("out/.rec.json")
	if string(data) != `{"v":1,"documents":{"B.md":{"id":"d2","modified":"2026-01-02T03:04:05Z","options":"opts","empty":true},"Sub/A.md":{"id":"d1","modified":"2026-01-02T03:04:05Z","options":"opts"}}}` {
		t.Fatalf("%s", data)
	}
	got := outdir.Read(files, "out", ".rec.json")
	if !maps.EqualFunc(got, docs, func(a, b outdir.Entry) bool { return a.ID == b.ID && a.Modified.Equal(b.Modified) && a.Options == b.Options && a.Empty == b.Empty }) {
		t.Fatalf("%v", got)
	}
	if !got["Sub/A.md"].Matches(it, "opts") || got["Sub/A.md"].Matches(it, "other") || got["Sub/A.md"].Matches(domain.Item{ID: "d1"}, "opts") {
		t.Fatal("matches")
	}
	// Pruned drops what does not match the plan, and only that.
	plan := []outdir.Planned{{Document: it, Path: "out/Sub/A.md"}, {Document: domain.Item{ID: "d9"}, Path: "out/B.md"}, {Document: it, Path: "out/C.md"}}
	pruned, dropped := outdir.Pruned(got, "out", plan, "opts")
	if !dropped || len(pruned) != 1 || len(got) != 2 {
		t.Fatalf("%v %v", pruned, dropped)
	}
	if _, dropped = outdir.Pruned(pruned, "out", plan, "opts"); dropped {
		t.Fatal("dropped again")
	}
	// Anything but a record of this layout is no record.
	for name, body := range map[string]string{"corrupt": "{not json", "version": `{"v":2,"documents":{}}`, "none": `{"v":1}`} {
		files.Write("out/.rec.json", []byte(body))
		if got := outdir.Read(files, "out", ".rec.json"); len(got) != 0 {
			t.Fatalf("%s: %v", name, got)
		}
	}
	files.Fail["Open"] = errBoom
	if got := outdir.Read(files, "out", ".rec.json"); len(got) != 0 {
		t.Fatal(got)
	}
	files.Fail["Produce"] = errBoom
	if err := outdir.Write(t.Context(), files, "out", ".rec.json", docs); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestKeyAndSameTime(t *testing.T) {
	t.Parallel()
	if got := outdir.Key("out", "elsewhere/A.md"); got != "../elsewhere/A.md" {
		t.Fatal(got)
	}
	if got := outdir.Key("/abs", "rel/A.md"); got != "rel/A.md" {
		t.Fatal(got)
	}
	if !outdir.SameTime(porttest.Epoch.Add(400*time.Millisecond), porttest.Epoch) || outdir.SameTime(porttest.Epoch.Add(time.Second), porttest.Epoch) {
		t.Fatal("same time")
	}
}
