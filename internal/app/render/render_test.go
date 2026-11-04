// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/render"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

// fixture is a render service over a small library and its doubles.
type fixture struct {
	s        *render.Service
	cloud    *porttest.Cloud
	files    *porttest.MemFiles
	archiver *porttest.Archiver
	renderer *porttest.Renderer
}

func setup(t *testing.T) fixture {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "f1aaaaaa", Name: "Books", Folder: true},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper.pdf", Parent: "f1aaaaaa", Ext: "pdf", Body: []byte("PDF ONE"), RM: true},
		porttest.Doc{ID: "d4aaaaaa", Name: "Notes", Parent: "f1aaaaaa", Pages: [][]byte{[]byte("one"), []byte("two")}},
		porttest.Doc{ID: "d5aaaaaa", Name: "Old", Parent: "f1aaaaaa", Trashed: true},
	)
	files := porttest.NewMemFiles()
	archiver := porttest.NewArchiver(files)
	r := &porttest.Renderer{}
	return fixture{
		s:     render.New(library.New(c, c, c), c, files, archiver, archiver, r),
		cloud: c, files: files, archiver: archiver, renderer: r,
	}
}

// written is the file at path: the double's first line and the names of
// the archive it drew.
func (f fixture) written(t *testing.T, path string) (string, []string) {
	t.Helper()
	data, ok := f.files.File(path)
	if !ok {
		t.Fatalf("%s was not written: %v", path, f.files.Paths())
	}
	head, archive, _ := strings.Cut(string(data), "\n")
	f.files.Write("drawn.rmdoc", []byte(archive))
	arc, err := f.archiver.OpenArchive("drawn.rmdoc")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range arc.Entries() {
		names = append(names, e.Name)
	}
	return head, names
}

func TestRenderPDF(t *testing.T) {
	t.Parallel()
	f := setup(t)
	report, err := f.s.Render(t.Context(), "/Books/Notes", "", port.RenderOptions{})
	if err != nil || report != (port.RenderReport{Path: "Notes-rendered.pdf", Pages: 3}) {
		t.Fatalf("%+v %v", report, err)
	}
	head, names := f.written(t, "Notes-rendered.pdf")
	if head != "PDF d4aaaaaa fresh a4" || strings.Join(names, " ") != "d4aaaaaa.metadata d4aaaaaa.content d4aaaaaa/p1.rm d4aaaaaa/p2.rm" {
		t.Fatalf("%q %v", head, names)
	}
	// The staging directory is gone; only the drawing is left.
	for _, p := range f.files.Paths() {
		if strings.HasPrefix(p, "stage-") {
			t.Fatalf("staging left behind: %v", f.files.Paths())
		}
	}
	// A trashed document is drawn too, into a directory, as asked.
	f.files.MkdirAll("out")
	f.renderer.Report = port.RenderReport{SkippedBlocks: 2, RefusedPages: 1, LinksDropped: 4}
	report, err = f.s.Render(t.Context(), "d5", "out", port.RenderOptions{Mode: port.PDFRecomposed, Paper: port.PaperTablet})
	want := port.RenderReport{Path: "out/Old-rendered.pdf", Pages: 3, SkippedBlocks: 2, RefusedPages: 1, LinksDropped: 4}
	if err != nil || report != want {
		t.Fatalf("%+v %v", report, err)
	}
	if head, _ = f.written(t, report.Path); head != "PDF d5aaaaaa recomposed tablet" {
		t.Fatal(head)
	}
}

func TestRenderPNG(t *testing.T) {
	t.Parallel()
	f := setup(t)
	report, err := f.s.Render(t.Context(), "d1", "", port.RenderOptions{Page: 2, Paper: port.PaperLetter})
	if err != nil || report != (port.RenderReport{Path: "Paper-p2.png", Pages: 1}) {
		t.Fatalf("%+v %v", report, err)
	}
	head, names := f.written(t, "Paper-p2.png")
	if head != "PNG d1aaaaaa p2 x1 letter" || len(names) != 4 {
		t.Fatalf("%q %v", head, names)
	}
	if report, err = f.s.Render(t.Context(), "d1", "page.png", port.RenderOptions{Page: 1, Scale: 0.25}); err != nil || report.Path != "page.png" {
		t.Fatalf("%+v %v", report, err)
	}
	if head, _ = f.written(t, "page.png"); head != "PNG d1aaaaaa p1 x0.25 a4" {
		t.Fatal(head)
	}
}

