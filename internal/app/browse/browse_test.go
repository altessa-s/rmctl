// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/browse"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func service(t testing.TB) (*browse.Service, *porttest.Cloud) {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "f1aaaaaa", Name: "Books", Folder: true},
		porttest.Doc{ID: "f2aaaaaa", Name: "Fiction", Folder: true, Parent: "f1aaaaaa"},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Ext: "pdf", Tags: []string{"work", "draft"}, Pinned: true},
		porttest.Doc{ID: "d2aaaaaa", Name: "Novel", Parent: "f2aaaaaa", Ext: "epub", Tags: []string{"work"}},
		porttest.Doc{ID: "d3aaaaaa", Name: "Old", Ext: "pdf", Trashed: true},
		porttest.Doc{ID: "f3aaaaaa", Name: "Gone", Folder: true, Trashed: true},
		porttest.Doc{ID: "d4aaaaaa", Name: "Inside gone", Parent: "f3aaaaaa", Ext: "pdf"},
	)
	return newBrowse(c), c
}

func ids(items []domain.Item) []domain.ItemID {
	out := make([]domain.ItemID, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func TestList(t *testing.T) {
	t.Parallel()
	s, _ := service(t)
	for _, tc := range []struct {
		query string
		all   bool
		want  []domain.ItemID
		err   error
	}{
		{query: "", want: []domain.ItemID{"f1aaaaaa"}},
		{query: "/", all: true, want: []domain.ItemID{"f1aaaaaa", "f3aaaaaa", "d3aaaaaa"}},
		{query: "/Books", want: []domain.ItemID{"f2aaaaaa", "d1aaaaaa"}},
		{query: "f2", want: []domain.ItemID{"d2aaaaaa"}},
		{query: "/Books/Paper", want: []domain.ItemID{"d1aaaaaa"}},
		{query: "/Nope", err: domain.ErrNotFound},
		{query: "/Gone", all: true, want: []domain.ItemID{"d4aaaaaa"}},
	} {
		_, items, err := s.List(t.Context(), tc.query, tc.all)
		if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
			t.Errorf("%q: %v", tc.query, err)
			continue
		}
		if got := ids(items); len(got) != len(tc.want) || (len(got) > 0 && got[0] != tc.want[0]) {
			t.Errorf("%q all=%v: %v, want %v", tc.query, tc.all, got, tc.want)
		}
	}
}

func TestStat(t *testing.T) {
	t.Parallel()
	s, _ := service(t)
	if _, it, err := s.Stat(t.Context(), "d3"); err != nil || it.Name != "Old" {
		t.Fatalf("%v %+v", err, it)
	}
	if _, _, err := s.Stat(t.Context(), "/"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, _, err := s.Stat(t.Context(), "/Missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	var amb *domain.AmbiguousError
	if _, _, err := s.Stat(t.Context(), "d"); !errors.As(err, &amb) {
		t.Fatalf("got %v", err)
	}
}

func TestFind(t *testing.T) {
	t.Parallel()
	s, _ := service(t)
	for _, tc := range []struct {
		name string
		q    port.FindQuery
		want int
		err  bool
	}{
		{name: "everything", q: port.FindQuery{}, want: 4},
		{name: "everything with trash", q: port.FindQuery{All: true}, want: 7},
		{name: "pattern", q: port.FindQuery{Pattern: "(?i)novel$"}, want: 1},
		{name: "tags all of", q: port.FindQuery{Tags: []string{"work", "draft"}}, want: 1},
		{name: "tag", q: port.FindQuery{Tags: []string{"work"}}, want: 2},
		{name: "starred", q: port.FindQuery{Starred: true}, want: 1},
		{name: "under", q: port.FindQuery{Under: "/Books/Fiction"}, want: 1},
		{name: "under a document", q: port.FindQuery{Under: "/Books/Paper"}, err: true},
		{name: "bad pattern", q: port.FindQuery{Pattern: "("}, err: true},
	} {
		_, items, err := s.Find(t.Context(), tc.q)
		if (err != nil) != tc.err || len(items) != tc.want {
			t.Errorf("%s: %v, %v", tc.name, err, ids(items))
		}
	}
}

func TestFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	s, c := service(t)
	c.Fail["Root"] = boom
	if _, _, err := s.List(t.Context(), "", false); !errors.Is(err, boom) {
		t.Fatalf("list %v", err)
	}
	if _, _, err := s.Stat(t.Context(), "x"); !errors.Is(err, boom) {
		t.Fatalf("stat %v", err)
	}
	if _, _, err := s.Find(t.Context(), port.FindQuery{}); !errors.Is(err, boom) {
		t.Fatalf("find %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	browse.New(nil, nil)
}

// FuzzList holds any query to three outcomes: a listing, a not-found, or a
// named failure of the library model. It never panics.
func FuzzList(f *testing.F) {
	for _, q := range []string{"", "/", "/Books", "f1", "d", "/Books/Paper/x", "//", "/../Books"} {
		f.Add(q)
	}
	s, _ := service(f)
	f.Fuzz(func(t *testing.T, q string) {
		_, _, err := s.List(t.Context(), q, true)
		var amb *domain.AmbiguousError
		if err != nil && !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrNotAFolder) && !errors.As(err, &amb) {
			t.Fatalf("%q: %v", q, err)
		}
	})
}
