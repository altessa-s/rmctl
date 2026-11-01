// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// docFiles are the files of an archived notebook under id.
func docFiles(id, name string) []domain.ArchiveFile {
	return []domain.ArchiveFile{
		{Name: id + ".metadata", Data: []byte(`{"visibleName":"` + name + `","type":"DocumentType","parent":"elsewhere","deleted":true,"version":7,"zzz":1}`)},
		{Name: id + ".content", Data: []byte(`{"fileType":"notebook","tags":[{"name":"t1","timestamp":1}]}`)},
		{Name: id + "/p1.rm", Data: []byte("page one of " + name)},
	}
}

// archive writes an archive of files at path in the fixture's file system.
func archive(t *testing.T, f fixture, path string, files ...domain.ArchiveFile) {
	t.Helper()
	data, err := porttest.NewArchiver(f.files).Pack(files)
	if err != nil {
		t.Fatal(err)
	}
	f.files.Write(path, data)
}

func TestImportFilesTheDocumentUnderDest(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "n.rmdoc", docFiles("a0000000-0000-4000-8000-000000000001", "Notes")...)
	before := len(f.c.Records())
	got, err := f.s.Import(t.Context(), "n.rmdoc", "/Books")
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	it := got[0].Item
	if it.ID != "a0000000-0000-4000-8000-000000000001" || got[0].From != it.ID || it.Parent != "f1aaaaaa" || it.Name != "Notes" ||
		len(it.Tags) != 1 || it.Tags[0] != "t1" {
		t.Fatalf("%+v", got[0])
	}
	if len(f.c.Records()) != before+1 {
		t.Fatalf("records %d → %d", before, len(f.c.Records()))
	}
	m := metadata(t, f, it.ID)
	// Filed under dest and out of the trash; nothing else is an edit.
	if m["parent"] != "f1aaaaaa" || m["deleted"] != false || m["version"] != float64(7) || m["zzz"] != float64(1) {
		t.Fatalf("metadata %v", m)
	}
}

func TestImportRenamesADocumentTheLibraryHolds(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "c.rmdoc", docFiles("d1aaaaaa", "Copy")...)
	original := f.c.Records()
	got, err := f.s.Import(t.Context(), "c.rmdoc", "")
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	it := got[0].Item
	if got[0].From != "d1aaaaaa" || it.ID == "d1aaaaaa" || !strings.HasPrefix(string(it.ID), "new") || it.Parent != domain.RootID {
		t.Fatalf("%+v", got[0])
	}
	for _, file := range it.Files {
		if !strings.HasPrefix(file.Name, string(it.ID)) {
			t.Errorf("%s is not renamed to %s", file.Name, it.ID)
		}
	}
	for _, rec := range original {
		if rec.ID == "d1aaaaaa" && !containsRecord(f.c.Records(), rec) {
			t.Fatal("the library's own document changed")
		}
	}
}

func containsRecord(records []domain.Record, want domain.Record) bool {
	return slices.Contains(records, want)
}

func TestImportSeveralDocumentsInOneCommit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	files := append(docFiles("b0000000-0000-4000-8000-000000000001", "One"), docFiles("b0000000-0000-4000-8000-000000000002", "Two")...)
	archive(t, f, "two.rmdoc", files...)
	commits := f.c.Count("CommitRoot")
	got, err := f.s.Import(t.Context(), "two.rmdoc", "/Books/Fiction")
	if err != nil || len(got) != 2 || got[1].Item.Name != "Two" || got[0].Item.Parent != "f2aaaaaa" {
		t.Fatalf("%v %+v", err, got)
	}
	if f.c.Count("CommitRoot")-commits != 1 {
		t.Fatalf("%d commits", f.c.Count("CommitRoot")-commits)
	}
}

func TestImportDivertsWhenTheFolderGoes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "n.rmdoc", docFiles("c0000000-0000-4000-8000-000000000001", "Notes")...)
	f.roots.before = func() { f.c.SetRoot(f.c.Records()[1:]) }
	got, err := f.s.Import(t.Context(), "n.rmdoc", "/Books")
	if err != nil || got[0].Item.Parent != domain.RootID || metadata(t, f, got[0].Item.ID)["parent"] != "" {
		t.Fatalf("%v %+v", err, got)
	}
}

