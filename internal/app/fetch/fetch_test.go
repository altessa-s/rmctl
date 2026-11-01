// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch_test

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

func setup(t testing.TB) (*fetch.Service, *porttest.Cloud, *porttest.MemFiles) {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "f1aaaaaa", Name: "Books", Folder: true},
		porttest.Doc{ID: "f2aaaaaa", Name: "Sub/Dir", Folder: true, Parent: "f1aaaaaa"},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Ext: "pdf", Body: []byte("PDF ONE")},
		porttest.Doc{ID: "d2bbbbbb", Name: "paper", Parent: "f1aaaaaa", Ext: "pdf", Body: []byte("PDF TWO")},
		porttest.Doc{ID: "d3aaaaaa", Name: "Novel.epub", Parent: "f2aaaaaa", Ext: "epub", Body: []byte("EPUB")},
		porttest.Doc{ID: "d4aaaaaa", Name: "Notes", Parent: "f1aaaaaa"},
		porttest.Doc{ID: "d5aaaaaa", Name: "Old", Parent: "f1aaaaaa", Ext: "pdf", Trashed: true},
		porttest.Doc{ID: "d6aaaaaa", Name: "../escape", Ext: "pdf", Body: []byte("TOP")},
		porttest.Doc{ID: "f9aaaaaa", Name: "Bin", Folder: true, Trashed: true},
		porttest.Doc{ID: "d9aaaaaa", Name: "Binned", Parent: "f9aaaaaa", Ext: "pdf", Body: []byte("BIN")},
	)
	files := porttest.NewMemFiles()
	return fetch.New(library.New(c, c, c), c, files, porttest.NewArchiver(files)), c, files
}

func TestSafeName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Paper":     "Paper",
		"a/b":       "a-b",
		`a\b:c`:     "a-b-c",
		"tab\there": "tab-here",
		"del\x7f":   "del-",
		"":          "unnamed",
		".":         "unnamed",
		"..":        "unnamed",
		"  ":        "unnamed",
		"../x":      "..-x",
	} {
		if got := fetch.SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func FuzzSafeName(f *testing.F) {
	for _, s := range []string{"", ".", "..", "a/b", `..\..`, "x\x00y"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := fetch.SafeName(s)
		if got == "" || got == "." || got == ".." || strings.ContainsAny(got, `/\:`) {
			t.Fatalf("SafeName(%q) = %q is not one safe component", s, got)
		}
		if filepath.Base(got) != got {
			t.Fatalf("SafeName(%q) = %q leaves its directory", s, got)
		}
	})
}

