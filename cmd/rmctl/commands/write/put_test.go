// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestPutSplitsFilesFromTheFolder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args  []string
		calls []string
	}{
		{[]string{"a.pdf"}, []string{"Put a.pdf /"}},
		{[]string{"a.pdf", "/Papers"}, []string{"Put a.pdf /Papers"}},
		{[]string{"a.pdf", "B.EPUB", "f-books"}, []string{"Put a.pdf f-books", "Put B.EPUB f-books"}},
		{[]string{"a.pdf", "b.pdf"}, []string{"Put a.pdf /", "Put b.pdf /"}},
		{[]string{"a.pdf", "x.pdf", "--to", "/A folder.pdf"}, []string{"Put a.pdf /A folder.pdf", "Put x.pdf /A folder.pdf"}},
	} {
		f := &clitest.Fake{}
		r := run(t, f, clitest.Options{}, append([]string{"put", "--yes"}, tc.args...)...)
		if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), tc.calls) {
			t.Fatalf("%v: %d %v %q", tc.args, r.Code, r.Error, f.Calls())
		}
	}
}

func TestPutOptions(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{ReplaceOld: domain.Item{ID: "d-old", Name: "N"}}
	r := run(t, f, clitest.Options{}, "put", "a.pdf", "/P", "--name", "N", "--tag", "x", "--cover", "first", "--replace", "--yes")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"Put a.pdf /P N x first replace"}) {
		t.Fatalf("%d %v %q", r.Code, r.Error, f.Calls())
	}
	if !strings.Contains(r.Err, "a.pdf replaces N (d-old); its annotations are dropped") {
		t.Fatalf("%q", r.Err)
	}
	// Nothing of that name: the file is added, and nothing is said to be lost.
	added := &clitest.Fake{}
	if r := run(t, added, clitest.Options{}, "put", "a.pdf", "/P", "--replace", "--yes"); r.Code != cliutil.CodeOK || strings.Contains(r.Err, "annotations") {
		t.Fatalf("%d %q", r.Code, r.Err)
	}
	dry := &clitest.Fake{ReplaceOld: domain.Item{ID: "d-old", Name: "N"}}
	if r := run(t, dry, clitest.Options{}, "put", "a.pdf", "/P", "--replace", "--dry-run"); !strings.Contains(r.Err, "would replace N") {
		t.Fatalf("%q", r.Err)
	}
	if !strings.Contains(r.Out, "Uploaded") {
		t.Fatalf("%q", r.Out)
	}
}

// One file that fails does not stop the others; the command fails at the end.
func TestPutContinuesPastAFailure(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{PutErr: map[string]error{"b.pdf": errors.New("locked PDF")}}
	r := run(t, f, clitest.Options{}, "put", "a.pdf", "b.pdf", "c.pdf", "/P", "--yes")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, ErrSomeFailed) || r.Error.Error() != "put: 1 of 3 files: some files were not uploaded" {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if len(f.Calls()) != 3 || !strings.Contains(r.Err, "put b.pdf: locked PDF") || strings.Count(r.Out, "Uploaded") != 2 {
		t.Fatalf("calls %v\nout %q\nerr %q", f.Calls(), r.Out, r.Err)
	}
	j := run(t, &clitest.Fake{PutErr: map[string]error{"b.pdf": errors.New("locked PDF")}}, clitest.Options{}, "put", "a.pdf", "b.pdf", "--yes", "--json")
	clitest.Golden(t, "put-json", j.Out)
}