// A conflict replays the change on a fresh root: an identifier the other
// writer took meanwhile is renamed then, and what was uploaded under a
// choice that still holds is not uploaded again.
func TestImportReplaysAConflict(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "n.rmdoc", docFiles("e0000000-0000-4000-8000-000000000001", "Notes")...)
	f.c.Conflicts = 2
	uploads := f.c.Count("Upload") + f.c.Count("UploadFile")
	got, err := f.s.Import(t.Context(), "n.rmdoc", "")
	if err != nil || got[0].Item.ID != "e0000000-0000-4000-8000-000000000001" {
		t.Fatalf("%v %+v", err, got)
	}
	// The .metadata and .content go up from memory, the page from its staged file.
	if n, staged := f.c.Count("Upload")+f.c.Count("UploadFile")-uploads, f.c.Count("UploadFile"); n != 3 || staged != 1 {
		t.Fatalf("%d uploads (%d staged) for three files over three attempts", n, staged)
	}

	g := setup(t)
	archive(t, g, "n.rmdoc", docFiles("e0000000-0000-4000-8000-000000000002", "Notes")...)
	// The other writer files a document under the archive's identifier.
	g.roots.before = func() {
		g.c.Add(porttest.Doc{ID: "e0000000-0000-4000-8000-000000000002", Name: "Theirs", Ext: "pdf"})
	}
	got, err = g.s.Import(t.Context(), "n.rmdoc", "")
	if err != nil || got[0].Item.ID == "e0000000-0000-4000-8000-000000000002" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestImportRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		files []domain.ArchiveFile
		raw   string
		want  error
	}{
		{name: "empty", raw: "[]", want: write.ErrEmptyArchive},
		{name: "no metadata", files: docFiles("x1", "X")[1:], want: write.ErrNotADocument},
		{name: "no content", files: []domain.ArchiveFile{docFiles("x1", "X")[0]}, want: write.ErrNotADocument},
		{
			name: "folder", want: write.ErrNotADocument,
			files: []domain.ArchiveFile{
				{Name: "x1.metadata", Data: []byte(`{"visibleName":"F","type":"CollectionType"}`)},
				{Name: "x1.content", Data: []byte(`{}`)},
			},
		},
		{name: "bad metadata", files: []domain.ArchiveFile{{Name: "x1.metadata", Data: []byte(`{`)}, {Name: "x1.content", Data: []byte(`{}`)}}},
		{name: "no document", raw: `[{"Name":"a.b/x","Data":""},{"Name":".hidden","Data":""}]`, want: write.ErrNotADocument},
	} {
		f := setup(t)
		if tc.raw != "" {
			f.files.Write("x.rmdoc", []byte(tc.raw))
		} else {
			archive(t, f, "x.rmdoc", tc.files...)
		}
		_, err := f.s.Import(t.Context(), "x.rmdoc", "")
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}

	f := setup(t)
	if _, err := f.s.Import(t.Context(), "missing.rmdoc", ""); err == nil {
		t.Error("a missing archive imported")
	}
	archive(t, f, "n.rmdoc", docFiles("x2", "N")...)
	if _, err := f.s.Import(t.Context(), "n.rmdoc", "/Books/Paper"); !errors.Is(err, domain.ErrNotAFolder) {
		t.Errorf("into a document: %v", err)
	}
	if _, err := f.s.Import(t.Context(), "n.rmdoc", "f3aaaaaa"); !errors.Is(err, domain.ErrDestinationGone) {
		t.Errorf("into the trash: %v", err)
	}
	for _, method := range []string{"Upload", "PutIndex", "Root"} {
		f.c.Fail[method] = errBoom
		if _, err := f.s.Import(t.Context(), "n.rmdoc", ""); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
		delete(f.c.Fail, method)
	}
	arc := porttest.NewArchiver(f.files)
	arc.Fail["Read"] = errBoom
	s := write.New(library.New(f.c, f.c, f.c), f.c, f.c, f.files, porttest.PDF{}, arc, porttest.NewIDs("r"), porttest.NewClock(porttest.Epoch))
	if _, err := s.Import(t.Context(), "n.rmdoc", ""); !errors.Is(err, errBoom) {
		t.Errorf("read: %v", err)
	}
}

// What an export writes, an import reads back: every file the same bytes
// under the new identifier, the metadata filed under the new folder.
func TestExportThenImportRoundTrip(t *testing.T) {
	t.Parallel()
	f := setup(t)
	lib := library.New(f.c, f.c, f.c)
	exporter := fetch.New(lib, f.c, f.files, porttest.NewArchiver(f.files))
	if _, err := exporter.Export(t.Context(), []string{"d1aaaaaa"}, "paper.rmdoc"); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	tree, _, err := lib.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	src, _ := tree.Get("d1aaaaaa")
	for _, file := range src.Files {
		data, ok := f.c.Blob(file.Hash)
		if !ok {
			t.Fatal(file.Name)
		}
		before[strings.TrimPrefix(file.Name, "d1aaaaaa")] = data
	}
	got, err := f.s.Import(t.Context(), "paper.rmdoc", "/Books/Fiction")
	if err != nil {
		t.Fatal(err)
	}
	it := got[0].Item
	if len(it.Files) != len(before) {
		t.Fatalf("%d files, want %d", len(it.Files), len(before))
	}
	for _, file := range it.Files {
		suffix := strings.TrimPrefix(file.Name, string(it.ID))
		data, _ := f.c.Blob(file.Hash)
		if suffix == ".metadata" {
			continue
		}
		if string(data) != string(before[suffix]) {
			t.Errorf("%s changed in the round trip", file.Name)
		}
	}
	if m := metadata(t, f, it.ID); m["parent"] != "f2aaaaaa" || m["visibleName"] != "Paper" {
		t.Fatalf("metadata %v", m)
	}
}

