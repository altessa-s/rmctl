// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/browse"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/pages"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// newBrowse is the browse use case over c, its pages read by the Pages double.
func newBrowse(c *porttest.Cloud) *browse.Service {
	return newBrowseWith(c, porttest.NewPages())
}

func newBrowseWith(c *porttest.Cloud, p *porttest.Pages) *browse.Service {
	lib := library.New(c, c, c)
	return browse.New(lib, pages.NewReader(lib, p))
}

func TestFindText(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "n1", Name: "Diary", Pages: [][]byte{
			porttest.PageBytes([]domain.Paragraph{{Text: "Monday"}}, nil, 0),
			porttest.PageBytes([]domain.Paragraph{{Text: "The Quick brown fox"}}, nil, 0),
		}},
		porttest.Doc{ID: "p1", Name: "Paper", Ext: "pdf", Pages: [][]byte{
			porttest.PageBytes(nil, []domain.Highlight{{ID: "1:1", Text: "a quick result", HasText: true}}, 0),
		}},
		porttest.Doc{ID: "p2", Name: "Plain", Ext: "pdf"},
		porttest.Doc{ID: "n2", Name: "Other", Pages: [][]byte{porttest.PageBytes([]domain.Paragraph{{Text: "slow"}}, nil, 0)}},
	)
	p := porttest.NewPages()
	s := newBrowseWith(c, p)
	_, items, err := s.Find(t.Context(), port.FindQuery{Text: "QUICK"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(items); !slices.Equal(got, []domain.ItemID{"n1", "p1"}) {
		t.Fatalf("%v", got)
	}
	// Only the documents with pages are read, page by page.
	if p.Count("ReadPage") != 4 {
		t.Fatalf("%d pages read", p.Count("ReadPage"))
	}
	// The other filters run first: a pattern that excludes a document keeps
	// its pages unread.
	before := p.Count("ReadPage")
	if _, items, err = s.Find(t.Context(), port.FindQuery{Text: "quick", Pattern: "^/Paper$"}); err != nil || !slices.Equal(ids(items), []domain.ItemID{"p1"}) {
		t.Fatalf("%v %v", ids(items), err)
	}
	if p.Count("ReadPage") != before+1 {
		t.Fatalf("read %d pages for one document", p.Count("ReadPage")-before)
	}
}

func TestFindTextFails(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	c.Add(porttest.Doc{ID: "n1", Name: "Diary", Pages: [][]byte{porttest.PageBytes(nil, nil, 0)}})
	p := porttest.NewPages()
	p.Fail["ReadPage"] = domain.ErrPagesUnsupported
	if _, _, err := newBrowseWith(c, p).Find(t.Context(), port.FindQuery{Text: "x"}); !errors.Is(err, domain.ErrPagesUnsupported) {
		t.Fatalf("%v", err)
	}
}

func TestNewNeedsPages(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	browse.New(library.New(porttest.NewCloud(), porttest.NewCloud(), porttest.NewCloud()), nil)
}
