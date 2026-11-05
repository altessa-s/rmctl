// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
)

// dupesLibrary is the shared library with two more copies of Paper's PDF:
// one in /Books, one at the top level.
func dupesLibrary(t *testing.T) *domain.Tree {
	t.Helper()
	items := clitest.Library().Items()
	paper, _ := clitest.Library().Get(clitest.PaperID)
	pdf, _ := paper.Original()
	for _, c := range []struct{ id, name, parent string }{{"d-copy", "Paper copy", clitest.BooksID}, {"d-top", "Paper at the top", ""}} {
		meta, err := domain.ParseMetadata(fmt.Appendf(nil, `{"visibleName":%q,"type":"DocumentType","parent":%q}`, c.name, c.parent))
		if err != nil {
			t.Fatal(err)
		}
		files := []domain.File{{Name: c.id + ".metadata", Hash: "m-" + c.id, Size: 50}, {Name: c.id + ".pdf", Hash: pdf.Hash, Size: pdf.Size}}
		items = append(items, domain.NewItem(domain.Record{ID: domain.ItemID(c.id)}, files, meta, nil))
	}
	return domain.NewTree(1, items)
}

func TestDu(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, "du")
	want := "path\t/\nbytes\t6535\ndocuments\t3\nfolders\t1\n\n" +
		"/Books\t6363\t2\t0\tf-books\n\n" +
		"epub\t1\t4205\npdf\t1\t2158\nnotebook\t1\t172\n"
	if r.Code != 0 || r.Out != want || f.Calls()[0] != "Usage / 1" {
		t.Fatalf("%d %v %q %v", r.Code, r.Error, r.Out, f.Calls())
	}
	if r := run(t, f, "du", "f-books", "--depth", "0", "--all"); r.Code != 0 || f.Calls()[1] != "Usage f-books 0 all" || strings.Contains(r.Out, "\n/Books\t") {
		t.Fatalf("%q %v", r.Out, f.Calls())
	}
	j := run(t, &clitest.Fake{}, "du", "--json")
	clitest.Golden(t, "du-json", j.Out)
	tbl := clitest.Exec(t, (&clitest.Fake{}).Services(), clitest.Options{OutTerminal: true}, Commands, "du", "--all", "--color", "never")
	clitest.Golden(t, "du-table", tbl.Out)
	if r := run(t, f, "du", "--depth", "-1"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%d", r.Code)
	}
	if r := run(t, &clitest.Fake{Err: errors.New("boom")}, "du"); r.Code != cliutil.CodeError || !strings.HasPrefix(r.Error.Error(), "du: ") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestDupes(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Library: dupesLibrary(t)}
	r := run(t, f, "dupes")
	want := "h-pdf-d-paper\t2048\t/Books/Paper\tpdf\td-paper\n" +
		"h-pdf-d-paper\t2048\t/Books/Paper copy\tpdf\td-copy\n" +
		"h-pdf-d-paper\t2048\t/Paper at the top\tpdf\td-top\n\n" +
		"groups\t1\nwasted\t4096\n"
	if r.Code != 0 || r.Out != want {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Out)
	}
	j := run(t, &clitest.Fake{Library: dupesLibrary(t)}, "dupes", "--json")
	clitest.Golden(t, "dupes-json", j.Out)
	tbl := clitest.Exec(t, (&clitest.Fake{Library: dupesLibrary(t)}).Services(), clitest.Options{OutTerminal: true}, Commands, "dupes", "--color", "never")
	clitest.Golden(t, "dupes-table", tbl.Out)
	none := run(t, &clitest.Fake{}, "dupes", "f-books", "--all")
	if none.Code != 0 || none.Out != "groups\t0\nwasted\t0\n" || !strings.Contains(none.Err, "no two documents share an original") {
		t.Fatalf("%q %q", none.Out, none.Err)
	}
	if r := run(t, &clitest.Fake{Err: errors.New("boom")}, "dupes"); r.Code != cliutil.CodeError || !strings.HasPrefix(r.Error.Error(), "dupes: ") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}
