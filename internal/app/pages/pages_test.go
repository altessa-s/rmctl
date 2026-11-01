// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package pages_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/pages"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// hookRoots runs before on the second Root call: between a use case's first
// look at the library and its commit.
type hookRoots struct {
	*porttest.Cloud
	mu     sync.Mutex
	n      int
	before func()
}

func (h *hookRoots) Root(ctx context.Context) (port.RootState, error) {
	h.mu.Lock()
	h.n++
	fire := h.n == 2 && h.before != nil
	h.mu.Unlock()
	if fire {
		h.before()
	}
	return h.Cloud.Root(ctx)
}

type fixture struct {
	cloud *porttest.Cloud
	roots *hookRoots
	pages *porttest.Pages
	files *porttest.MemFiles
	svc   *pages.Service
}

func newFixture(t testing.TB, docs ...porttest.Doc) *fixture {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(docs...)
	p := porttest.NewPages()
	files := porttest.NewMemFiles()
	roots := &hookRoots{Cloud: c}
	lib := library.New(roots, c, c)
	svc := pages.New(lib, pages.NewReader(lib, p), p, p, c, c, files, porttest.NewIDs("nb"), porttest.NewClock(porttest.Epoch))
	return &fixture{cloud: c, roots: roots, pages: p, files: files, svc: svc}
}

// without is the root without the record of id.
func without(records []domain.Record, id domain.ItemID) []domain.Record {
	return slices.DeleteFunc(slices.Clone(records), func(r domain.Record) bool { return r.ID == id })
}

func para(text string) []domain.Paragraph {
	return []domain.Paragraph{{Text: text, Style: domain.StylePlain}}
}

func marked(ids ...string) []byte {
	hs := make([]domain.Highlight, len(ids))
	for i, id := range ids {
		hs[i] = domain.Highlight{ID: id, Text: "text " + id, HasText: true}
	}
	return porttest.PageBytes(nil, hs, 0)
}

func library3() []porttest.Doc {
	return []porttest.Doc{
		{ID: "f1", Name: "Books", Folder: true},
		{ID: "n1", Name: "Diary", Pages: [][]byte{
			porttest.PageBytes(para("one"), nil, 0), porttest.PageBytes(para("two"), nil, 2), porttest.PageBytes(para("three"), nil, 0),
		}},
		{ID: "p1", Name: "Paper", Ext: "pdf", Parent: "f1", Pages: [][]byte{marked("1:1", "1:2"), porttest.PageBytes(para("note"), nil, 0), marked("1:3")}},
		{ID: "p2", Name: "Plain", Ext: "pdf"},
		{ID: "t1", Name: "Binned", Ext: "pdf", Trashed: true, Pages: [][]byte{marked("1:9")}},
	}
}

func texts(ps []domain.Page) []string {
	var out []string
	for _, p := range ps {
		for _, para := range p.Paragraphs {
			out = append(out, para.Text)
		}
	}
	return out
}

