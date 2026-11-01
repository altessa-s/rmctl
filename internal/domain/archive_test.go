// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

func TestGroupArchive(t *testing.T) {
	t.Parallel()
	entries := []domain.ArchiveEntry{
		{Name: "a.metadata", DocumentID: "a"},
		{Name: "b.metadata", DocumentID: "b"},
		{Name: "a/p.rm", DocumentID: "a"},
		{Name: ".x", DocumentID: ""},
	}
	got := domain.GroupArchive(entries)
	if len(got) != 3 || got[0].ID != "a" || len(got[0].Entries) != 2 || got[0].Entries[1].Name != "a/p.rm" ||
		got[1].ID != "b" || got[2].ID != "" {
		t.Fatalf("%+v", got)
	}
	if domain.GroupArchive(nil) != nil {
		t.Fatal("no entries make no documents")
	}
}

func TestRenameRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, want string
		ok         bool
	}{
		{"old.metadata", "new.metadata", true},
		{"old/p1.rm", "new/p1.rm", true},
		{"old/p1/img.png", "new/p1/img.png", true},
		{"older.metadata", "older.metadata", false},
		{"old", "old", false},
		{"x/old.rm", "x/old.rm", false},
	} {
		got, ok := domain.RenameRecord(tc.name, "old", "new")
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %q %v", tc.name, got, ok)
		}
	}
	if _, ok := domain.RenameRecord("x.metadata", "", "new"); ok {
		t.Fatal("an empty identifier renamed")
	}
}

func FuzzRenameRecord(f *testing.F) {
	for _, s := range []string{"a1.metadata", "a1/p.rm", "a1", "a10.content", ".x"} {
		f.Add(s, "a1", "b2")
	}
	f.Fuzz(func(t *testing.T, name, from, to string) {
		got, ok := domain.RenameRecord(name, domain.ItemID(from), domain.ItemID(to))
		if !ok {
			if got != name {
				t.Fatalf("an unrenamed %q changed to %q", name, got)
			}
			return
		}
		// A renamed record is to's, with exactly the suffix from had.
		suffix := strings.TrimPrefix(name, from)
		if got != to+suffix || (suffix[0] != '.' && suffix[0] != '/') {
			t.Fatalf("%q from %q to %q gave %q", name, from, to, got)
		}
		back, ok := domain.RenameRecord(got, domain.ItemID(to), domain.ItemID(from))
		if to != "" && (!ok || back != name) {
			t.Fatalf("renaming %q back gave %q %v", got, back, ok)
		}
	})
}

func FuzzGroupArchive(f *testing.F) {
	f.Add("a.metadata,a/p.rm,b.content", "a,a,b")
	f.Fuzz(func(t *testing.T, names, ids string) {
		var entries []domain.ArchiveEntry
		idList := strings.Split(ids, ",")
		for i, n := range strings.Split(names, ",") {
			entries = append(entries, domain.ArchiveEntry{Name: n, DocumentID: domain.ItemID(idList[i%len(idList)])})
		}
		total := 0
		seen := map[domain.ItemID]bool{}
		for _, g := range domain.GroupArchive(entries) {
			if seen[g.ID] {
				t.Fatalf("document %q grouped twice", g.ID)
			}
			seen[g.ID] = true
			for _, e := range g.Entries {
				if e.DocumentID != g.ID {
					t.Fatalf("%q in group %q", e.Name, g.ID)
				}
			}
			total += len(g.Entries)
		}
		if total != len(entries) {
			t.Fatalf("%d entries grouped, %d given", total, len(entries))
		}
	})
}

func TestImportedUnder(t *testing.T) {
	t.Parallel()
	m, err := domain.ParseMetadata([]byte(`{"visibleName":"A","type":"DocumentType","parent":"trash","deleted":true,"version":3,"lastModified":"5"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := string(m.ImportedUnder("f1").Bytes())
	if want := `{"deleted":false,"lastModified":"5","parent":"f1","type":"DocumentType","version":3,"visibleName":"A"}`; got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	plain, err := domain.ParseMetadata([]byte(`{"visibleName":"A","type":"DocumentType"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(plain.ImportedUnder("").Bytes()); got != `{"parent":"","type":"DocumentType","visibleName":"A"}` {
		t.Fatalf("a deleted flag was added: %s", got)
	}
}
