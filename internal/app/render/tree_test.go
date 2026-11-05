// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render_test

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/render"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// treeSetup is a render service over a library with folders, a name
// collision and the trash:
//
//	/Top                 d0
//	/Books/Paper.pdf     d1   pdf
//	/Books/Paper         d2   notebook, the same file name as d1
//	/Books/Sub/Deep      d3
//	/Books/Sub/a/b       d7   a slash in its name
//	/Old                 d5   in the trash, which the tablet keeps at the top
//	/Gone/Inside         d6   in a folder in the trash
func treeSetup(t *testing.T) fixture {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "d0aaaaaa", Name: "Top"},
		porttest.Doc{ID: "f1aaaaaa", Name: "Books", Folder: true},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper.pdf", Parent: "f1aaaaaa", Ext: "pdf", RM: true},
		porttest.Doc{ID: "d2aaaaaa", Name: "Paper", Parent: "f1aaaaaa"},
		porttest.Doc{ID: "f2aaaaaa", Name: "Sub", Parent: "f1aaaaaa", Folder: true},
		porttest.Doc{ID: "d3aaaaaa", Name: "Deep", Parent: "f2aaaaaa"},
		porttest.Doc{ID: "d7aaaaaa", Name: "a/b", Parent: "f2aaaaaa"},
		porttest.Doc{ID: "d5aaaaaa", Name: "Old", Parent: "f1aaaaaa", Trashed: true},
		porttest.Doc{ID: "f3aaaaaa", Name: "Gone", Parent: "f1aaaaaa", Folder: true, Trashed: true},
		porttest.Doc{ID: "d6aaaaaa", Name: "Inside", Parent: "f3aaaaaa"},
	)
	files := porttest.NewMemFiles()
	archiver := porttest.NewArchiver(files)
	r := &porttest.Renderer{}
	return fixture{
		s:     render.New(library.New(c, c, c), c, files, archiver, archiver, r),
		cloud: c, files: files, archiver: archiver, renderer: r,
	}
}

// rows are a report's documents as "status path document", in order.
func rows(r port.RenderTreeReport) []string {
	out := make([]string, len(r.Documents))
	for i, d := range r.Documents {
		out[i] = fmt.Sprintf("%s %s %s", d.Status, d.Report.Path, d.LibraryPath)
	}
	return out
}

// pdfs are the files written, the staging directories aside.
func pdfs(f fixture) []string {
	var out []string
	for _, p := range f.files.Paths() {
		if !strings.HasPrefix(p, "stage-") && path.Base(p) != render.ManifestName {
			out = append(out, p)
		}
	}
	return out
}

func TestRenderTreeMirrorsTheFolders(t *testing.T) {
	t.Parallel()
	for _, jobs := range []int{0, 1, 8} {
		f := treeSetup(t)
		report, err := f.s.RenderTree(t.Context(), "/Books", "out", port.RenderOptions{Paper: port.PaperLetter}, port.RenderTreeOptions{Jobs: jobs})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{
			"rendered out/Paper (d2aaaaaa).pdf /Books/Paper",
			"rendered out/Paper.pdf /Books/Paper.pdf",
			"rendered out/Sub/Deep.pdf /Books/Sub/Deep",
			"rendered out/Sub/a-b.pdf /Books/Sub/a/b",
		}
		if got := rows(report); report.Dir != "out" || !slices.Equal(got, want) {
			t.Fatalf("jobs %d: %s %q", jobs, report.Dir, got)
		}
		if got := pdfs(f); !slices.Equal(got, []string{"out/Paper (d2aaaaaa).pdf", "out/Paper.pdf", "out/Sub/Deep.pdf", "out/Sub/a-b.pdf"}) {
			t.Fatalf("jobs %d: %v", jobs, got)
		}
		for _, d := range report.Documents {
			if d.Report.Pages != 3 || d.Err != nil {
				t.Fatalf("%+v", d)
			}
			// Each PDF takes the document's modification time.
			if mtime, ok, _ := f.files.ModTime(d.Report.Path); !ok || !mtime.Equal(porttest.Epoch) {
				t.Fatalf("%s: %v %v", d.Report.Path, mtime, ok)
			}
		}
		if head, names := f.written(t, "out/Paper.pdf"); head != "PDF d1aaaaaa fresh letter" || len(names) != 4 {
			t.Fatalf("%q %v", head, names)
		}
	}
}

