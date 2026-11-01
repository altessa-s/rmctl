// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// archived opens what an export wrote and lists its entries.
func archived(t *testing.T, files *porttest.MemFiles, path string) ([]string, map[string]string) {
	t.Helper()
	arc, err := porttest.NewArchiver(files).OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	bodies := map[string]string{}
	err = arc.Read(nil, func(e domain.ArchiveEntry, data []byte) error {
		names = append(names, e.Name)
		bodies[e.Name] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names, bodies
}

func TestExportOneDocument(t *testing.T) {
	t.Parallel()
	s, _, files := setup(t)
	report, err := s.Export(t.Context(), []string{"d1"}, "")
	if err != nil || report.Path != "Paper.rmdoc" || report.Documents != 1 || report.Files != 3 {
		t.Fatalf("%+v %v", report, err)
	}
	names, bodies := archived(t, files, report.Path)
	if !slices.Equal(names, []string{"d1aaaaaa.metadata", "d1aaaaaa.content", "d1aaaaaa.pdf"}) || bodies["d1aaaaaa.pdf"] != "PDF ONE" {
		t.Fatalf("%v %v", names, bodies)
	}
}

func TestExportFoldersAndSeveralQueries(t *testing.T) {
	t.Parallel()
	s, _, files := setup(t)
	// The folder stands for its documents, the trashed one left out; d1
	// named again is in the archive once.
	report, err := s.Export(t.Context(), []string{"/Books", "d1"}, "out.rmdoc")
	if err != nil || report.Documents != 4 {
		t.Fatalf("%+v %v", report, err)
	}
	names, _ := archived(t, files, "out.rmdoc")
	ids := map[string]bool{}
	for _, n := range names {
		ids[n[:8]] = true
	}
	for _, want := range []string{"d1aaaaaa", "d2bbbbbb", "d3aaaaaa", "d4aaaaaa"} {
		if !ids[want] {
			t.Errorf("%s missing from %v", want, names)
		}
	}
	if ids["d5aaaaaa"] || ids["f1aaaaaa"] || ids["f2aaaaaa"] {
		t.Fatalf("a trashed document or a folder was exported: %v", names)
	}
	// The top level is every document outside the trash.
	top, err := s.Export(t.Context(), []string{"/"}, "all.rmdoc")
	if err != nil || top.Documents != 5 {
		t.Fatalf("%+v %v", top, err)
	}
	// A trashed document named itself is exported.
	if r, err := s.Export(t.Context(), []string{"d5"}, "old.rmdoc"); err != nil || r.Documents != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestExportTrashedFolderNamedItself(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	c.Add(
		porttest.Doc{ID: "f1", Name: "Gone", Folder: true, Trashed: true},
		porttest.Doc{ID: "d1", Name: "Inside", Parent: "f1", Ext: "pdf"},
	)
	files := porttest.NewMemFiles()
	s := fetch.New(library.New(c, c, c), c, files, porttest.NewArchiver(files))
	if r, err := s.Export(t.Context(), []string{"f1"}, "x.rmdoc"); err != nil || r.Documents != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := s.Export(t.Context(), []string{"/"}, "x.rmdoc"); !errors.Is(err, fetch.ErrNothingToExport) {
		t.Fatalf("the trash was exported from the top: %v", err)
	}
}

func TestExportRefusals(t *testing.T) {
	t.Parallel()
	s, c, files := setup(t)
	bad := c.Record(porttest.Doc{ID: "bad", Name: "Bad", Ext: "pdf", Extra: []domain.File{{Name: "bad/../x", Hash: "h"}}})
	dup := c.Record(porttest.Doc{ID: "dup", Name: "Dup", Ext: "pdf", Extra: []domain.File{{Name: "dup.pdf", Hash: "h"}}})
	empty := c.Record(porttest.Doc{ID: "emptyfol", Name: "Empty", Folder: true})
	c.SetRoot(append(c.Records(), bad, dup, empty))
	// Load the tree first: its metadata reads are not the export's.
	if _, err := s.Export(t.Context(), []string{"/Nope"}, ""); err == nil {
		t.Fatal("a missing path exported")
	}
	reads := c.Count("Read") + c.Count("Download")
	if _, err := s.Export(t.Context(), []string{"bad"}, ""); !errors.Is(err, porttest.ErrBadArchiveName) {
		t.Errorf("bad: %v", err)
	}
	if _, err := s.Export(t.Context(), []string{"dup"}, ""); !errors.Is(err, fetch.ErrRepeatedEntry) {
		t.Errorf("dup: %v", err)
	}
	if c.Count("Read")+c.Count("Download") != reads {
		t.Fatal("a refused export downloaded files")
	}
	for _, q := range [][]string{nil, {"/Nope"}, {"emptyfol"}} {
		if _, err := s.Export(t.Context(), q, ""); err == nil {
			t.Errorf("%v exported", q)
		}
	}
	arc := porttest.NewArchiver(files)
	arc.Fail["WriteArchive"] = errBoom
	packer := fetch.New(library.New(c, c, c), c, files, arc)
	if _, err := packer.Export(context.Background(), []string{"d1"}, "p.rmdoc"); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	files.Fail["Stat"] = errBoom
	if _, err := s.Export(t.Context(), []string{"d1"}, "x"); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	c.Fail["Root"] = errBoom
	if _, err := s.Export(t.Context(), []string{"d1"}, "x"); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

// staged is every file an export left in a staging directory.
func staged(files *porttest.MemFiles) []string {
	return slices.DeleteFunc(files.Paths(), func(p string) bool { return !strings.HasPrefix(p, "stage-") })
}

// An export downloads each file to a staging directory, writes the archive
// there, and installs it at dest only when it is whole; the staging
// directory goes however the export ends.
func TestExportStagesAndCleansUp(t *testing.T) {
	t.Parallel()
	s, c, files := setup(t)
	// Load the tree first: its metadata reads are not the export's.
	if _, err := s.Export(t.Context(), []string{"/Nope"}, ""); err == nil {
		t.Fatal("a missing path exported")
	}
	reads := c.Count("Read")
	if _, err := s.Export(t.Context(), []string{"d1"}, "ok.rmdoc"); err != nil {
		t.Fatal(err)
	}
	// Every file streams down; none is read whole into memory.
	if c.Count("Read") != reads || c.Count("Download") != 3 {
		t.Fatalf("calls %v", c.Calls())
	}
	if left := staged(files); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
	for _, tc := range []struct{ cloud, local, archive string }{
		{cloud: "Download"},
		{local: "Produce"},
		{local: "Open"},
		{archive: "WriteArchive"},
	} {
		t.Run(tc.cloud+tc.local+tc.archive, func(t *testing.T) {
			t.Parallel()
			_, c, files := setup(t)
			arc := porttest.NewArchiver(files)
			s := fetch.New(library.New(c, c, c), c, files, arc)
			if tc.cloud != "" {
				c.Fail[tc.cloud] = errBoom
			}
			if tc.local != "" {
				files.Fail[tc.local] = errBoom
			}
			if tc.archive != "" {
				arc.Fail[tc.archive] = errBoom
			}
			if _, err := s.Export(t.Context(), []string{"d1"}, "x.rmdoc"); !errors.Is(err, errBoom) {
				t.Fatalf("got %v", err)
			}
			if _, ok := files.File("x.rmdoc"); ok {
				t.Fatal("a failed export left an archive")
			}
			if left := staged(files); len(left) != 0 {
				t.Fatalf("left %v", left)
			}
		})
	}
}

func TestExportStagingFailures(t *testing.T) {
	t.Parallel()
	s, c, files := setup(t)
	files.Fail["Stage"] = errBoom
	if _, err := s.Export(t.Context(), []string{"d1"}, "x.rmdoc"); !errors.Is(err, errBoom) || c.Count("Download") != 0 {
		t.Fatalf("%v", err)
	}
	delete(files.Fail, "Stage")
	// The archive is in place; the staging directory that could not be
	// removed is said, and named.
	files.Fail["Unstage"] = errBoom
	r, err := s.Export(t.Context(), []string{"d1"}, "x.rmdoc")
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "staging directory stage-") || r.Files != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, ok := files.File("x.rmdoc"); !ok {
		t.Fatal("the archive is missing")
	}
}