// A plan says what Import would do against the current root: an identifier
// the library holds is shown as the new one, the folder is resolved, and
// nothing is uploaded or committed.
func TestPlanImport(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "p.rmdoc", append(docFiles("d1aaaaaa", "Copy"), docFiles("a0000000-0000-4000-8000-000000000009", "Fresh")...)...)
	records := f.c.Records()
	uploads, commits := f.c.Count("Upload"), f.c.Count("CommitRoot")
	got, err := f.s.PlanImport(t.Context(), "p.rmdoc", "/Books")
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	taken, fresh := got[0], got[1]
	if taken.From != "d1aaaaaa" || taken.Item.ID == "d1aaaaaa" || taken.Item.Parent != "f1aaaaaa" {
		t.Fatalf("taken %+v", taken)
	}
	for _, file := range taken.Item.Files {
		if !strings.HasPrefix(file.Name, string(taken.Item.ID)) {
			t.Errorf("%s is not renamed to %s", file.Name, taken.Item.ID)
		}
	}
	if fresh.From != fresh.Item.ID || fresh.Item.Name != "Fresh" {
		t.Fatalf("fresh %+v", fresh)
	}
	if f.c.Count("Upload") != uploads || f.c.Count("CommitRoot") != commits || !slices.Equal(f.c.Records(), records) {
		t.Fatal("a plan changed the library")
	}
	if _, err := f.s.PlanImport(t.Context(), "p.rmdoc", "/Nope"); err == nil {
		t.Fatal("a plan into a missing folder succeeded")
	}
	if _, err := f.s.PlanImport(t.Context(), "missing.rmdoc", ""); err == nil {
		t.Fatal("a plan of a missing archive succeeded")
	}
}

// staging is every file an import left in a staging directory.
func staging(f fixture) []string {
	return slices.DeleteFunc(f.files.Paths(), func(p string) bool { return !strings.HasPrefix(p, "stage-") })
}

// An import holds the entries that are not .metadata or .content on disk,
// uploads them from there, and removes them however it ends.
func TestImportStagesTheEntriesAndCleansUp(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "n.rmdoc", docFiles("e0000000-0000-4000-8000-000000000003", "Notes")...)
	read := f.c.ReadLocal
	var uploaded []string
	f.c.ReadLocal = func(p string) ([]byte, error) {
		uploaded = append(uploaded, p)
		return read(p)
	}
	got, err := f.s.Import(t.Context(), "n.rmdoc", "")
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	if len(uploaded) != 1 || !strings.HasPrefix(uploaded[0], "stage-") {
		t.Fatalf("uploaded from %v", uploaded)
	}
	page, ok := got[0].Item.File("rm")
	if !ok || page.Size != int64(len("page one of Notes")) {
		t.Fatalf("page %+v", got[0].Item.Files)
	}
	if left := staging(f); len(left) != 0 {
		t.Fatalf("left %v", left)
	}

	for _, tc := range []struct {
		name  string
		cloud string
		local string
		files []domain.ArchiveFile
	}{
		{name: "not a document", files: docFiles("x3", "X")[1:]},
		{name: "staging write", local: "Produce"},
		{name: "upload", cloud: "Upload"},
		{name: "staged upload", cloud: "UploadFile"},
		{name: "commit", cloud: "CommitRoot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			files := tc.files
			if files == nil {
				files = docFiles("x4", "N")
			}
			archive(t, f, "n.rmdoc", files...)
			if tc.cloud != "" {
				f.c.Fail[tc.cloud] = errBoom
			}
			if tc.local != "" {
				f.files.Fail[tc.local] = errBoom
			}
			if _, err := f.s.Import(t.Context(), "n.rmdoc", ""); err == nil {
				t.Fatal("no error")
			}
			if left := staging(f); len(left) != 0 {
				t.Fatalf("left %v", left)
			}
		})
	}
}

func TestImportStagingFailures(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "n.rmdoc", docFiles("x5", "N")...)
	f.files.Fail["Stage"] = errBoom
	if _, err := f.s.Import(t.Context(), "n.rmdoc", ""); !errors.Is(err, errBoom) || f.c.Count("Upload") != 0 {
		t.Fatalf("%v", err)
	}
	delete(f.files.Fail, "Stage")
	// The documents are in; the staging directory that could not be removed
	// is said, and named.
	f.files.Fail["Unstage"] = errBoom
	got, err := f.s.Import(t.Context(), "n.rmdoc", "")
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "staging directory stage-") || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
}

// A plan reads only what it shows: nothing is staged, and the sizes are
// the archive's.
func TestPlanImportStagesNothing(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "n.rmdoc", docFiles("x6", "N")...)
	f.files.Fail["Stage"] = errBoom
	f.files.Fail["Produce"] = errBoom
	got, err := f.s.PlanImport(t.Context(), "n.rmdoc", "")
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	page, ok := got[0].Item.File("rm")
	if !ok || page.Size != int64(len("page one of N")) {
		t.Fatalf("files %+v", got[0].Item.Files)
	}
}
