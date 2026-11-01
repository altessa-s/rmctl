// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

const epub = "PK\x03\x04 an epub body"

func TestPutPDF(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("in/Report.pdf", []byte("%PDF body"))
	f.pdf.Counts["in/Report.pdf"] = 3
	var done int64
	it, err := f.s.Put(t.Context(), "in/Report.pdf", "/Books", port.PutOptions{Tags: []string{"a", "a", ""}}, func(d, _ int64) { done = d })
	if err != nil || it.Name != "Report" || it.Parent != "f1aaaaaa" || it.Kind != domain.KindPDF || done != 9 {
		t.Fatalf("%v %+v %d", err, it, done)
	}
	var names []string
	for _, file := range it.Files {
		names = append(names, strings.TrimPrefix(file.Name, string(it.ID)))
	}
	if !slices.Equal(names, []string{".pdf", ".content", ".metadata", ".pagedata"}) {
		t.Fatalf("files %v", names)
	}
	page, _ := it.File("pagedata")
	if b, _ := f.c.Blob(page.Hash); string(b) != "Blank\nBlank\nBlank\n" {
		t.Fatalf("pagedata %q", b)
	}
	content, _ := it.File("content")
	b, _ := f.c.Blob(content.Hash)
	var c map[string]any
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if c["pageCount"] != float64(3) || c["sizeInBytes"] != "9" || len(c["tags"].([]any)) != 1 {
		t.Fatalf("content %v", c)
	}
	if got, ok := tree(t, f).Get(it.ID); !ok || got.Tags[0] != "a" {
		t.Fatalf("not in the library: %+v", got)
	}
}

func TestPutEPUBNamedAtTheTop(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("Book.EPUB", []byte(epub))
	it, err := f.s.Put(t.Context(), "Book.EPUB", "", port.PutOptions{Name: "My book"}, nil)
	if err != nil || it.Name != "My book" || it.Parent != "" || it.Kind != domain.KindEPUB || len(it.Files) != 3 {
		t.Fatalf("%v %+v", err, it)
	}
}

func TestPutDivertsWhenTheFolderGoes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("x.pdf", []byte("%PDF"))
	f.pdf.Counts["x.pdf"] = 1
	f.roots.before = func() { f.c.SetRoot(f.c.Records()[1:]) }
	it, err := f.s.Put(t.Context(), "x.pdf", "/Books", port.PutOptions{}, nil)
	if err != nil || it.Parent != domain.RootID {
		t.Fatalf("%v %+v", err, it)
	}
	if m := metadata(t, f, it.ID); m["parent"] != "" {
		t.Fatalf("parent %v", m["parent"])
	}
	f = setup(t)
	f.files.Write("x.pdf", []byte("%PDF"))
	f.pdf.Counts["x.pdf"] = 1
	f.roots.before = func() {
		f.c.SetRoot(f.c.Records()[1:])
		f.c.Fail["Upload"] = errBoom
	}
	if _, err := f.s.Put(t.Context(), "x.pdf", "/Books", port.PutOptions{}, nil); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestPutRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path string
		body       string
		pages      int
		pdfErr     error
		dest       string
		want       error
		dir        bool
	}{
		{name: "missing", path: "none.pdf", want: domain.ErrNotFound},
		{name: "directory", path: "dir", dir: true, want: domain.ErrUnsupportedFile},
		{name: "text", path: "a.txt", body: "x", want: domain.ErrUnsupportedFile},
		{name: "pdf without pages", path: "a.pdf", body: "%PDF", want: domain.ErrUnsupportedFile},
		{name: "locked pdf", path: "a.pdf", body: "%PDF", pdfErr: errBoom, want: domain.ErrUnsupportedFile},
		{name: "not an epub", path: "a.epub", body: "nope", want: domain.ErrUnsupportedFile},
		{name: "drm epub", path: "a.epub", body: epub + " META-INF/encryption.xml", want: domain.ErrUnsupportedFile},
		{name: "empty name", path: ".pdf", body: "%PDF", pages: 1, want: domain.ErrEmptyName},
		{name: "destination is a document", path: "a.pdf", body: "%PDF", pages: 1, dest: "/Books/Paper", want: domain.ErrNotAFolder},
		{name: "destination in the trash", path: "a.pdf", body: "%PDF", pages: 1, dest: "f3aaaaaa", want: domain.ErrDestinationGone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			if tc.dir {
				if err := f.files.MkdirAll(tc.path); err != nil {
					t.Fatal(err)
				}
			} else if tc.body != "" {
				f.files.Write(tc.path, []byte(tc.body))
			}
			f.pdf.Counts[tc.path] = tc.pages
			f.pdf.Err = tc.pdfErr
			if _, err := f.s.Put(t.Context(), tc.path, tc.dest, port.PutOptions{}, nil); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			if f.c.Count("UploadFile") != 0 {
				t.Fatal("a refused file was uploaded")
			}
		})
	}
}

