// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestUsage(t *testing.T) {
	t.Parallel()
	s, c := service(t)
	_, u, err := s.Usage(t.Context(), "/", 1, false)
	if err != nil || u.Total.Documents != 2 || u.Total.Folders != 2 || len(u.Folders) != 1 || u.Folders[0].Path != "/Books" {
		t.Fatalf("%+v %v", u, err)
	}
	if _, all, _ := s.Usage(t.Context(), "", 1, true); all.Total.Documents != 4 || all.Total.Bytes <= u.Total.Bytes {
		t.Fatalf("%+v", all.Total)
	}
	if _, books, err := s.Usage(t.Context(), "/Books", -1, false); err != nil || books.Total.Path != "/Books" || len(books.Folders) != 0 {
		t.Fatalf("%+v %v", books, err)
	}
	if _, _, err := s.Usage(t.Context(), "/Books/Paper", 1, false); !errors.Is(err, domain.ErrNotAFolder) {
		t.Fatal(err)
	}
	c.Fail = map[string]error{"Root": errBoomBrowse}
	if _, _, err := s.Usage(t.Context(), "/", 1, false); !errors.Is(err, errBoomBrowse) {
		t.Fatal(err)
	}
}

func TestDuplicates(t *testing.T) {
	t.Parallel()
	s, c := service(t)
	body := []byte("%PDF the same file")
	c.Add(
		porttest.Doc{ID: "d5aaaaaa", Name: "Copy", Parent: "f1aaaaaa", Ext: "pdf", Body: body},
		porttest.Doc{ID: "d6aaaaaa", Name: "Copy again", Ext: "pdf", Body: body},
		porttest.Doc{ID: "d7aaaaaa", Name: "Notes"},
	)
	_, groups, err := s.Duplicates(t.Context(), "/", false)
	if err != nil || len(groups) != 1 || len(groups[0].Items) != 2 || groups[0].Items[0].ID != "d5aaaaaa" {
		t.Fatalf("%+v %v", groups, err)
	}
	if _, groups, _ := s.Duplicates(t.Context(), "/Books", false); len(groups) != 0 {
		t.Fatalf("%+v", groups)
	}
	if _, _, err := s.Duplicates(t.Context(), "/Books/Paper", false); !errors.Is(err, domain.ErrNotAFolder) {
		t.Fatal(err)
	}
	if _, _, err := s.Duplicates(t.Context(), "/Nothing/Here", false); err == nil {
		t.Fatal("a missing folder was searched")
	}
	c.Fail = map[string]error{"Root": errBoomBrowse}
	if _, _, err := s.Duplicates(t.Context(), "/", false); !errors.Is(err, errBoomBrowse) {
		t.Fatal(err)
	}
}

var errBoomBrowse = errors.New("boom")