func TestRenderImages(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		opts       port.RenderOptions
		path, head string
	}{
		{port.RenderOptions{Page: 2, Format: port.FormatJPEG}, "Paper-p2.jpg", "JPG d1aaaaaa p2 x1 q0 a4"},
		{port.RenderOptions{Page: 1, Format: port.FormatJPEG, Scale: 2, Quality: 90}, "Paper-p1.jpg", "JPG d1aaaaaa p1 x2 q90 a4"},
		{port.RenderOptions{Page: 3, Format: port.FormatSVG, Paper: port.PaperTablet}, "Paper-p3.svg", "SVG d1aaaaaa p3 tablet"},
		{port.RenderOptions{Page: 1, NoTemplates: true}, "Paper-p1.png", "PNG d1aaaaaa p1 x1 a4 no-template"},
		{port.RenderOptions{NoTemplates: true}, "Paper-rendered.pdf", "PDF d1aaaaaa fresh a4 no-template"},
	} {
		f := setup(t)
		report, err := f.s.Render(t.Context(), "d1", "", c.opts)
		if err != nil || report.Path != c.path {
			t.Fatalf("%+v: %+v %v", c.opts, report, err)
		}
		if head, _ := f.written(t, c.path); head != c.head {
			t.Fatalf("%+v: %q", c.opts, head)
		}
	}
}

func TestRenderRefuses(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		query string
		opts  port.RenderOptions
		want  error
	}{
		{"folder", "/Books", port.RenderOptions{}, render.ErrFolder},
		{"missing", "/Nothing", port.RenderOptions{}, domain.ErrNotFound},
		{"page beyond", "d4", port.RenderOptions{Page: 4}, domain.ErrNoSuchPage},
		{"bad options", "d4", port.RenderOptions{Page: -1}, render.ErrBadOptions},
	} {
		f := setup(t)
		if _, err := f.s.Render(t.Context(), c.query, "", c.opts); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", c.name, err)
		}
		if paths := f.files.Paths(); len(paths) != 0 {
			t.Fatalf("%s: wrote %v", c.name, paths)
		}
	}
}

func TestRenderUnavailableDownloadsNothing(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.renderer.Unavailable = true
	if _, err := f.s.Render(t.Context(), "d4", "", port.RenderOptions{}); !errors.Is(err, domain.ErrRenderUnsupported) {
		t.Fatal(err)
	}
	if n := f.cloud.Count("Download") + f.cloud.Count("Root"); n != 0 {
		t.Fatalf("%d calls to the cloud", n)
	}
	if _, err := f.s.RenderFile(t.Context(), "x.rmdoc", "", port.RenderOptions{}); !errors.Is(err, domain.ErrRenderUnsupported) {
		t.Fatal(err)
	}
}