// A single file's failure, and a failure that fails every file, keep their
// own exit status.
func TestPutKeepsTheExitStatus(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{Err: port.ErrRootConflict}, clitest.Options{}, "put", "a.pdf", "--yes")
	if r.Code != cliutil.CodeConflict || r.Error.Error() != "put: "+port.ErrRootConflict.Error() {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	f := &clitest.Fake{Err: port.ErrNotRegistered}
	r = run(t, f, clitest.Options{}, "put", "a.pdf", "b.pdf", "--yes")
	if r.Code != cliutil.CodeNotRegistered || len(f.Calls()) != 1 {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
}

func TestPutDryRun(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	// No --yes and no terminal: a dry run needs neither.
	r := run(t, f, clitest.Options{}, "put", "a.pdf", "/P", "--dry-run")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"Put a.pdf /P     dry-run"}) ||
		r.Out != "would upload\tUploaded\tpdf\td-new\t-\ta.pdf\t/P\n" {
		t.Fatalf("%d %v %q %q", r.Code, r.Error, f.Calls(), r.Out)
	}
	// A notebook dry run asks the use case to check the file and the folder.
	n := &clitest.Fake{}
	r = run(t, n, clitest.Options{}, "put", "notes.md", "/N", "--notebook", "--dry-run")
	if r.Code != cliutil.CodeOK || !slices.Equal(n.Calls(), []string{"PlanNotebook notes.md /N  rm2"}) ||
		!strings.HasPrefix(r.Out, "would make\tNotes\tnotebook\t") {
		t.Fatalf("%d %v %q %q", r.Code, r.Error, n.Calls(), r.Out)
	}
	// What would fail the upload fails the dry run.
	bad := &clitest.Fake{PutErr: map[string]error{"notes.md": errors.New("a table on line 3")}}
	if r := run(t, bad, clitest.Options{}, "put", "notes.md", "/N", "--notebook", "--dry-run"); r.Code != cliutil.CodeError ||
		!strings.Contains(r.Error.Error(), "line 3") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestPutNotebook(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, clitest.Options{}, "put", "notes.md", "plan.txt", "/Notes", "--notebook", "--device", "move", "--tag", "x", "--yes")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"Notebook notes.md /Notes  move x", "Notebook plan.txt /Notes  move x"}) {
		t.Fatalf("%d %v %q", r.Code, r.Error, f.Calls())
	}
	r = run(t, &clitest.Fake{PagesErr: domain.ErrPagesUnsupported}, clitest.Options{}, "put", "notes.md", "--notebook", "--yes")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, cliutil.ErrNoPages) {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	r = run(t, &clitest.Fake{PutErr: map[string]error{"a.md": errors.New("too long")}}, clitest.Options{}, "put", "a.md", "b.md", "--notebook", "--yes")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Err, "put a.md: too long") {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Err)
	}
}

func TestPutRefusesBeforeSending(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"put", "notes.md"},                   // text without --notebook
		{"put", "a.pdf", "--notebook"},        // a PDF with --notebook
		{"put", "a.docx"},                     // not uploadable
		{"put", "a.pdf", "--cover", "middle"}, // unknown cover
		{"put", "a.md", "--notebook", "--device", "kindle"},
		{"put", "a.pdf", "b.pdf", "--name", "One"}, // --name for several files
		{"put", "a.md", "--notebook", "--replace"}, // --replace is for documents
		{"put", "a.md", "--notebook", "--cover", "first"},
		{"put", "a.pdf"}, // no --yes off a terminal
	} {
		f := &clitest.Fake{}
		r := run(t, f, clitest.Options{}, args...)
		if r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
			t.Fatalf("%v: %d %v %v", args, r.Code, r.Error, f.Calls())
		}
	}
	r := run(t, &clitest.Fake{}, clitest.Options{Terminal: true, Stdin: "y\n"}, "put", "a.pdf", "b.pdf", "/P", "--replace")
	if !strings.Contains(r.Err, "Upload 2 files into /P, replacing documents of the same name? [y/N]") {
		t.Fatalf("%q", r.Err)
	}
	r = run(t, &clitest.Fake{}, clitest.Options{Terminal: true, Stdin: "n\n"}, "put", "a.md", "--notebook")
	if !strings.Contains(r.Err, "Make a.md into notebooks in /? [y/N]") || !errors.Is(r.Error, cliutil.ErrDeclined) {
		t.Fatalf("%q %v", r.Err, r.Error)
	}
	if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, "put", "a.pdf", "--yes"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
	if r := clitest.Exec(t, cliutil.Services{Writer: &clitest.Fake{}}, clitest.Options{}, Commands, "put", "a.md", "--notebook", "--yes"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}

func TestPutStopsOnInterrupt(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cliutil.ErrInterrupted)
	f := &clitest.Fake{}
	r := clitest.Exec(t, f.Services(), clitest.Options{Context: ctx}, Commands, "put", "a.pdf", "b.pdf", "--yes")
	if r.Code != cliutil.CodeInterrupted || len(f.Calls()) != 0 {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
}