func TestRenderTreeDefaultDirectory(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	report, err := f.s.RenderTree(t.Context(), "f2", "", port.RenderOptions{}, port.RenderTreeOptions{})
	if err != nil || report.Dir != "Sub" || !slices.Equal(pdfs(f), []string{"Sub/Deep.pdf", "Sub/a-b.pdf"}) {
		t.Fatalf("%+v %v %v", report, err, pdfs(f))
	}
	// The whole library goes into a directory of its own.
	f = treeSetup(t)
	report, err = f.s.RenderTree(t.Context(), "/", "", port.RenderOptions{}, port.RenderTreeOptions{})
	want := []string{
		"remarkable/Books/Paper (d2aaaaaa).pdf", "remarkable/Books/Paper.pdf", "remarkable/Books/Sub/Deep.pdf",
		"remarkable/Books/Sub/a-b.pdf", "remarkable/Top.pdf",
	}
	if err != nil || report.Dir != render.TopDir || !slices.Equal(pdfs(f), want) {
		t.Fatalf("%+v %v %v", report, err, pdfs(f))
	}
}

func TestRenderTreeTrash(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	report, err := f.s.RenderTree(t.Context(), "/", "out", port.RenderOptions{}, port.RenderTreeOptions{All: true})
	if err != nil || len(report.Documents) != 7 {
		t.Fatalf("%v %q", err, rows(report))
	}
	for _, p := range []string{"out/Old.pdf", "out/Gone/Inside.pdf"} {
		if _, ok := f.files.File(p); !ok {
			t.Fatalf("%s missing: %v", p, pdfs(f))
		}
	}
	// A folder in the trash named itself keeps its documents.
	f = treeSetup(t)
	report, err = f.s.RenderTree(t.Context(), "f3", "", port.RenderOptions{}, port.RenderTreeOptions{})
	if err != nil || !slices.Equal(rows(report), []string{"rendered Gone/Inside.pdf /Gone/Inside"}) {
		t.Fatalf("%v %q", err, rows(report))
	}
}

func TestRenderTreeIncremental(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	inc := port.RenderTreeOptions{Incremental: true}
	if _, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, inc); err != nil {
		t.Fatal(err)
	}
	drawn, downloads := len(f.renderer.Calls()), f.cloud.Count("Download")
	report, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, inc)
	want := []string{"unchanged out/Deep.pdf /Books/Sub/Deep", "unchanged out/a-b.pdf /Books/Sub/a/b"}
	if err != nil || !slices.Equal(rows(report), want) || len(f.renderer.Calls()) != drawn || f.cloud.Count("Download") != downloads {
		t.Fatalf("%v %q %d", err, rows(report), len(f.renderer.Calls()))
	}
	// A PDF of another time is drawn again, and so is a missing one.
	if err = f.files.SetModTime("out/Deep.pdf", porttest.Epoch.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = f.files.Remove("out/a-b.pdf"); err != nil {
		t.Fatal(err)
	}
	report, err = f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, inc)
	if err != nil || report.Count(port.RenderRendered) != 2 {
		t.Fatalf("%v %q", err, rows(report))
	}
	// A time to the second is the same time.
	if err = f.files.SetModTime("out/Deep.pdf", porttest.Epoch.Add(400*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if report, err = f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, inc); err != nil || report.Count(port.RenderUnchanged) != 2 {
		t.Fatalf("%v %q", err, rows(report))
	}
	// Without incremental every document is drawn.
	if report, err = f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, port.RenderTreeOptions{}); err != nil || report.Count(port.RenderRendered) != 2 {
		t.Fatalf("%v %q", err, rows(report))
	}
}