// bigFiles reports every file as larger than the cloud accepts.
type bigFiles struct{ *porttest.MemFiles }

func (bigFiles) Stat(string) (int64, bool, bool, error) { return write.MaxUpload + 1, false, true, nil }

func TestPutTooLarge(t *testing.T) {
	t.Parallel()
	f := setup(t)
	c := f.c
	s := write.New(libraryOf(c), c, c, bigFiles{f.files}, porttest.PDF{}, porttest.NewArchiver(f.files), porttest.NewIDs("n"), porttest.NewClock(porttest.Epoch))
	if _, err := s.Put(t.Context(), "big.pdf", "", port.PutOptions{}, nil); !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatalf("got %v", err)
	}
}

func TestPutFailures(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"Root", "UploadFile", "Upload", "PutIndex", "CommitRoot"} {
		f := setup(t)
		f.files.Write("a.pdf", []byte("%PDF"))
		f.pdf.Counts["a.pdf"] = 1
		f.c.Fail[method] = errBoom
		if _, err := f.s.Put(t.Context(), "a.pdf", "", port.PutOptions{}, nil); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	for _, method := range []string{"Stat", "ReadHead", "ReadTail"} {
		f := setup(t)
		f.files.Write("a.epub", []byte(epub))
		f.files.Fail[method] = errBoom
		if _, err := f.s.Put(t.Context(), "a.epub", "", port.PutOptions{}, nil); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
	}
}

func TestUpload(t *testing.T) {
	t.Parallel()
	f := setup(t)
	for _, p := range []string{"src/a.pdf", "src/sub/b.epub", "src/sub/deep/c.pdf", "src/notes.txt"} {
		body := "%PDF"
		if strings.HasSuffix(p, ".epub") {
			body = epub
		}
		f.files.Write(p, []byte(body))
		f.pdf.Counts[p] = 1
	}
	report, err := f.s.Upload(t.Context(), "src", "/Books", port.UploadOptions{Jobs: 2})
	if err != nil || report.Written != 3 {
		t.Fatalf("%v %+v", err, report)
	}
	tr := tree(t, f)
	var paths []string
	for _, it := range tr.Items() {
		if p := tr.Path(it.ID); strings.HasPrefix(p, "/Books/sub") || p == "/Books/a" {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	want := []string{"/Books/a", "/Books/sub", "/Books/sub/b", "/Books/sub/deep", "/Books/sub/deep/c"}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths %v", paths)
	}
	report, err = f.s.Upload(t.Context(), "src", "/Books", port.UploadOptions{Incremental: true})
	if err != nil || report.Written != 0 || report.Skipped != 3 {
		t.Fatalf("%v %+v", err, report)
	}
}

func TestUploadFailures(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Fail["Walk"] = errBoom
	if _, err := f.s.Upload(t.Context(), "src", "", port.UploadOptions{}); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	f.c.Fail["Root"] = errBoom
	if _, err := f.s.Upload(t.Context(), "src", "", port.UploadOptions{}); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	if _, err := f.s.Upload(t.Context(), "src", "/Books/Paper", port.UploadOptions{}); !errors.Is(err, domain.ErrNotAFolder) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	f.files.Write(filepath.Join("src", "Paper", "x.pdf"), []byte("%PDF"))
	if _, err := f.s.Upload(t.Context(), "src", "/Books", port.UploadOptions{}); !errors.Is(err, domain.ErrNotAFolder) {
		t.Fatalf("got %v", err)
	}
	f = setup(t)
	f.files.Write(filepath.Join("src", "x.pdf"), []byte("%PDF"))
	if _, err := f.s.Upload(t.Context(), "src", "", port.UploadOptions{}); !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatalf("got %v", err)
	}
}