func marked() port.DocumentText {
	return port.DocumentText{
		Item: clitest.Item(clitest.Spec{ID: "d-paper", Name: "Paper", Kind: domain.KindPDF}),
		Pages: []domain.Page{
			// Two highlights; a page of marker strokes alone; one highlight
			// and two marker strokes.
			{ID: "p1", Number: 1, Highlights: []domain.Highlight{{ID: "1:1"}, {ID: "1:2"}}, Stats: domain.PageStats{Marks: 2}},
			{ID: "p2", Number: 2, Stats: domain.PageStats{Marks: 1}},
			{ID: "p3", Number: 3, Highlights: []domain.Highlight{{ID: "1:3"}}, Stats: domain.PageStats{Marks: 3}},
		},
	}
}

func TestUnmark(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Document: marked(), Unmarked: port.UnmarkReport{Item: marked().Item, Erased: 6, Highlights: 3, Strokes: 3, Pages: 3}}
	r := run(t, f, clitest.Options{Terminal: true, Stdin: "n\n"}, "unmark", "/Paper", "--page", "1,3")
	if !strings.Contains(r.Err, "Erase 6 marks (3 highlights, 3 marker strokes) on 3 pages of Paper? [y/N]") || !errors.Is(r.Error, cliutil.ErrDeclined) ||
		!slices.Equal(f.Calls(), []string{"Pages /Paper 1 3"}) {
		t.Fatalf("%q %v %v", r.Err, r.Error, f.Calls())
	}
	f = &clitest.Fake{Document: marked(), Unmarked: port.UnmarkReport{Item: marked().Item, Erased: 6, Highlights: 3, Strokes: 3, Pages: 3}}
	r = run(t, f, clitest.Options{}, "unmark", "/Paper", "--yes")
	if r.Code != cliutil.CodeOK || r.Out != "id\td-paper\nname\tPaper\nerased\t6\nhighlights\t3\nstrokes\t3\npages\t3\n" ||
		!slices.Equal(f.Calls(), []string{"Pages /Paper", "Unmark /Paper"}) {
		t.Fatalf("%d %v %q %v", r.Code, r.Error, r.Out, f.Calls())
	}
	j := run(t, &clitest.Fake{Document: marked(), Unmarked: port.UnmarkReport{Item: marked().Item, Erased: 6, Highlights: 3, Strokes: 3, Pages: 3}},
		clitest.Options{}, "unmark", "d-paper", "--yes", "--json")
	clitest.Golden(t, "unmark-json", j.Out)
}

func TestUnmarkNothingToErase(t *testing.T) {
	t.Parallel()
	doc := marked()
	doc.Pages = []domain.Page{{ID: "p9", Number: 9}}
	f := &clitest.Fake{Document: doc}
	// Nothing to erase asks nothing and changes nothing, even without --yes.
	r := run(t, f, clitest.Options{}, "unmark", "/Paper")
	if r.Code != cliutil.CodeOK || r.Err != "Paper has no highlighter marks; nothing to erase\n" || len(f.Calls()) != 1 {
		t.Fatalf("%d %v %q %v", r.Code, r.Error, r.Err, f.Calls())
	}
}