func TestPagesInOrder(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	got, err := f.svc.Pages(t.Context(), "/Diary", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Item.ID != "n1" || len(got.Pages) != 3 || !slices.Equal(texts(got.Pages), []string{"one", "two", "three"}) {
		t.Fatalf("%+v", got)
	}
	for i, p := range got.Pages {
		if p.Number != i+1 || p.ID != porttest.PageID(i) {
			t.Fatalf("page %d: %+v", i, p)
		}
	}
	if got.Pages[1].Stats.Strokes != 2 {
		t.Fatalf("%+v", got.Pages[1].Stats)
	}
}

func TestPagesSelected(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	got, err := f.svc.Pages(t.Context(), "n1", []string{"3", porttest.PageID(0)})
	if err != nil || !slices.Equal(texts(got.Pages), []string{"one", "three"}) {
		t.Fatalf("%v %v", texts(got.Pages), err)
	}
	for _, sel := range []string{"0", "4", "nope"} {
		if _, err := f.svc.Pages(t.Context(), "n1", []string{sel}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v", sel, err)
		}
	}
}

func TestPagesOfOtherDocuments(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	// A PDF without .rm pages has the pages its .content names, empty.
	got, err := f.svc.Pages(t.Context(), "/Plain", nil)
	if err != nil || len(got.Pages) != 1 || len(got.Pages[0].Paragraphs) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	// A document in the trash is still read.
	if got, err = f.svc.Pages(t.Context(), "t1", nil); err != nil || len(got.Pages[0].Highlights) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := f.svc.Pages(t.Context(), "/Books", nil); !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatalf("a folder: %v", err)
	}
	if _, err := f.svc.Pages(t.Context(), "/Nope", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := f.svc.Pages(t.Context(), "/", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the top level: %v", err)
	}
}

// Without a .content that names its pages, a document's .rm files are its
// pages, in index order.
func TestPagesWithoutOrder(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	body := porttest.PageBytes(para("loose"), nil, 0)
	page := c.Put("n9/page-a.rm", body)
	nested := c.Put("n9/page-a/attachment.rm", body)
	c.Add(porttest.Doc{ID: "n9", Name: "Loose", Ext: "epub", Extra: []domain.File{page, nested}})
	p := porttest.NewPages()
	lib := library.New(c, c, c)
	svc := pages.New(lib, pages.NewReader(lib, p), p, p, c, c, porttest.NewMemFiles(), porttest.NewIDs("x"), porttest.NewClock(porttest.Epoch))
	got, err := svc.Pages(t.Context(), "n9", nil)
	if err != nil || len(got.Pages) != 1 || got.Pages[0].ID != "page-a" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPagesFailures(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"Root", "Read"} {
		f := newFixture(t, library3()...)
		boom := errors.New(method + " failed")
		f.cloud.Fail[method] = boom
		if _, err := f.svc.Pages(t.Context(), "n1", nil); !errors.Is(err, boom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	f := newFixture(t, library3()...)
	f.pages.Fail["ReadPage"] = domain.ErrPagesUnsupported
	if _, err := f.svc.Pages(t.Context(), "n1", nil); !errors.Is(err, domain.ErrPagesUnsupported) {
		t.Fatal(err)
	}
	bad := newFixture(t, porttest.Doc{ID: "b1", Name: "Bad", Pages: [][]byte{[]byte("not a page")}})
	var malformed *domain.MalformedFileError
	if _, err := bad.svc.Pages(t.Context(), "b1", nil); !errors.As(err, &malformed) || !strings.Contains(err.Error(), "page 1") {
		t.Fatal(err)
	}
}

func TestUnmark(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	before, _, _ := library.New(f.cloud, f.cloud, f.cloud).Load(t.Context())
	old, _ := before.Get("p1")
	report, err := f.svc.Unmark(t.Context(), "/Books/Paper", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Erased != 3 || report.Pages != 2 || report.Item.ID != "p1" {
		t.Fatalf("%+v", report)
	}
	after, _, _ := library.New(f.cloud, f.cloud, f.cloud).Load(t.Context())
	cur, _ := after.Get("p1")
	if cur.Hash == old.Hash {
		t.Fatal("the index did not change")
	}
	// The unmarked pages changed; every other file kept its hash.
	changed := 0
	for _, f := range cur.Files {
		prev, _ := oldFile(old, f.Name)
		if prev.Hash != f.Hash {
			changed++
		}
	}
	if changed != 3 { // two pages and the metadata
		t.Fatalf("%d files changed", changed)
	}
	got, err := f.svc.Pages(t.Context(), "p1", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got.Pages {
		if len(p.Highlights) != 0 {
			t.Fatalf("page %d kept %d highlights", p.Number, len(p.Highlights))
		}
	}
	meta := map[string]any{}
	mf, _ := cur.File("metadata")
	data, _ := f.cloud.Blob(mf.Hash)
	if err := json.Unmarshal(data, &meta); err != nil || meta["version"] != float64(2) || meta["metadatamodified"] != true {
		t.Fatalf("%v %v", meta, err)
	}
	// Nothing is left to erase: the second run writes nothing.
	commits := f.cloud.Count("CommitRoot")
	if report, err = f.svc.Unmark(t.Context(), "p1", nil); err != nil || report.Erased != 0 {
		t.Fatalf("%+v %v", report, err)
	}
	if f.cloud.Count("CommitRoot") != commits {
		t.Fatal("an unmark with nothing marked wrote a root")
	}
}

func oldFile(it domain.Item, name string) (domain.File, bool) {
	for _, f := range it.Files {
		if f.Name == name {
			return f, true
		}
	}
	return domain.File{}, false
}

func TestUnmarkSelectedPagesAndConflict(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	f.cloud.Conflicts = 1
	report, err := f.svc.Unmark(t.Context(), "p1", []string{"3"})
	if err != nil || report.Erased != 1 || report.Pages != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	got, _ := f.svc.Pages(t.Context(), "p1", nil)
	if len(got.Pages[0].Highlights) != 2 || len(got.Pages[2].Highlights) != 0 {
		t.Fatalf("%+v", got.Pages)
	}
}

func TestUnmarkRefuses(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	if _, err := f.svc.Unmark(t.Context(), "t1", nil); !errors.Is(err, domain.ErrInTrash) {
		t.Fatalf("trashed: %v", err)
	}
	if _, err := f.svc.Unmark(t.Context(), "/Books", nil); !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatalf("folder: %v", err)
	}
	if _, err := f.svc.Unmark(t.Context(), "p1", []string{"9"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("page: %v", err)
	}
	for _, method := range []string{"Highlights", "Erase"} {
		g := newFixture(t, library3()...)
		boom := errors.New(method)
		g.pages.Fail[method] = boom
		if _, err := g.svc.Unmark(t.Context(), "p1", nil); !errors.Is(err, boom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	for _, method := range []string{"Upload", "PutIndex", "Read", "Root"} {
		g := newFixture(t, library3()...)
		boom := errors.New(method)
		g.cloud.Fail[method] = boom
		if _, err := g.svc.Unmark(t.Context(), "p1", nil); !errors.Is(err, boom) {
			t.Errorf("%s: %v", method, err)
		}
	}
}

// A document that leaves the library between the read and the commit is
// not resurrected.
func TestUnmarkVanished(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	f.roots.before = func() { f.cloud.SetRoot(without(f.cloud.Records(), "p1")) }
	if _, err := f.svc.Unmark(t.Context(), "p1", nil); !errors.Is(err, domain.ErrVanished) {
		t.Fatalf("%v", err)
	}
}

func TestNotebookFromMarkdown(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	f.files.Write("/in/notes.md", []byte("# Title\n\nSome *text* here.\n\n- one\n- two\n\n1. first\n"))
	it, err := f.svc.Notebook(t.Context(), "/in/notes.md", "/Books", port.NotebookOptions{Tags: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	if it.Name != "notes" || it.Parent != "f1" || it.Kind != domain.KindNotebook || !slices.Equal(it.Tags, []string{"work"}) {
		t.Fatalf("%+v", it)
	}
	if len(f.pages.Drafts) != 1 {
		t.Fatalf("%d pages", len(f.pages.Drafts))
	}
	d := f.pages.Drafts[0]
	if d.Sheet != domain.SheetOf(domain.DeviceRM2) || d.Column != domain.ColumnWide || d.Paragraphs[0].Style != domain.StyleHeading || d.Author == [16]byte{} {
		t.Fatalf("%+v", d)
	}
	got, err := f.svc.Pages(t.Context(), string(it.ID), nil)
	if err != nil || len(got.Pages) != 1 || texts(got.Pages)[0] != "Title" {
		t.Fatalf("%+v %v", got, err)
	}
	cf, _ := got.Item.File("content")
	content, _ := f.cloud.Blob(cf.Hash)
	if !strings.Contains(string(content), `"formatVersion":2`) || !strings.Contains(string(content), `"tags":[{"name":"work"`) {
		t.Fatalf("%s", content)
	}
	pd, ok := got.Item.File("pagedata")
	if body, _ := f.cloud.Blob(pd.Hash); !ok || string(body) != "Blank\n" {
		t.Fatalf("pagedata %q", body)
	}
}

func TestNotebookFromText(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	var long strings.Builder
	for range 200 {
		long.WriteString("a line of text\n")
	}
	f.files.Write("/in/long.txt", []byte(long.String()))
	it, err := f.svc.Notebook(t.Context(), "/in/long.txt", "", port.NotebookOptions{Name: "Long", Device: domain.DeviceMove})
	if err != nil {
		t.Fatal(err)
	}
	if it.Name != "Long" || it.Parent != domain.RootID || len(f.pages.Drafts) < 2 {
		t.Fatalf("%+v %d pages", it, len(f.pages.Drafts))
	}
	if f.pages.Drafts[0].Sheet != domain.SheetOf(domain.DeviceMove) || f.pages.Drafts[0].Column != domain.ColumnNarrow {
		t.Fatalf("%+v", f.pages.Drafts[0])
	}
}

// A destination folder gone between the read and the commit sends the
// notebook to the top level instead.
func TestNotebookDiverts(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	f.files.Write("/in/n.md", []byte("x"))
	f.roots.before = func() { f.cloud.SetRoot(without(f.cloud.Records(), "f1")) }
	it, err := f.svc.Notebook(t.Context(), "/in/n.md", "/Books", port.NotebookOptions{})
	if err != nil || it.Parent != domain.RootID {
		t.Fatalf("%+v %v", it, err)
	}
	mf, _ := it.File("metadata")
	data, _ := f.cloud.Blob(mf.Hash)
	if !strings.Contains(string(data), `"parent":""`) {
		t.Fatalf("%s", data)
	}
	// A failure to rewrite the metadata fails the notebook.
	g := newFixture(t, library3()...)
	g.files.Write("/in/n.md", []byte("x"))
	g.roots.before = func() {
		g.cloud.SetRoot(without(g.cloud.Records(), "f1"))
		g.cloud.Fail["Upload"] = errors.New("upload")
	}
	if _, err := g.svc.Notebook(t.Context(), "/in/n.md", "/Books", port.NotebookOptions{}); err == nil {
		t.Fatal("a failed diversion succeeded")
	}
	h := newFixture(t, library3()...)
	h.files.Write("/in/n.md", []byte("x"))
	h.roots.before = func() {
		h.cloud.SetRoot(without(h.cloud.Records(), "f1"))
		h.cloud.Fail["PutIndex"] = errors.New("index")
	}
	if _, err := h.svc.Notebook(t.Context(), "/in/n.md", "/Books", port.NotebookOptions{}); err == nil {
		t.Fatal("a failed diversion succeeded")
	}
}

func TestNotebookRefuses(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	f.files.Write("/in/a.pdf", []byte("x"))
	f.files.Write("/in/table.md", []byte("| a | b |\n"))
	f.files.Write("/in/bad.txt", []byte("\xff\xfe"))
	f.files.Write("/in/.md", []byte("x"))
	f.files.Write("/in/ok.md", []byte("x"))
	f.files.Write("/in/big.txt", make([]byte, pages.MaxText+1))
	if err := f.files.MkdirAll("/in/dir.md"); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]error{
		"/in/a.pdf": domain.ErrUnsupportedFile, "/in/table.md": domain.ErrUnsupportedFile, "/in/bad.txt": domain.ErrUnsupportedFile,
		"/in/missing.md": domain.ErrNotFound, "/in/.md": domain.ErrEmptyName, "/in/big.txt": domain.ErrUnsupportedFile,
		"/in/dir.md": domain.ErrUnsupportedFile,
	} {
		if _, err := f.svc.Notebook(t.Context(), path, "", port.NotebookOptions{}); !errors.Is(err, want) {
			t.Errorf("%s: %v", path, err)
		}
	}
	if _, err := f.svc.Notebook(t.Context(), "/in/ok.md", "", port.NotebookOptions{Device: "kindle"}); !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatalf("device: %v", err)
	}
	if _, err := f.svc.Notebook(t.Context(), "/in/ok.md", "/Paper", port.NotebookOptions{}); err == nil {
		t.Fatal("a document as the destination")
	}
	for _, method := range []string{"WritePage"} {
		g := newFixture(t, library3()...)
		g.files.Write("/in/ok.md", []byte("x"))
		boom := errors.New(method)
		g.pages.Fail[method] = boom
		if _, err := g.svc.Notebook(t.Context(), "/in/ok.md", "", port.NotebookOptions{}); !errors.Is(err, boom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	for _, method := range []string{"Upload", "PutIndex", "Root", "CommitRoot"} {
		g := newFixture(t, library3()...)
		g.files.Write("/in/ok.md", []byte("x"))
		boom := errors.New(method)
		g.cloud.Fail[method] = boom
		if _, err := g.svc.Notebook(t.Context(), "/in/ok.md", "", port.NotebookOptions{}); !errors.Is(err, boom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	g := newFixture(t, library3()...)
	g.files.Fail["Stat"] = errors.New("stat")
	if _, err := g.svc.Notebook(t.Context(), "/in/ok.md", "", port.NotebookOptions{}); err == nil {
		t.Fatal("stat failure")
	}
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	p := porttest.NewPages()
	lib := library.New(c, c, c)
	r := pages.NewReader(lib, p)
	files, ids, clk := porttest.NewMemFiles(), porttest.NewIDs("x"), porttest.NewClock(porttest.Epoch)
	for i, fn := range []func(){
		func() { pages.NewReader(nil, p) },
		func() { pages.NewReader(lib, nil) },
		func() { pages.New(nil, r, p, p, c, c, files, ids, clk) },
		func() { pages.New(lib, nil, p, p, c, c, files, ids, clk) },
		func() { pages.New(lib, r, nil, p, c, c, files, ids, clk) },
		func() { pages.New(lib, r, p, nil, c, c, files, ids, clk) },
		func() { pages.New(lib, r, p, p, nil, c, files, ids, clk) },
		func() { pages.New(lib, r, p, p, c, nil, files, ids, clk) },
		func() { pages.New(lib, r, p, p, c, c, nil, ids, clk) },
		func() { pages.New(lib, r, p, p, c, c, files, nil, clk) },
		func() { pages.New(lib, r, p, p, c, c, files, ids, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("constructor %d did not panic", i)
				}
			}()
			fn()
		}()
	}
}

func TestContains(t *testing.T) {
	t.Parallel()
	ps := []domain.Page{{Paragraphs: para("Hello World")}, {Highlights: []domain.Highlight{{Text: "marked PASSAGE", HasText: true}, {Text: "hidden"}}}}
	for text, want := range map[string]bool{"world": true, "passage": true, "hidden": false, "absent": false} {
		if pages.Contains(ps, text) != want {
			t.Errorf("%s", text)
		}
	}
}

// A plan does everything Notebook does before the upload — reads, parses,
// lays out and writes the pages, resolves the folder — and nothing after.
func TestPlanNotebook(t *testing.T) {
	t.Parallel()
	f := newFixture(t, library3()...)
	f.files.Write("/in/notes.md", []byte("# Title\n\n- one\n"))
	records := f.cloud.Records()
	uploads, commits := f.cloud.Count("Upload"), f.cloud.Count("CommitRoot")
	it, err := f.svc.PlanNotebook(t.Context(), "/in/notes.md", "/Books", port.NotebookOptions{Device: domain.DeviceMove})
	if err != nil || it.Name != "notes" || it.Parent != "f1" || it.Kind != domain.KindNotebook || it.ID != "" {
		t.Fatalf("%+v %v", it, err)
	}
	if len(f.pages.Drafts) != 1 || f.pages.Drafts[0].Sheet != domain.SheetOf(domain.DeviceMove) {
		t.Fatalf("the pages were not laid out: %+v", f.pages.Drafts)
	}
	if f.cloud.Count("Upload") != uploads || f.cloud.Count("CommitRoot") != commits || !slices.Equal(f.cloud.Records(), records) {
		t.Fatal("a plan changed the library")
	}
	// Each check of the upload is a check of the plan.
	f.files.Write("/in/table.md", []byte("| a | b |\n|---|---|\n"))
	for _, tc := range []struct{ local, dest string }{
		{"/in/missing.md", ""},
		{"/in/table.md", ""},
		{"/in/notes.md", "/Nope"},
	} {
		if _, err := f.svc.PlanNotebook(t.Context(), tc.local, tc.dest, port.NotebookOptions{}); err == nil {
			t.Errorf("%s into %q: planned", tc.local, tc.dest)
		}
	}
	if _, err := f.svc.PlanNotebook(t.Context(), "/in/notes.md", "", port.NotebookOptions{Device: "kindle"}); err == nil {
		t.Error("an unknown device planned")
	}
}

// The highlighter leaves strokes as well as highlights: unmark erases a page
// of marker strokes alone, and the report tells the two apart.
func TestUnmarkCountsMarkerStrokes(t *testing.T) {
	t.Parallel()
	hl := []domain.Highlight{{ID: "1:1", Text: "words", HasText: true}}
	f := newFixture(t, porttest.Doc{ID: "m1", Name: "Marked", Ext: "pdf", Pages: [][]byte{
		porttest.MarkedPageBytes(nil, nil, 2, []string{"2:1", "2:2"}),
		porttest.MarkedPageBytes(nil, hl, 1, []string{"2:3"}),
		porttest.PageBytes(para("clean"), nil, 0),
	}})
	report, err := f.svc.Unmark(t.Context(), "m1", nil)
	if err != nil || report.Erased != 4 || report.Highlights != 1 || report.Strokes != 3 || report.Pages != 2 {
		t.Fatalf("%+v %v", report, err)
	}
	got, err := f.svc.Pages(t.Context(), "m1", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, pg := range got.Pages {
		if pg.Stats.Marks != 0 {
			t.Errorf("page %d still has %d marks", pg.Number, pg.Stats.Marks)
		}
	}
}