// An incremental drawing keeps a PDF only when the manifest records it
// drawn from the same document with the same options: another paper, mode
// or template choice draws it again, and so does a manifest that is gone or
// unreadable.
func TestRenderTreeIncrementalHonorsTheOptions(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	inc := port.RenderTreeOptions{Incremental: true}
	draw := func(opts port.RenderOptions) port.RenderTreeReport {
		t.Helper()
		report, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", opts, inc)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	if r := draw(port.RenderOptions{}); r.Count(port.RenderRendered) != 2 {
		t.Fatalf("%q", rows(r))
	}
	if _, ok := f.files.File("out/" + render.ManifestName); !ok {
		t.Fatal("no manifest")
	}
	// The defaults spelled out are the same options.
	if r := draw(port.RenderOptions{Paper: port.PaperA4, Mode: port.PDFFresh}); r.Count(port.RenderUnchanged) != 2 {
		t.Fatalf("%q", rows(r))
	}
	for _, c := range []struct {
		name string
		opts port.RenderOptions
	}{
		{"paper", port.RenderOptions{Paper: port.PaperLetter}},
		{"mode", port.RenderOptions{Paper: port.PaperLetter, Mode: port.PDFPreserving}},
		{"templates", port.RenderOptions{Paper: port.PaperLetter, Mode: port.PDFPreserving, NoTemplates: true}},
	} {
		if r := draw(c.opts); r.Count(port.RenderRendered) != 2 {
			t.Fatalf("%s: %q", c.name, rows(r))
		}
		if r := draw(c.opts); r.Count(port.RenderUnchanged) != 2 {
			t.Fatalf("%s again: %q", c.name, rows(r))
		}
	}
	if head, _ := f.written(t, "out/Deep.pdf"); head != "PDF d3aaaaaa preserving letter no-template" {
		t.Fatalf("%q", head)
	}
	last := port.RenderOptions{Paper: port.PaperLetter, Mode: port.PDFPreserving, NoTemplates: true}
	for name, damage := range map[string]func(){
		"corrupt": func() { f.files.Write("out/"+render.ManifestName, []byte("{not json")) },
		"version": func() { f.files.Write("out/"+render.ManifestName, []byte(`{"v":99,"documents":{}}`)) },
		"missing": func() {
			if err := f.files.Remove("out/" + render.ManifestName); err != nil {
				t.Fatal(err)
			}
		},
	} {
		damage()
		if r := draw(last); r.Count(port.RenderRendered) != 2 {
			t.Fatalf("%s: %q", name, rows(r))
		}
		if r := draw(last); r.Count(port.RenderUnchanged) != 2 {
			t.Fatalf("%s, then: %q", name, rows(r))
		}
	}
	// A document modified since is drawn again, under the same options.
	records, _ := library.Replace(f.cloud.Records(),
		f.cloud.Record(porttest.Doc{ID: "d3aaaaaa", Name: "Deep", Parent: "f2aaaaaa", Modified: porttest.Epoch.Add(time.Hour)}))
	f.cloud.SetRoot(records)
	if r := draw(last); r.Count(port.RenderRendered) != 1 || r.Count(port.RenderUnchanged) != 1 {
		t.Fatalf("%q", rows(r))
	}
}

// A drawing under other options forgets the old record of a PDF before it
// draws anything: stopped halfway, it never leaves a PDF of the new options
// recorded under the old ones.
func TestRenderTreeManifestBeforeDrawing(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	inc := port.RenderTreeOptions{Incremental: true}
	if _, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, inc); err != nil {
		t.Fatal(err)
	}
	f.renderer.FailIDs = map[domain.ItemID]error{"d3aaaaaa": errBoom, "d7aaaaaa": errBoom}
	if _, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{Paper: port.PaperLetter}, inc); !errors.Is(err, render.ErrDocumentsFailed) {
		t.Fatal(err)
	}
	f.renderer.FailIDs = nil
	if r, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, inc); err != nil || r.Count(port.RenderRendered) != 2 {
		t.Fatalf("%v %q", err, rows(r))
	}
	// A manifest that cannot be written fails the drawing.
	f.files.Fail["Produce"] = errBoom
	if _, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{Paper: port.PaperTablet}, inc); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestRenderTreeFailedDocumentsGoOn(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	f.renderer.FailIDs = map[domain.ItemID]error{"d1aaaaaa": errBoom}
	report, err := f.s.RenderTree(t.Context(), "/Books", "out", port.RenderOptions{Mode: port.PDFPreserving}, port.RenderTreeOptions{Jobs: 2})
	if !errors.Is(err, render.ErrDocumentsFailed) || err.Error() != "1 of 4 documents could not be drawn" {
		t.Fatal(err)
	}
	if report.Count(port.RenderRendered) != 3 || report.Count(port.RenderFailed) != 1 {
		t.Fatalf("%q", rows(report))
	}
	failed := report.Documents[1]
	if failed.Status != port.RenderFailed || !errors.Is(failed.Err, errBoom) || failed.Report.Path != "out/Paper.pdf" {
		t.Fatalf("%+v", failed)
	}
	if _, ok := f.files.File("out/Paper.pdf"); ok {
		t.Fatal("a failed document was written")
	}
	for _, c := range []struct {
		name string
		set  func(fixture)
	}{
		{"name", func(f fixture) { f.archiver.Fail["CheckName"] = errBoom }},
		{"download", func(f fixture) { f.cloud.Fail["Download"] = errBoom }},
		{"modification time", func(f fixture) {
			// A PDF's time is read only of a document the manifest records.
			if _, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, port.RenderTreeOptions{}); err != nil {
				t.Fatal(err)
			}
			f.files.Fail["ModTime"] = errBoom
		}},
		{"set the modification time", func(f fixture) { f.files.Fail["SetModTime"] = errBoom }},
	} {
		f := treeSetup(t)
		c.set(f)
		report, err := f.s.RenderTree(t.Context(), "/Books/Sub", "out", port.RenderOptions{}, port.RenderTreeOptions{Incremental: true})
		if !errors.Is(err, render.ErrDocumentsFailed) || report.Count(port.RenderFailed) != 2 || !errors.Is(report.Documents[0].Err, errBoom) {
			t.Fatalf("%s: %v %q", c.name, err, rows(report))
		}
	}
}

