// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestPagesDouble(t *testing.T) {
	t.Parallel()
	p := porttest.NewPages()
	data := porttest.PageBytes([]domain.Paragraph{{Text: "hi", Style: domain.StylePlain}},
		[]domain.Highlight{{ID: "1:1", Text: "x", HasText: true}, {ID: "1:2"}}, 3)
	page, err := p.ReadPage(data)
	if err != nil || len(page.Paragraphs) != 1 || len(page.Highlights) != 2 || page.Stats.Strokes != 3 {
		t.Fatalf("%+v %v", page, err)
	}
	ids, err := p.Highlights(data)
	if err != nil || len(ids) != 2 {
		t.Fatalf("%v %v", ids, err)
	}
	edited, err := p.Erase(data, []string{"1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := p.ReadPage(edited); len(again.Highlights) != 1 || again.Highlights[0].ID != "1:2" {
		t.Fatalf("%+v", again)
	}
	if _, err := p.Erase(data, []string{"9:9"}); err == nil {
		t.Fatal("an unknown item was erased")
	}
	written, err := p.WritePage(domain.PageDraft{Paragraphs: []domain.Paragraph{{Text: "w"}}})
	if err != nil || len(p.Drafts) != 1 {
		t.Fatalf("%v %v", err, p.Drafts)
	}
	if w, _ := p.ReadPage(written); w.Paragraphs[0].Text != "w" {
		t.Fatalf("%+v", w)
	}
	if p.Count("ReadPage") != 3 {
		t.Fatalf("%d reads", p.Count("ReadPage"))
	}
	for _, bad := range []func() error{
		func() error { _, err := p.ReadPage([]byte("junk")); return err },
		func() error { _, err := p.Highlights([]byte("junk")); return err },
		func() error { _, err := p.Erase([]byte("junk"), nil); return err },
	} {
		if bad() == nil {
			t.Error("junk was read as a page")
		}
	}
	boom := errors.New("boom")
	for _, m := range []string{"ReadPage", "Highlights", "Erase", "WritePage"} {
		p.Fail[m] = boom
	}
	if _, err := p.ReadPage(data); !errors.Is(err, boom) {
		t.Fatal("ReadPage")
	}
	if _, err := p.Highlights(data); !errors.Is(err, boom) {
		t.Fatal("Highlights")
	}
	if _, err := p.Erase(data, nil); !errors.Is(err, boom) {
		t.Fatal("Erase")
	}
	if _, err := p.WritePage(domain.PageDraft{}); !errors.Is(err, boom) {
		t.Fatal("WritePage")
	}
}

func TestLibraryBuilders(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	c.AddLarge(2, 3)
	if n := len(c.Records()); n != 8 {
		t.Fatalf("%d records", n)
	}
	if porttest.PageID(0) != "p1" {
		t.Fatal(porttest.PageID(0))
	}
	page := porttest.PageBytes(nil, nil, 0)
	recs := c.Add(
		porttest.Doc{ID: "nb", Name: "Notebook", Pages: [][]byte{page, page}},
		porttest.Doc{ID: "pdf", Name: "Paper", Ext: "pdf", Pages: [][]byte{page}, Tags: []string{"t"}},
		porttest.Doc{ID: "ep", Name: "Book", Ext: "epub"},
	)
	for _, rec := range recs {
		files, err := c.Files(context.Background(), rec)
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: %v %v", rec.ID, files, err)
		}
	}
}
