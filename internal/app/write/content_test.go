// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// annotated adds /Books/Annotated: a PDF of two pages, each with a .rm, a
// cached thumbnail, and the tags and pin a content upload must keep.
func annotated(t *testing.T, f fixture) domain.Item {
	t.Helper()
	f.c.Add(porttest.Doc{
		ID: "d9aaaaaa", Name: "Annotated", Parent: "f1aaaaaa", Ext: "pdf", Pinned: true, Tags: []string{"keep"},
		Pages: [][]byte{[]byte("ink on page one"), []byte("ink on page two")},
		Extra: []domain.File{
			f.c.Put("d9aaaaaa.thumbnails/p1.png", []byte("old picture")),
			f.c.Put("d9aaaaaa.highlights/p2.json", []byte(`{"highlights":[]}`)),
		},
	})
	it, ok := tree(t, f).Get("d9aaaaaa")
	if !ok {
		t.Fatal("not added")
	}
	return it
}

func newPDF(f fixture, pages int) string {
	path := fmt.Sprintf("in/Annotated-%d.pdf", pages)
	f.files.Write(path, []byte(fmt.Sprintf("%%PDF of %d pages", pages)))
	f.pdf.Counts[path] = pages
	return path
}

func contentOf(t *testing.T, f fixture, it domain.Item) map[string]any {
	t.Helper()
	cf, _ := it.File("content")
	data, _ := f.c.Blob(cf.Hash)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func blobOf(t *testing.T, f fixture, it domain.Item, name string) string {
	t.Helper()
	for _, file := range it.Files {
		if file.Name == name {
			b, _ := f.c.Blob(file.Hash)
			return string(b)
		}
	}
	return ""
}

func names(it domain.Item) []string {
	out := make([]string, 0, len(it.Files))
	for _, f := range it.Files {
		out = append(out, strings.TrimPrefix(f.Name, string(it.ID)))
	}
	slices.Sort(out)
	return out
}

func putContent(t *testing.T, f fixture, local string, opts port.PutOptions) (domain.Item, port.ContentPlan, error) {
	t.Helper()
	var plan port.ContentPlan
	opts.ContentOnly = true
	opts.Name = "Annotated"
	opts.OnContentPlan = func(p port.ContentPlan) { plan = p }
	it, err := f.s.Put(t.Context(), local, "/Books", opts, nil)
	return it, plan, err
}

func TestContentOnlySameAndMorePages(t *testing.T) {
	t.Parallel()
	f := setup(t)
	before := annotated(t, f)
	ink := map[string]string{}
	for _, file := range before.Files {
		if strings.HasSuffix(file.Name, ".rm") {
			ink[file.Name] = file.Hash
		}
	}
	it, plan, err := putContent(t, f, newPDF(f, 2), port.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.OldPages != 2 || plan.NewPages != 2 || plan.Kept != 2 || plan.Appended != 0 || plan.Dropped != 0 || plan.Annotated != 2 ||
		len(plan.IDMap) != 2 {
		t.Fatalf("%+v", plan)
	}
	if it.ID != before.ID || it.Name != "Annotated" || it.Parent != "f1aaaaaa" || !it.Pinned || !slices.Equal(it.Tags, []string{"keep"}) {
		t.Fatalf("%+v", it)
	}
	// Every kept page has a new identifier, and its ink and highlights are
	// the very same blobs under it; the thumbnail is gone; the PDF is new.
	c := contentOf(t, f, it)
	pages, _ := c["pages"].([]any)
	if len(pages) != 2 {
		t.Fatalf("%v", c)
	}
	p1, p2 := pages[0].(string), pages[1].(string)
	if p1 == "p1" || p2 == "p2" || plan.IDMap["p1"] != p1 || plan.IDMap["p2"] != p2 {
		t.Fatalf("the pages kept their identifiers: %v %v", pages, plan.IDMap)
	}
	hashes := map[string]string{}
	for _, file := range it.Files {
		hashes[file.Name] = file.Hash
	}
	if hashes["d9aaaaaa/"+p1+".rm"] != ink["d9aaaaaa/p1.rm"] || hashes["d9aaaaaa/"+p2+".rm"] != ink["d9aaaaaa/p2.rm"] ||
		hashes["d9aaaaaa.highlights/"+p2+".json"] != highlights(before) {
		t.Fatalf("the annotations did not follow their pages: %v", hashes)
	}
	want := []string{".content", ".highlights/" + p2 + ".json", ".metadata", ".pagedata", ".pdf", "/" + p1 + ".rm", "/" + p2 + ".rm"}
	slices.Sort(want)
	if !slices.Equal(names(it), want) {
		t.Fatalf("%v", names(it))
	}
	if blobOf(t, f, it, "d9aaaaaa.pdf") != "%PDF of 2 pages" {
		t.Fatal("the PDF was not replaced")
	}
	if c["sizeInBytes"] != "15" || fmt.Sprint(c["tags"]) != fmt.Sprint(contentOf(t, f, before)["tags"]) {
		t.Fatalf("%v", c)
	}
	if meta := metadata(t, f, it.ID); meta["visibleName"] != "Annotated" || meta["pinned"] != true || meta["parent"] != "f1aaaaaa" {
		t.Fatalf("%v", meta)
	}

	// Four pages: the two keep their places and ink, two new ones follow.
	it, plan, err = putContent(t, f, newPDF(f, 4), port.PutOptions{CurrentPage: 3})
	if err != nil || plan.Appended != 2 || plan.NewPages != 4 || plan.NewOriginal != 4 || plan.OldOriginal != 2 {
		t.Fatalf("%+v %v", plan, err)
	}
	c = contentOf(t, f, it)
	pages, _ = c["pages"].([]any)
	if len(pages) != 4 || pages[0] != plan.IDMap[p1] || pages[1] != plan.IDMap[p2] || fmt.Sprint(c["redirectionPageMap"]) != "[0 1 2 3]" ||
		c["pageCount"] != float64(4) || c["originalPageCount"] != float64(4) {
		t.Fatalf("%v", c)
	}
	if blobOf(t, f, it, "d9aaaaaa.pagedata") != "Blank\nBlank\nBlank\nBlank\n" || metadata(t, f, it.ID)["lastOpenedPage"] != float64(2) {
		t.Fatalf("pagedata or opening page")
	}

	// Back to two: the dropped pages carry no ink, so nothing is refused.
	if _, plan, err = putContent(t, f, newPDF(f, 2), port.PutOptions{}); err != nil || plan.Dropped != 2 || len(plan.Lost) != 0 {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestContentOnlyFewerPages(t *testing.T) {
	t.Parallel()
	f := setup(t)
	annotated(t, f)
	before := len(f.c.Records())
	_, plan, err := putContent(t, f, newPDF(f, 1), port.PutOptions{})
	if !errors.Is(err, domain.ErrAnnotationsDropped) || !strings.Contains(err.Error(), "page 2 (p2, 2 annotation files)") {
		t.Fatal(err)
	}
	if len(plan.Lost) != 1 || plan.Lost[0] != (port.AnnotatedPage{Number: 2, ID: "p2", Files: 2}) || plan.Dropped != 1 {
		t.Fatalf("%+v", plan)
	}
	if got, _ := tree(t, f).Get("d9aaaaaa"); len(got.Files) == 0 || len(f.c.Records()) != before {
		t.Fatal("a refused upload changed the library")
	}
	it, plan, err := putContent(t, f, newPDF(f, 1), port.PutOptions{DropAnnotations: true})
	if err != nil {
		t.Fatal(err)
	}
	p1 := plan.IDMap["p1"]
	if !slices.Equal(names(it), []string{".content", ".metadata", ".pagedata", ".pdf", "/" + p1 + ".rm"}) ||
		fmt.Sprint(contentOf(t, f, it)["pages"]) != "["+p1+"]" {
		t.Fatalf("%v", names(it))
	}
}

func TestContentOnlyDryRun(t *testing.T) {
	t.Parallel()
	f := setup(t)
	before := annotated(t, f)
	records := f.c.Records()
	it, plan, err := putContent(t, f, newPDF(f, 3), port.PutOptions{DryRun: true})
	if err != nil || it.ID != before.ID || plan.Appended != 1 {
		t.Fatalf("%+v %+v %v", it, plan, err)
	}
	if !slices.Equal(f.c.Records(), records) {
		t.Fatal("a dry run changed the root")
	}
	// A dry run refuses what the upload would.
	if _, _, err := putContent(t, f, newPDF(f, 1), port.PutOptions{DryRun: true}); !errors.Is(err, domain.ErrAnnotationsDropped) {
		t.Fatal(err)
	}
}

func TestContentOnlyRefusals(t *testing.T) {
	t.Parallel()
	f := setup(t)
	annotated(t, f)
	f.files.Write("in/Novel.epub", []byte(epub))
	pdf := newPDF(f, 2)
	tests := map[string]struct {
		local, dest, name string
		opts              port.PutOptions
		want              error
	}{
		"an EPUB":                 {local: "in/Novel.epub", dest: "/Books/Fiction", name: "Novel", want: domain.ErrUnsupportedFile},
		"no document of the name": {local: pdf, dest: "/Books", name: "Nothing", want: domain.ErrNoContentTarget},
		"not a PDF":               {local: pdf, dest: "/Books/Fiction", name: "Novel", want: domain.ErrNotPDFDocument},
		"with --replace":          {local: pdf, dest: "/Books", name: "Annotated", opts: port.PutOptions{Replace: true}, want: domain.ErrUnsupportedFile},
		"a page beyond":           {local: pdf, dest: "/Books", name: "Annotated", opts: port.PutOptions{CurrentPage: 3}, want: domain.ErrNoSuchPage},
	}
	for name, tc := range tests {
		opts := tc.opts
		opts.ContentOnly, opts.Name = true, tc.name
		if _, err := f.s.Put(t.Context(), tc.local, tc.dest, opts, nil); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCurrentPage(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("in/Three.pdf", []byte("%PDF three"))
	f.pdf.Counts["in/Three.pdf"] = 3
	it, err := f.s.Put(t.Context(), "in/Three.pdf", "/", port.PutOptions{CurrentPage: 3}, nil)
	if err != nil || metadata(t, f, it.ID)["lastOpenedPage"] != float64(2) {
		t.Fatalf("%v %v", err, metadata(t, f, it.ID))
	}
	it, err = f.s.Put(t.Context(), "in/Three.pdf", "/", port.PutOptions{Replace: true, CurrentPage: 1}, nil)
	if err != nil || metadata(t, f, it.ID)["lastOpenedPage"] != float64(0) {
		t.Fatalf("%v %v", err, metadata(t, f, it.ID))
	}
	if _, err := f.s.Put(t.Context(), "in/Three.pdf", "/", port.PutOptions{CurrentPage: 4}, nil); !errors.Is(err, domain.ErrNoSuchPage) {
		t.Fatal(err)
	}
	f.files.Write("in/Book.epub", []byte(epub))
	if _, err := f.s.Put(t.Context(), "in/Book.epub", "/", port.PutOptions{CurrentPage: 1}, nil); !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatal(err)
	}
	if _, err := f.s.Put(t.Context(), "in/Three.pdf", "/", port.PutOptions{CurrentPage: -1}, nil); !errors.Is(err, domain.ErrNoSuchPage) {
		t.Fatal(err)
	}
}

func highlights(it domain.Item) string {
	for _, f := range it.Files {
		if strings.Contains(f.Name, ".highlights/") {
			return f.Hash
		}
	}
	return ""
}

// A conflict replays the change, and the pages take the identifiers the
// plan announced, not new ones.
func TestContentOnlyReplayKeepsTheAnnouncedIDs(t *testing.T) {
	t.Parallel()
	f := setup(t)
	annotated(t, f)
	f.c.Conflicts = 1
	it, plan, err := putContent(t, f, newPDF(f, 3), port.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pages, _ := contentOf(t, f, it)["pages"].([]any)
	if len(pages) != 3 || pages[0] != plan.IDMap["p1"] || pages[1] != plan.IDMap["p2"] {
		t.Fatalf("%v %v", pages, plan.IDMap)
	}
	for _, file := range it.Files {
		if strings.Contains(file.Name, "/p1.") || strings.Contains(file.Name, "/p2.") {
			t.Fatalf("%s is filed under an old identifier", file.Name)
		}
	}
}
