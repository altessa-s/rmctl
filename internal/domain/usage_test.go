// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"fmt"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

// usageItem is an item of a usage test: a folder, or a document whose
// original is a file of hash and size.
func usageItem(t *testing.T, id, parent, typ, ext, hash string, size int64) domain.Item {
	t.Helper()
	meta, err := domain.ParseMetadata(fmt.Appendf(nil, `{"visibleName":%q,"type":%q,"parent":%q}`, id, typ, parent))
	if err != nil {
		t.Fatal(err)
	}
	files := []domain.File{{Name: id + ".metadata", Hash: "m-" + id, Size: 0}}
	if ext != "" {
		files = append(files, domain.File{Name: id + "." + ext, Hash: hash, Size: size})
	} else if typ == domain.TypeDocument {
		files = append(files, domain.File{Name: id + "/p1.rm", Hash: hash, Size: size})
	}
	return domain.NewItem(domain.Record{ID: domain.ItemID(id)}, files, meta, nil)
}

// usageTree is:
//
//	A/        (folder)
//	  B/      (folder)
//	    b1    pdf 300 hash h1
//	  a1      pdf 100 hash h1
//	  a2      epub 50 hash h2
//	n1        notebook 20 hash h1
//	t1        pdf 1000 hash h1, in the trash
//	r1        pdf 100 hash h1
func usageTree(t *testing.T) *domain.Tree {
	t.Helper()
	return domain.NewTree(1, []domain.Item{
		usageItem(t, "A", "", domain.TypeCollection, "", "", 0),
		usageItem(t, "B", "A", domain.TypeCollection, "", "", 0),
		usageItem(t, "b1", "B", domain.TypeDocument, "pdf", "h1", 300),
		usageItem(t, "a1", "A", domain.TypeDocument, "pdf", "h1", 100),
		usageItem(t, "a2", "A", domain.TypeDocument, "epub", "h2", 50),
		usageItem(t, "n1", "", domain.TypeDocument, "", "h1", 20),
		usageItem(t, "t1", "trash", domain.TypeDocument, "pdf", "h1", 1000),
		usageItem(t, "r1", "", domain.TypeDocument, "pdf", "h1", 100),
	})
}

func TestUsage(t *testing.T) {
	t.Parallel()
	tree := usageTree(t)
	u := tree.Usage(domain.RootID, 1, false)
	if u.Total.Path != "/" || u.Total.Bytes != 570 || u.Total.Documents != 5 || u.Total.Folders != 2 {
		t.Fatalf("%+v", u.Total)
	}
	if len(u.Folders) != 1 || u.Folders[0].Path != "/A" || u.Folders[0].Bytes != 450 || u.Folders[0].Documents != 3 || u.Folders[0].Folders != 1 {
		t.Fatalf("%+v", u.Folders)
	}
	if fmt.Sprint(u.Kinds) != "[{pdf 3 500} {epub 1 50} {notebook 1 20}]" {
		t.Fatalf("%v", u.Kinds)
	}
	// Deeper, largest first; 0 is the total alone; the trash counts with all.
	if u := tree.Usage(domain.RootID, 2, false); len(u.Folders) != 2 || u.Folders[0].Path != "/A" || u.Folders[1].Path != "/A/B" || u.Folders[1].Depth != 2 {
		t.Fatalf("%+v", u.Folders)
	}
	if u := tree.Usage(domain.RootID, 0, false); len(u.Folders) != 0 || u.Total.Bytes != 570 {
		t.Fatalf("%+v", u)
	}
	if u := tree.Usage(domain.RootID, 1, true); u.Total.Bytes != 1570 || u.Total.Documents != 6 {
		t.Fatalf("%+v", u.Total)
	}
	if u := tree.Usage("A", 1, false); u.Total.Path != "/A" || u.Total.Bytes != 450 || len(u.Folders) != 1 || u.Folders[0].Depth != 1 {
		t.Fatalf("%+v", u)
	}
	// A loop counts each folder once.
	loop := domain.NewTree(1, []domain.Item{
		usageItem(t, "x", "y", domain.TypeCollection, "", "", 0),
		usageItem(t, "y", "x", domain.TypeCollection, "", "", 0),
		usageItem(t, "d", "x", domain.TypeDocument, "pdf", "h", 5),
	})
	if u := loop.Usage("x", 5, false); u.Total.Bytes != 5 || u.Total.Folders != 1 {
		t.Fatalf("%+v", u)
	}
}

func TestDuplicates(t *testing.T) {
	t.Parallel()
	tree := usageTree(t)
	groups := tree.Duplicates(domain.RootID, false)
	if len(groups) != 1 {
		t.Fatalf("%+v", groups)
	}
	g := groups[0]
	var ids []domain.ItemID
	for _, it := range g.Items {
		ids = append(ids, it.ID)
	}
	// The notebook has no original; the trash is out; sorted by path.
	if g.Hash != "h1" || g.Size != 100 || fmt.Sprint(ids) != "[b1 a1 r1]" || g.Wasted() != 200 {
		t.Fatalf("%+v %v", g, ids)
	}
	if groups := tree.Duplicates(domain.RootID, true); len(groups[0].Items) != 4 {
		t.Fatalf("%+v", groups)
	}
	// Below /A only one copy of h1 is left besides b1's, h2 is alone.
	if groups := tree.Duplicates("A", false); len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf("%+v", groups)
	}
	if groups := tree.Duplicates("B", false); len(groups) != 0 {
		t.Fatalf("%+v", groups)
	}
}