func TestFileNameAndDisambiguated(t *testing.T) {
	t.Parallel()
	if got := fetch.FileName("Book.PDF", "pdf"); got != "Book.PDF" {
		t.Errorf("got %q", got)
	}
	if got := fetch.FileName("Book", "pdf"); got != "Book.pdf" {
		t.Errorf("got %q", got)
	}
	if got := fetch.FileName("Book", ""); got != "Book" {
		t.Errorf("got %q", got)
	}
	if got := fetch.Disambiguated("Book.pdf", "ab_cd-ef12345", "pdf"); got != "Book (abcd-ef1).pdf" {
		t.Errorf("got %q", got)
	}
	if got := fetch.Disambiguated("Folder", "12345678-9", ""); got != "Folder (12345678)" {
		t.Errorf("got %q", got)
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	s, c, files := setup(t)
	path, err := s.Get(t.Context(), "/Books/Paper", "", nil)
	if err != nil || path != "Paper.pdf" {
		t.Fatalf("%v %q", err, path)
	}
	if b, _ := files.File("Paper.pdf"); string(b) != "PDF ONE" {
		t.Fatalf("got %q", b)
	}
	if err := files.MkdirAll("out"); err != nil {
		t.Fatal(err)
	}
	var done int64
	path, err = s.Get(t.Context(), "d3", "out", func(d, _ int64) { done = d })
	if err != nil || path != filepath.Join("out", "Novel.epub") || done != 4 {
		t.Fatalf("%v %q %d", err, path, done)
	}
	if path, err = s.Get(t.Context(), "d6", "x/", nil); err != nil || path != filepath.Join("x", "..-escape.pdf") {
		t.Fatalf("%v %q", err, path)
	}
	if path, err = s.Get(t.Context(), "d5", "named.pdf", nil); err != nil || path != "named.pdf" {
		t.Fatalf("trashed: %v %q", err, path)
	}
	for _, tc := range []struct {
		query string
		want  error
	}{
		{"/Books", domain.ErrNoOriginal},
		{"d4", domain.ErrNoOriginal},
		{"/Nope", domain.ErrNotFound},
		{"/", domain.ErrNotFound},
	} {
		if _, err := s.Get(t.Context(), tc.query, "", nil); !errors.Is(err, tc.want) {
			t.Errorf("%q: %v", tc.query, err)
		}
	}
	var amb *domain.AmbiguousError
	if _, err := s.Get(t.Context(), "d", "", nil); !errors.As(err, &amb) {
		t.Errorf("got %v", err)
	}
	c.Fail["Download"] = errBoom
	if _, err := s.Get(t.Context(), "d1", "again.pdf", nil); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	if _, ok := files.File("again.pdf"); ok {
		t.Fatal("a failed download left a file")
	}
	files.Fail["Stat"] = errBoom
	if _, err := s.Get(t.Context(), "d1", "x", nil); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	c.Fail["Root"] = errBoom
	if _, err := s.Get(t.Context(), "d1", "", nil); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestMirror(t *testing.T) {
	t.Parallel()
	s, _, files := setup(t)
	files.Write(filepath.Join("m", "stale.pdf"), []byte("old"))
	report, err := s.Mirror(t.Context(), "/Books", "m", port.MirrorOptions{Delete: true, Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join("m", "Paper (d1aaaaaa).pdf"),
		filepath.Join("m", "Sub-Dir", "Novel.epub"),
		filepath.Join("m", "paper (d2bbbbbb).pdf"),
	}
	if got := files.Paths(); !slices.Equal(got, want) {
		t.Fatalf("files %v", got)
	}
	if report != (port.MirrorReport{Written: 3, Skipped: 1, Deleted: 1}) {
		t.Fatalf("report %+v", report)
	}
	report, err = s.Mirror(t.Context(), "f1", "m", port.MirrorOptions{Incremental: true})
	if err != nil || report.Written != 0 || report.Skipped != 4 {
		t.Fatalf("%v %+v", err, report)
	}
}

func TestMirrorTopAndSingle(t *testing.T) {
	t.Parallel()
	s, _, files := setup(t)
	report, err := s.Mirror(t.Context(), "", "all", port.MirrorOptions{})
	if err != nil || report.Written != 4 {
		t.Fatalf("%v %+v", err, report)
	}
	if _, ok := files.File(filepath.Join("all", "..-escape.pdf")); !ok {
		t.Fatalf("files %v", files.Paths())
	}
	report, err = s.Mirror(t.Context(), "d3", "one", port.MirrorOptions{})
	if err != nil || report.Written != 1 {
		t.Fatalf("%v %+v", err, report)
	}
	report, err = s.Mirror(t.Context(), "d4", "none", port.MirrorOptions{})
	if err != nil || report.Skipped != 1 {
		t.Fatalf("%v %+v", err, report)
	}
}

func TestMirrorOfATrashedFolderDeletesNothing(t *testing.T) {
	t.Parallel()
	s, _, files := setup(t)
	kept := filepath.Join("m", "mine.pdf")
	files.Write(kept, []byte("local"))
	for _, q := range []string{"f9aaaaaa", "d9aaaaaa"} {
		if _, err := s.Mirror(t.Context(), q, "m", port.MirrorOptions{Delete: true}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%s: got %v", q, err)
		}
	}
	if got := files.Paths(); !slices.Equal(got, []string{kept}) {
		t.Fatalf("files %v", got)
	}
}

func TestMirrorFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cloud, local string
		opts         port.MirrorOptions
		query        string
	}{
		{cloud: "Root"},
		{cloud: "Download"},
		{local: "MkdirAll"},
		{local: "Stat", opts: port.MirrorOptions{Incremental: true}},
		{local: "Walk", opts: port.MirrorOptions{Delete: true}},
		{query: "/Nope"},
	} {
		s, c, files := setup(t)
		if tc.cloud != "" {
			c.Fail[tc.cloud] = errBoom
		}
		if tc.local != "" {
			files.Fail[tc.local] = errBoom
		}
		q := tc.query
		if q == "" {
			q = "/Books"
		}
		if _, err := s.Mirror(t.Context(), q, "m", tc.opts); err == nil {
			t.Errorf("%+v: no error", tc)
		}
	}
	s, _, files := setup(t)
	files.Write(filepath.Join("m", "stale"), nil)
	files.Fail["Remove"] = errBoom
	if _, err := s.Mirror(t.Context(), "/Books", "m", port.MirrorOptions{Delete: true}); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	lib := library.New(c, c, c)
	files := porttest.NewMemFiles()
	arc := porttest.NewArchiver(files)
	for i, f := range []func(){
		func() { fetch.New(nil, c, files, arc) },
		func() { fetch.New(lib, nil, files, arc) },
		func() { fetch.New(lib, c, nil, arc) },
		func() { fetch.New(lib, c, files, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("case %d did not panic", i)
				}
			}()
			f()
		}()
	}
}