func TestUnmarkErrors(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{PagesErr: domain.ErrPagesUnsupported}, clitest.Options{}, "unmark", "d", "--yes")
	if !errors.Is(r.Error, cliutil.ErrNoPages) || r.Code != cliutil.CodeError {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	r = run(t, &clitest.Fake{Err: port.ErrRootConflict}, clitest.Options{}, "unmark", "d", "--yes")
	if r.Code != cliutil.CodeConflict {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	// The count is read, then the erase fails.
	failing := &failingUnmark{Document: marked()}
	r = clitest.Exec(t, failing.services(), clitest.Options{}, Commands, "unmark", "d", "--yes")
	if r.Code != cliutil.CodeError || r.Error.Error() != "unmark: erase refused" {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r = run(t, &clitest.Fake{}, clitest.Options{}, "unmark"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%d", r.Code)
	}
	if r = clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, "unmark", "d", "--yes"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}

// failingUnmark counts like the fake and refuses the erase.
type failingUnmark struct {
	clitest.Fake
}

func (f *failingUnmark) Unmark(context.Context, string, []string) (port.UnmarkReport, error) {
	return port.UnmarkReport{}, errors.New("erase refused")
}

func (f *failingUnmark) services() cliutil.Services {
	s := f.Fake.Services()
	s.Pages = f
	return s
}

func contentPlanFake() *clitest.Fake {
	return &clitest.Fake{ContentPlan: &port.ContentPlan{
		Document: clitest.Item(clitest.Spec{ID: "d-paper", Name: "Paper", Kind: domain.KindPDF}),
		OldPages: 3, NewPages: 2, OldOriginal: 3, NewOriginal: 2, Kept: 2, Dropped: 1, Annotated: 2,
		Lost: []port.AnnotatedPage{{Number: 3, ID: "p3", Files: 1}},
	}}
}

func TestPutContentOnly(t *testing.T) {
	t.Parallel()
	f := contentPlanFake()
	r := run(t, f, clitest.Options{Terminal: true, Stdin: "n\n"}, "put", "Paper.pdf", "/Books", "--content-only", "--drop-annotations")
	if !errors.Is(r.Error, cliutil.ErrDeclined) || !strings.Contains(r.Err, "Replace the PDF of /Books/Paper, keeping its annotations on 2 pages? [y/N]") {
		t.Fatalf("%v %q", r.Error, r.Err)
	}
	if calls := f.Calls(); len(calls) != 1 || !strings.Contains(calls[0], "dry-run content-only drop") {
		t.Fatalf("the plan before the question: %v", calls)
	}
	f = contentPlanFake()
	r = run(t, f, clitest.Options{}, "put", "Paper.pdf", "/Books", "--content-only", "--drop-annotations", "--current-page", "2", "--yes", "--json")
	if r.Code != 0 || !strings.Contains(r.Out, `"content_plan": {`) || !strings.Contains(r.Out, `"annotated_dropped": [`) ||
		!strings.Contains(r.Out, `"page": 3`) || !strings.Contains(r.Err, "page 3 (p3) is dropped with 1 annotation files") {
		t.Fatalf("%d %v\n%s\n%s", r.Code, r.Error, r.Out, r.Err)
	}
	if calls := f.Calls(); !strings.HasSuffix(calls[len(calls)-1], " content-only drop page=2") {
		t.Fatalf("%v", calls)
	}
	r = run(t, contentPlanFake(), clitest.Options{}, "put", "Paper.pdf", "/Books", "--content-only", "--yes")
	if r.Code != 0 || !strings.HasPrefix(r.Out, "replaced content\t") || !strings.Contains(r.Err, "Paper: 3 pages (a PDF of 3) become 2 (a PDF of 2): keeps 2") {
		t.Fatalf("%q %q", r.Out, r.Err)
	}
	r = run(t, contentPlanFake(), clitest.Options{}, "put", "Paper.pdf", "/Books", "--content-only", "--dry-run")
	if r.Code != 0 || !strings.HasPrefix(r.Out, "would replace content\t") || !strings.Contains(r.Err, "would keep 2") {
		t.Fatalf("%q %q", r.Out, r.Err)
	}
	// A plan that fails is the command's failure, with the plan told.
	failing := contentPlanFake()
	failing.PutErr = map[string]error{"Paper.pdf": domain.ErrAnnotationsDropped}
	r = run(t, failing, clitest.Options{}, "put", "Paper.pdf", "/Books", "--content-only", "--yes")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, domain.ErrAnnotationsDropped) || !strings.Contains(r.Err, "would keep") {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Err)
	}
}

func TestPutPDFOnlyFlagRefusals(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"put", "a.epub", "--content-only"},
		{"put", "a.rmdoc", "--content-only"},
		{"put", "a.pdf", "--content-only", "--replace"},
		{"put", "a.md", "--content-only", "--notebook"},
		{"put", "a.pdf", "--content-only", "--tag", "x"},
		{"put", "a.pdf", "--content-only", "--cover", "first"},
		{"put", "a.pdf", "b.pdf", "--content-only"},
		{"put", "a.pdf", "--drop-annotations"},
		{"put", "a.pdf", "--current-page", "-1"},
		{"put", "a.epub", "--current-page", "2"},
		{"put", "a.md", "--notebook", "--current-page", "2"},
	} {
		f := &clitest.Fake{}
		if r := run(t, f, clitest.Options{}, append(args, "--yes")...); r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
			t.Errorf("%v: %d %v %v", args, r.Code, r.Error, f.Calls())
		}
	}
	f := &clitest.Fake{}
	if r := run(t, f, clitest.Options{}, "put", "a.pdf", "--current-page", "2", "--yes"); r.Code != 0 || !strings.HasSuffix(f.Calls()[0], "page=2") {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
}