func TestRenderFailures(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		set  func(fixture)
	}{
		{"root", func(f fixture) { f.cloud.Fail["Root"] = errBoom }},
		{"name", func(f fixture) { f.archiver.Fail["CheckName"] = errBoom }},
		{"stat", func(f fixture) { f.files.Fail["Stat"] = errBoom }},
		{"stage", func(f fixture) { f.files.Fail["Stage"] = errBoom }},
		{"unstage", func(f fixture) { f.files.Fail["Unstage"] = errBoom }},
		{"download", func(f fixture) { f.cloud.Fail["Download"] = errBoom }},
		{"pack", func(f fixture) { f.archiver.Fail["WriteArchive"] = errBoom }},
		{"open", func(f fixture) { f.files.Fail["Open"] = errBoom }},
		{"render", func(f fixture) { f.renderer.Fail = errBoom }},
		{"write", func(f fixture) { f.files.Fail["Produce"] = errBoom }},
	} {
		f := setup(t)
		c.set(f)
		if _, err := f.s.Render(t.Context(), "d4", "out.pdf", port.RenderOptions{}); !errors.Is(err, errBoom) {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, ok := f.files.File("out.pdf"); ok {
			t.Fatalf("%s: out.pdf was written", c.name)
		}
	}
	// A canceled context stops it.
	f := setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.s.Render(ctx, "d4", "out.pdf", port.RenderOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// archive packs documents, each a name, a folder's name after "+", or "!"
// for a .metadata that does not decode, into a local archive at path.
func archive(t *testing.T, f fixture, path string, docs map[domain.ItemID]string) {
	t.Helper()
	var files []domain.ArchiveFile
	for id, name := range docs {
		meta := domain.NewDocumentMetadata(name, "", porttest.Epoch)
		if folder, ok := strings.CutPrefix(name, "+"); ok {
			meta = domain.NewFolderMetadata(folder, "", porttest.Epoch)
		}
		data := meta.Bytes()
		if name == "!" {
			data = []byte("{not json")
		}
		files = append(files,
			domain.ArchiveFile{Name: string(id) + ".metadata", Data: data},
			domain.ArchiveFile{Name: string(id) + "/p1.rm", Data: []byte("page")})
	}
	data, err := f.archiver.Pack(files)
	if err != nil {
		t.Fatal(err)
	}
	f.files.Write(path, data)
}

func TestRenderFile(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "one.rmdoc", map[domain.ItemID]string{"a1": "Meeting", "f1": "+Folder", "bad": "!"})
	report, err := f.s.RenderFile(t.Context(), "one.rmdoc", "", port.RenderOptions{})
	if err != nil || report.Path != "Meeting-rendered.pdf" || report.Pages != 3 {
		t.Fatalf("%+v %v", report, err)
	}
	if head, _ := f.written(t, report.Path); head != "PDF a1 fresh a4" {
		t.Fatal(head)
	}
	if n := f.cloud.Count("Root") + f.cloud.Count("Download"); n != 0 {
		t.Fatalf("%d calls to the cloud", n)
	}
	// Several documents: one is named by its identifier or a prefix.
	archive(t, f, "many.rmdoc", map[domain.ItemID]string{"a1x": "One", "a2x": "Two", "b1x": "Three"})
	if _, err = f.s.RenderFile(t.Context(), "many.rmdoc", "", port.RenderOptions{}); !errors.Is(err, render.ErrSeveralDocuments) ||
		!strings.Contains(err.Error(), "a1x") {
		t.Fatal(err)
	}
	report, err = f.s.RenderFile(t.Context(), "many.rmdoc", "p.png", port.RenderOptions{Document: "b", Page: 1})
	if err != nil || report.Path != "p.png" {
		t.Fatalf("%+v %v", report, err)
	}
	if head, _ := f.written(t, "p.png"); head != "PNG b1x p1 x1 a4" {
		t.Fatal(head)
	}
	if _, err = f.s.RenderFile(t.Context(), "many.rmdoc", "", port.RenderOptions{Document: "a2x"}); err != nil {
		t.Fatal(err)
	}
	var ambiguous *domain.AmbiguousError
	if _, err = f.s.RenderFile(t.Context(), "many.rmdoc", "", port.RenderOptions{Document: "a"}); !errors.As(err, &ambiguous) {
		t.Fatal(err)
	}
	if _, err = f.s.RenderFile(t.Context(), "many.rmdoc", "", port.RenderOptions{Document: "z"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRenderFileRefuses(t *testing.T) {
	t.Parallel()
	f := setup(t)
	if _, err := f.s.RenderFile(t.Context(), "none.rmdoc", "", port.RenderOptions{}); err == nil || !strings.HasPrefix(err.Error(), "none.rmdoc: ") {
		t.Fatal(err)
	}
	archive(t, f, "folder.rmdoc", map[domain.ItemID]string{"f1": "+Folder"})
	if _, err := f.s.RenderFile(t.Context(), "folder.rmdoc", "", port.RenderOptions{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := f.s.RenderFile(t.Context(), "folder.rmdoc", "", port.RenderOptions{Mode: "odd"}); !errors.Is(err, render.ErrBadOptions) {
		t.Fatal(err)
	}
	archive(t, f, "one.rmdoc", map[domain.ItemID]string{"a1": "Meeting"})
	for _, method := range []string{"Stat", "Open"} {
		f.files.Fail[method] = errBoom
		if _, err := f.s.RenderFile(t.Context(), "one.rmdoc", "out", port.RenderOptions{}); !errors.Is(err, errBoom) {
			t.Fatalf("%s: %v", method, err)
		}
		delete(f.files.Fail, method)
	}
	f.archiver.Fail["Read"] = errBoom
	if _, err := f.s.RenderFile(t.Context(), "one.rmdoc", "", port.RenderOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestNormalized(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in, want port.RenderOptions
	}{
		{port.RenderOptions{}, port.RenderOptions{Format: port.FormatPDF, Mode: port.PDFFresh, Paper: port.PaperA4}},
		{
			port.RenderOptions{Mode: port.PDFPreserving, Paper: port.PaperLetter, NoTemplates: true},
			port.RenderOptions{Format: port.FormatPDF, Mode: port.PDFPreserving, Paper: port.PaperLetter, NoTemplates: true},
		},
		{port.RenderOptions{Page: 3}, port.RenderOptions{Page: 3, Format: port.FormatPNG, Paper: port.PaperA4, Scale: render.DefaultScale}},
		{
			port.RenderOptions{Page: 1, Scale: render.MaxScale},
			port.RenderOptions{Page: 1, Format: port.FormatPNG, Paper: port.PaperA4, Scale: render.MaxScale},
		},
		{
			port.RenderOptions{Page: 2, Format: port.FormatJPEG},
			port.RenderOptions{Page: 2, Format: port.FormatJPEG, Paper: port.PaperA4, Scale: render.DefaultScale},
		},
		{
			port.RenderOptions{Page: 2, Format: port.FormatJPEG, Quality: render.MaxQuality},
			port.RenderOptions{Page: 2, Format: port.FormatJPEG, Paper: port.PaperA4, Scale: render.DefaultScale, Quality: render.MaxQuality},
		},
		{port.RenderOptions{Page: 2, Format: port.FormatSVG}, port.RenderOptions{Page: 2, Format: port.FormatSVG, Paper: port.PaperA4}},
	} {
		if got, err := render.Normalized(c.in); err != nil || got != c.want {
			t.Errorf("%+v: %+v %v", c.in, got, err)
		}
	}
	for _, bad := range []port.RenderOptions{
		{Page: -1},
		{Paper: "a3"},
		{Mode: "draft"},
		{Scale: 2},
		{Page: 1, Mode: port.PDFFresh},
		{Page: 1, Scale: -1},
		{Page: 1, Scale: render.MaxScale + 1},
		{Format: "gif", Page: 1},
		{Format: port.FormatPDF, Page: 1},
		{Format: port.FormatPNG},
		{Format: port.FormatJPEG},
		{Format: port.FormatSVG},
		{Page: 1, Format: port.FormatSVG, Scale: 2},
		{Page: 1, Format: port.FormatSVG, Mode: port.PDFFresh},
		{Page: 1, Quality: 50},
		{Page: 1, Format: port.FormatSVG, Quality: 50},
		{Quality: 50},
		{Page: 1, Format: port.FormatJPEG, Quality: -1},
		{Page: 1, Format: port.FormatJPEG, Quality: render.MaxQuality + 1},
	} {
		if _, err := render.Normalized(bad); !errors.Is(err, render.ErrBadOptions) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestFileName(t *testing.T) {
	t.Parallel()
	pdf, png := port.RenderOptions{}, port.RenderOptions{Page: 12}
	jpg, svg := port.RenderOptions{Page: 2, Format: port.FormatJPEG}, port.RenderOptions{Page: 3, Format: port.FormatSVG}
	for _, c := range []struct {
		name string
		opts port.RenderOptions
		want string
	}{
		{"Notes", pdf, "Notes-rendered.pdf"},
		{"Paper.PDF", pdf, "Paper-rendered.pdf"},
		{"Novel.epub", png, "Novel-p12.png"},
		{".pdf", pdf, ".pdf-rendered.pdf"},
		{"a/b", png, "a-b-p12.png"},
		{"pdf", pdf, "pdf-rendered.pdf"},
		{"Notes", jpg, "Notes-p2.jpg"},
		{"Paper.pdf", svg, "Paper-p3.svg"},
		{"Notes", port.RenderOptions{Format: port.FormatPDF}, "Notes-rendered.pdf"},
	} {
		if got := render.FileName(c.name, c.opts); got != c.want {
			t.Errorf("%q: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNewPanicsWithoutADependency(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	files := porttest.NewMemFiles()
	a := porttest.NewArchiver(files)
	lib := library.New(c, c, c)
	r := &porttest.Renderer{}
	for i, build := range []func(){
		func() { render.New(nil, c, files, a, a, r) },
		func() { render.New(lib, nil, files, a, a, r) },
		func() { render.New(lib, c, nil, a, a, r) },
		func() { render.New(lib, c, files, nil, a, r) },
		func() { render.New(lib, c, files, a, nil, r) },
		func() { render.New(lib, c, files, a, a, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%d: no panic", i)
				}
			}()
			build()
		}()
	}
}
