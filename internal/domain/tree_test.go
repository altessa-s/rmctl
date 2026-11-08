// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

// item builds an item through NewItem, the way the loader does.
func item(t testing.TB, id, name string, folder bool, parent string, files ...domain.File) domain.Item {
	t.Helper()
	typ := domain.TypeDocument
	if folder {
		typ = domain.TypeCollection
	}
	meta, err := domain.ParseMetadata([]byte(fmt.Sprintf(`{"visibleName":%q,"type":%q,"parent":%q}`, name, typ, parent)))
	if err != nil {
		t.Fatal(err)
	}
	return domain.NewItem(domain.Record{ID: domain.ItemID(id), Hash: "h-" + id}, files, meta, nil)
}

func ids(items []domain.Item) []domain.ItemID {
	out := make([]domain.ItemID, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// library:
//
//	/Books/            b1
//	  Go.pdf           d1
//	  Old/             b2 (trashed)
//	    Inside.pdf     d2
//	/Notes             n1 (notebook)
//	/notes             n2 (same name, other case)
//	Gone.pdf           d3 (trashed at the top)
//	Lost.pdf           d4 (parent missing: stranded)
//	duplicate b1       (dropped)
func library(t testing.TB) *domain.Tree {
	t.Helper()
	pdf := domain.File{Name: "x.pdf", Hash: "p", Size: 10}
	return domain.NewTree(7, []domain.Item{
		item(t, "b1aaaaaa", "Books", true, ""),
		item(t, "d1", "Go.pdf", false, "b1aaaaaa", pdf),
		item(t, "b2", "Old", true, "trash"),
		item(t, "d2", "Inside.pdf", false, "b2", pdf),
		item(t, "n1", "Notes", false, ""),
		item(t, "n2", "notes", false, ""),
		item(t, "d3", "Gone.pdf", false, "trash", pdf),
		item(t, "d4", "Lost.pdf", false, "nowhere", pdf),
		item(t, "b1aaaaaa", "Duplicate", true, ""),
	})
}

func TestTreeShape(t *testing.T) {
	t.Parallel()
	tr := library(t)
	if tr.Len() != 8 || tr.Repeated != 1 || tr.Generation != 7 || len(tr.Items()) != 8 {
		t.Fatalf("len %d repeated %d", tr.Len(), tr.Repeated)
	}
	if got := ids(tr.Children(domain.RootID, false)); !slices.Equal(got, []domain.ItemID{"b1aaaaaa", "d4", "n1", "n2"}) {
		t.Fatalf("top %v", got)
	}
	if got := ids(tr.Children(domain.RootID, true)); !slices.Equal(got, []domain.ItemID{"b1aaaaaa", "b2", "d3", "d4", "n1", "n2"}) {
		t.Fatalf("top with trash %v", got)
	}
	d4, _ := tr.Get("d4")
	if !tr.Stranded(d4) {
		t.Fatal("d4 is stranded")
	}
	d1, _ := tr.Get("d1")
	if tr.Stranded(d1) || d1.Kind != domain.KindPDF || d1.Size != 10 {
		t.Fatalf("%+v", d1)
	}
	if _, ok := tr.Get("missing"); ok {
		t.Fatal("found a missing item")
	}
}

func TestTrashIsInherited(t *testing.T) {
	t.Parallel()
	tr := library(t)
	for id, want := range map[domain.ItemID]bool{"b2": true, "d2": true, "d3": true, "d1": false, "d4": false, "missing": false} {
		if got := tr.IsTrashed(id); got != want {
			t.Errorf("%s: %v", id, got)
		}
	}
	d2, _ := tr.Get("d2")
	if d2.TrashedHere() {
		t.Fatal("d2 is trashed only through its folder")
	}
}

func TestTrashWalkSurvivesALoop(t *testing.T) {
	t.Parallel()
	tr := domain.NewTree(1, []domain.Item{item(t, "a", "A", true, "b"), item(t, "b", "B", true, "a")})
	if tr.IsTrashed("a") {
		t.Fatal("a loop is not the trash")
	}
	if p := tr.Path("a"); p != "/B/A" {
		t.Fatalf("path %q", p)
	}
	if tr.IsWithin("a", "zzz") {
		t.Fatal("a loop is within nothing")
	}
}

func TestDescendantsAndWithin(t *testing.T) {
	t.Parallel()
	tr := domain.NewTree(1, []domain.Item{
		item(t, "a", "A", true, ""), item(t, "b", "B", true, "a"), item(t, "c", "C", false, "b"), item(t, "d", "D", false, ""),
	})
	if got := ids(tr.Descendants("a")); !slices.Equal(got, []domain.ItemID{"b", "c"}) {
		t.Fatalf("%v", got)
	}
	if !tr.IsWithin("c", "a") || !tr.IsWithin("a", "a") || tr.IsWithin("d", "a") || tr.IsWithin("a", "c") {
		t.Fatal("within")
	}
	if tr.Path("c") != "/A/B/C" || tr.Path(domain.RootID) != "/" {
		t.Fatalf("path %q", tr.Path("c"))
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	tr := library(t)
	tests := []struct {
		name, query string
		all         bool
		want        domain.ItemID
		found       bool
		err         error
		ambiguous   bool
	}{
		{name: "full id", query: "d1", want: "d1", found: true},
		{name: "id prefix", query: "b1a", want: "b1aaaaaa", found: true},
		{name: "path", query: "/Books/Go.pdf", want: "d1", found: true},
		{name: "relative path", query: "Books/Go.pdf", want: "d1", found: true},
		{name: "trailing slash", query: "/Books/", want: "b1aaaaaa", found: true},
		{name: "top", query: "/", found: false},
		{name: "case matters", query: "/Notes", want: "n1", found: true},
		{name: "missing", query: "/Nope", err: domain.ErrNotFound},
		{name: "through a document", query: "/Books/Go.pdf/x", err: domain.ErrNotAFolder},
		{name: "trashed hidden", query: "/Old", err: domain.ErrNotFound},
		{name: "trashed with all", query: "/Old/Inside.pdf", all: true, want: "d2", found: true},
		{name: "trashed id is hidden", query: "d3", err: domain.ErrNotFound},
		{name: "id inside a trashed folder is hidden", query: "d2", err: domain.ErrNotFound},
		{name: "trashed id with all", query: "d3", all: true, want: "d3", found: true},
		{name: "ambiguous prefix", query: "d", ambiguous: true},
		{name: "empty", query: "", err: domain.ErrEmptyName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			it, found, err := tr.Resolve(tt.query, tt.all)
			var amb *domain.AmbiguousError
			switch {
			case tt.ambiguous:
				if !errors.As(err, &amb) || len(amb.Candidates) < 2 || amb.Error() == "" {
					t.Fatalf("err %v", err)
				}
			case tt.err != nil:
				if !errors.Is(err, tt.err) {
					t.Fatalf("err %v", err)
				}
			default:
				if err != nil || found != tt.found || it.ID != tt.want {
					t.Fatalf("%v %v %v", it.ID, found, err)
				}
			}
		})
	}
}

func TestResolveAmbiguousPath(t *testing.T) {
	t.Parallel()
	tr := domain.NewTree(1, []domain.Item{item(t, "a", "Same", false, ""), item(t, "b", "Same", false, "")})
	var amb *domain.AmbiguousError
	if _, _, err := tr.Resolve("/Same", false); !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Fatalf("err %v", err)
	}
}

func TestResolveFolder(t *testing.T) {
	t.Parallel()
	tr := library(t)
	for query, want := range map[string]domain.ItemID{"": domain.RootID, "/": domain.RootID, "/Books": "b1aaaaaa", "b1a": "b1aaaaaa"} {
		if got, err := tr.ResolveFolder(query); err != nil || got != want {
			t.Errorf("%q: %v %v", query, got, err)
		}
	}
	if _, err := tr.ResolveFolder("d1"); !errors.Is(err, domain.ErrNotAFolder) {
		t.Fatalf("err %v", err)
	}
	if _, err := tr.ResolveFolder("/Nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}

func FuzzResolve(f *testing.F) {
	for _, q := range []string{"/Books/Go.pdf", "b1", "/", "//", "d", "Books/../x", "/Old/Inside.pdf"} {
		f.Add(q, false)
	}
	tr := library(f)
	f.Fuzz(func(t *testing.T, query string, all bool) {
		it, found, err := tr.Resolve(query, all)
		if err != nil {
			if found {
				t.Fatal("found with an error")
			}
			return
		}
		if found {
			if _, ok := tr.Get(it.ID); !ok {
				t.Fatalf("resolved %q to an item not in the tree", query)
			}
			if !all && tr.IsTrashed(it.ID) && query[0] == '/' {
				t.Fatalf("a path reached a trashed item without --all: %q", query)
			}
		}
	})
}

func TestNewListedItem(t *testing.T) {
	t.Parallel()
	tree := domain.NewTree(1, []domain.Item{
		domain.NewListedItem("f", "Folder", domain.KindFolder, domain.TrashID, true),
		domain.NewListedItem("d", "Doc", domain.KindPDF, "f", false),
	})
	d, ok := tree.Get("d")
	if !ok || d.Name != "Doc" || d.Kind != domain.KindPDF || d.Parent != "f" || d.TrashedHere() || len(d.Files) != 0 {
		t.Fatalf("%+v", d)
	}
	if f, _ := tree.Get("f"); !f.TrashedHere() || !tree.IsTrashed("d") {
		t.Fatal("the trash flag is lost")
	}
}