func TestRenderTreeRefuses(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		query string
		opts  port.RenderOptions
		want  error
	}{
		{"document", "/Books/Paper.pdf", port.RenderOptions{}, render.ErrNotFolder},
		{"missing", "/Nothing", port.RenderOptions{}, domain.ErrNotFound},
		{"page", "/Books", port.RenderOptions{Page: 1}, render.ErrBadOptions},
		{"image", "/Books", port.RenderOptions{Format: port.FormatPNG}, render.ErrBadOptions},
		{"scale", "/Books", port.RenderOptions{Scale: 2}, render.ErrBadOptions},
		{"quality", "/Books", port.RenderOptions{Quality: 80}, render.ErrBadOptions},
		{"archive document", "/Books", port.RenderOptions{Document: "3f2a"}, render.ErrBadOptions},
		{"mode", "/Books", port.RenderOptions{Mode: "draft"}, render.ErrBadOptions},
	} {
		f := treeSetup(t)
		if _, err := f.s.RenderTree(t.Context(), c.query, "out", c.opts, port.RenderTreeOptions{}); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", c.name, err)
		}
		if paths := f.files.Paths(); len(paths) != 0 {
			t.Fatalf("%s: wrote %v", c.name, paths)
		}
	}
	f := treeSetup(t)
	f.renderer.Unavailable = true
	if _, err := f.s.RenderTree(t.Context(), "/Books", "out", port.RenderOptions{}, port.RenderTreeOptions{}); !errors.Is(err, domain.ErrRenderUnsupported) {
		t.Fatal(err)
	}
	if n := f.cloud.Count("Download") + f.cloud.Count("Root"); n != 0 {
		t.Fatalf("%d calls to the cloud", n)
	}
	f = treeSetup(t)
	f.files.Fail["MkdirAll"] = errBoom
	if _, err := f.s.RenderTree(t.Context(), "/Books", "out", port.RenderOptions{}, port.RenderTreeOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestRenderTreeCanceled(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := f.s.RenderTree(ctx, "/Books", "out", port.RenderOptions{}, port.RenderTreeOptions{Jobs: 2})
	if !errors.Is(err, context.Canceled) || len(report.Documents) != 0 || len(pdfs(f)) != 0 {
		t.Fatalf("%v %q %v", err, rows(report), pdfs(f))
	}
}

func TestFolder(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	for query, want := range map[string]bool{"/": true, "": true, "/Books": true, "f3": true, "/Books/Paper.pdf": false, "d5": false} {
		if got, err := f.s.Folder(t.Context(), query); err != nil || got != want {
			t.Fatalf("%q: %v %v", query, got, err)
		}
	}
	if _, err := f.s.Folder(t.Context(), "/Nothing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	f.cloud.Fail["Root"] = errBoom
	if _, err := f.s.Folder(t.Context(), "/Books"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestTreeFileName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"Paper.pdf": "Paper.pdf", "Book.EPUB": "Book.pdf", "Notes": "Notes.pdf", "a/b": "a-b.pdf", ".pdf": ".pdf.pdf", "": "unnamed.pdf",
	} {
		if got := render.TreeFileName(name); got != want {
			t.Fatalf("%q: %q", name, got)
		}
	}
}

// TestTreePlanCollisions checks that the first of a collision, by creation
// and then identifier, keeps its name, that names collide without case,
// and that a folder and a document share one directory's names.
func TestTreePlanCollisions(t *testing.T) {
	t.Parallel()
	at := func(h int) time.Time { return porttest.Epoch.Add(time.Duration(h) * time.Hour) }
	items := []domain.Item{
		{ID: "zz000001", Name: "notes", Kind: domain.KindNotebook, Created: at(1)},
		{ID: "aa000002", Name: "Notes", Kind: domain.KindNotebook, Created: at(2)},
		{ID: "bb000003", Name: "NOTES.pdf", Kind: domain.KindPDF, Created: at(2)},
		{ID: "cc000004", Name: "Notes.pdf", Kind: domain.KindFolder, Created: at(0)},
		{ID: "dd000005", Name: "x", Kind: domain.KindNotebook, Parent: "cc000004"},
	}
	plan := render.TreePlan(domain.NewTree(1, items), domain.RootID, "out", false)
	got := make([]string, len(plan))
	for i, p := range plan {
		got[i] = string(p.Document.ID) + " " + p.Path
	}
	want := []string{
		"bb000003 out/NOTES (bb000003).pdf",
		"aa000002 out/Notes (aa000002).pdf",
		"dd000005 out/Notes.pdf/x.pdf",
		"zz000001 out/notes (zz000001).pdf",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}
