// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

const backupDir = "bk"

// backupSetup is setup with the archiver at hand.
func backupSetup(t *testing.T) (*fetch.Service, *porttest.Cloud, *porttest.MemFiles, *porttest.Archiver) {
	t.Helper()
	_, c, files := setup(t)
	arc := porttest.NewArchiver(files)
	return fetch.New(library.New(c, c, c), c, files, arc, porttest.NewClock(porttest.Epoch)), c, files, arc
}

// manifestOf reads the manifest a backup wrote.
func manifestOf(t *testing.T, files *porttest.MemFiles) domain.BackupManifest {
	t.Helper()
	raw, ok := files.File(backupDir + "/manifest.json")
	if !ok {
		t.Fatal("no manifest")
	}
	m, err := domain.ParseBackupManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func entryIDs(m domain.BackupManifest) []string {
	ids := make([]string, 0, len(m.Documents))
	for _, e := range m.Documents {
		ids = append(ids, string(e.ID))
	}
	return ids
}

// replaceRecord swaps one record of the root for d's.
func replaceRecord(c *porttest.Cloud, d porttest.Doc) {
	records := c.Records()
	for i, r := range records {
		if r.ID == d.ID {
			records[i] = c.Record(d)
		}
	}
	c.SetRoot(records)
}

// dropRecord takes one record out of the root.
func dropRecord(c *porttest.Cloud, id domain.ItemID) {
	c.SetRoot(slices.DeleteFunc(c.Records(), func(r domain.Record) bool { return r.ID == id }))
}

func TestBackupWritesArchivesAndManifest(t *testing.T) {
	t.Parallel()
	s, c, files, _ := backupSetup(t)
	r, err := s.Backup(t.Context(), backupDir, "/", port.BackupOptions{Profile: "work"})
	if err != nil || r != (port.BackupReport{Dir: backupDir, Documents: 5, Written: 5}) {
		t.Fatalf("%+v %v", r, err)
	}
	m := manifestOf(t, files)
	if m.Format != domain.BackupFormat || m.Profile != "work" || !m.Created.Equal(porttest.Epoch) || m.Path != "/" {
		t.Fatalf("%+v", m)
	}
	// Path order; the trash stays out.
	if got := entryIDs(m); !slices.Equal(got, []string{"d6aaaaaa", "d4aaaaaa", "d1aaaaaa", "d3aaaaaa", "d2bbbbbb"}) {
		t.Fatalf("%v", got)
	}
	e, _ := m.Entry("d1aaaaaa")
	var hash string
	for _, rec := range c.Records() {
		if rec.ID == "d1aaaaaa" {
			hash = rec.Hash
		}
	}
	body, _ := files.File("bk/d1aaaaaa.rmdoc")
	if e.Name != "Paper" || e.Path != "/Books/Paper" || e.Kind != domain.KindPDF || e.Hash != hash ||
		e.File != "d1aaaaaa.rmdoc" || e.Size != int64(len(body)) || e.Trashed {
		t.Fatalf("%+v", e)
	}
	names, bodies := archived(t, files, "bk/d1aaaaaa.rmdoc")
	if !slices.Equal(names, []string{"d1aaaaaa.metadata", "d1aaaaaa.content", "d1aaaaaa.pdf"}) || bodies["d1aaaaaa.pdf"] != "PDF ONE" {
		t.Fatalf("%v", names)
	}
	// Nothing is left staged.
	for _, p := range files.Paths() {
		if p[:2] != "bk" {
			t.Errorf("stray file %s", p)
		}
	}
}

func TestBackupIsIncremental(t *testing.T) {
	t.Parallel()
	s, c, files, _ := backupSetup(t)
	opts := port.BackupOptions{Profile: "p", Jobs: 2}
	if _, err := s.Backup(t.Context(), backupDir, "", opts); err != nil {
		t.Fatal(err)
	}
	r, err := s.Backup(t.Context(), backupDir, "", opts)
	if err != nil || r.Written != 0 || r.Skipped != 5 || r.Documents != 5 {
		t.Fatalf("%+v %v", r, err)
	}
	// A changed document is written again; so is an archive whose size is
	// not the one recorded.
	replaceRecord(c, porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Ext: "pdf", Body: []byte("PDF ONE, v2")})
	files.Write("bk/d4aaaaaa.rmdoc", []byte("truncated"))
	r, err = s.Backup(t.Context(), backupDir, "", opts)
	if err != nil || r.Written != 2 || r.Skipped != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, bodies := archived(t, files, "bk/d1aaaaaa.rmdoc"); bodies["d1aaaaaa.pdf"] != "PDF ONE, v2" {
		t.Fatalf("%v", bodies)
	}
	if _, bodies := archived(t, files, "bk/d4aaaaaa.rmdoc"); len(bodies) == 0 {
		t.Fatal("the damaged archive was not rewritten")
	}
}

func TestBackupMovesOrPrunesDocumentsGone(t *testing.T) {
	t.Parallel()
	s, c, files, _ := backupSetup(t)
	opts := port.BackupOptions{Profile: "p"}
	if _, err := s.Backup(t.Context(), backupDir, "/", opts); err != nil {
		t.Fatal(err)
	}
	dropRecord(c, "d2bbbbbb")
	r, err := s.Backup(t.Context(), backupDir, "/", opts)
	if err != nil || r.Moved != 1 || r.Pruned != 0 || r.Documents != 4 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, ok := files.File("bk/d2bbbbbb.rmdoc"); ok {
		t.Fatal("the archive is still in place")
	}
	if _, ok := files.File("bk/removed/d2bbbbbb.rmdoc"); !ok {
		t.Fatal("the archive was not moved aside")
	}
	if _, ok := manifestOf(t, files).Entry("d2bbbbbb"); ok {
		t.Fatal("the manifest still lists it")
	}
	dropRecord(c, "d3aaaaaa")
	opts.Prune = true
	r, err = s.Backup(t.Context(), backupDir, "/", opts)
	if err != nil || r.Moved != 0 || r.Pruned != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, ok := files.File("bk/d3aaaaaa.rmdoc"); ok {
		t.Fatal("not pruned")
	}
	if _, ok := files.File("bk/removed/d3aaaaaa.rmdoc"); ok {
		t.Fatal("pruned into removed/")
	}
	// An archive already gone by hand is not counted.
	dropRecord(c, "d4aaaaaa")
	if err = files.Remove("bk/d4aaaaaa.rmdoc"); err != nil {
		t.Fatal(err)
	}
	if r, err = s.Backup(t.Context(), backupDir, "/", opts); err != nil || r.Pruned != 0 || r.Moved != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestBackupPathsAndTrash(t *testing.T) {
	t.Parallel()
	s, _, files, _ := backupSetup(t)
	r, err := s.Backup(t.Context(), backupDir, "f2aaaaaa", port.BackupOptions{Profile: "p"})
	if err != nil || r.Documents != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if m := manifestOf(t, files); m.Path != "/Books/Sub/Dir" || !slices.Equal(entryIDs(m), []string{"d3aaaaaa"}) {
		t.Fatalf("%+v", m)
	}
	// One document; the folder's archive is set aside.
	r, err = s.Backup(t.Context(), backupDir, "/Books/Paper", port.BackupOptions{Profile: "p"})
	if err != nil || r.Documents != 1 || r.Moved != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	// --all takes the trash, a document in a trashed folder too.
	r, err = s.Backup(t.Context(), backupDir, "/", port.BackupOptions{Profile: "p", All: true})
	if err != nil || r.Documents != 7 {
		t.Fatalf("%+v %v", r, err)
	}
	m := manifestOf(t, files)
	if e, _ := m.Entry("d5aaaaaa"); !e.Trashed {
		t.Fatalf("%+v", e)
	}
	if e, _ := m.Entry("d9aaaaaa"); !e.Trashed {
		t.Fatalf("%+v", e)
	}
	if _, err = s.Backup(t.Context(), backupDir, "/Nowhere", port.BackupOptions{Profile: "p"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestBackupRefusesAnotherProfile(t *testing.T) {
	t.Parallel()
	s, _, files, _ := backupSetup(t)
	if _, err := s.Backup(t.Context(), backupDir, "/", port.BackupOptions{Profile: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(t.Context(), backupDir, "/", port.BackupOptions{Profile: "b"}); !errors.Is(err, fetch.ErrOtherProfile) {
		t.Fatalf("%v", err)
	}
	if _, err := s.Backup(t.Context(), backupDir, "/", port.BackupOptions{Profile: "b", Force: true}); err != nil {
		t.Fatal(err)
	}
	if m := manifestOf(t, files); m.Profile != "b" {
		t.Fatalf("%+v", m)
	}
}

func TestBackupRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	s, _, files, _ := backupSetup(t)
	files.Write("file", []byte("x"))
	if _, err := s.Backup(t.Context(), "file", "/", port.BackupOptions{}); !errors.Is(err, fetch.ErrNotADirectory) {
		t.Fatalf("%v", err)
	}
	if err := files.MkdirAll(backupDir); err != nil {
		t.Fatal(err)
	}
	files.Write("bk/manifest.json", []byte(`{"format": 9}`))
	if _, err := s.Backup(t.Context(), backupDir, "/", port.BackupOptions{}); !errors.Is(err, domain.ErrBackupManifest) {
		t.Fatalf("%v", err)
	}
	if err := files.MkdirAll("dm/manifest.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(t.Context(), "dm", "/", port.BackupOptions{}); !errors.Is(err, domain.ErrBackupManifest) {
		t.Fatalf("%v", err)
	}
}

func TestBackupFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(c *porttest.Cloud, files *porttest.MemFiles, arc *porttest.Archiver)
		// first runs a backup before the failure is set.
		first bool
	}{
		{name: "root", setup: func(c *porttest.Cloud, _ *porttest.MemFiles, _ *porttest.Archiver) { c.Fail["Root"] = errBoom }},
		{name: "check", setup: func(_ *porttest.Cloud, _ *porttest.MemFiles, a *porttest.Archiver) { a.Fail["CheckName"] = errBoom }},
		{name: "stat", setup: func(_ *porttest.Cloud, f *porttest.MemFiles, _ *porttest.Archiver) { f.Fail["Stat"] = errBoom }},
		{name: "mkdir", setup: func(_ *porttest.Cloud, f *porttest.MemFiles, _ *porttest.Archiver) { f.Fail["MkdirAll"] = errBoom }},
		{name: "download", setup: func(c *porttest.Cloud, _ *porttest.MemFiles, _ *porttest.Archiver) { c.Fail["Read"] = errBoom }},
		{name: "write", setup: func(_ *porttest.Cloud, _ *porttest.MemFiles, a *porttest.Archiver) { a.Fail["WriteArchive"] = errBoom }},
		{name: "stage", setup: func(_ *porttest.Cloud, f *porttest.MemFiles, _ *porttest.Archiver) { f.Fail["Stage"] = errBoom }},
		{name: "produce", setup: func(_ *porttest.Cloud, f *porttest.MemFiles, _ *porttest.Archiver) { f.Fail["Produce"] = errBoom }},
		{name: "rename", first: true, setup: func(c *porttest.Cloud, f *porttest.MemFiles, _ *porttest.Archiver) {
			dropRecord(c, "d1aaaaaa")
			f.Fail["Rename"] = errBoom
		}},
		{name: "remove", first: true, setup: func(c *porttest.Cloud, f *porttest.MemFiles, _ *porttest.Archiver) {
			dropRecord(c, "d1aaaaaa")
			f.Fail["Remove"] = errBoom
		}},
		{name: "stat again", first: true, setup: func(_ *porttest.Cloud, f *porttest.MemFiles, _ *porttest.Archiver) {
			f.Fail["Stat"] = errBoom
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, c, files, arc := backupSetup(t)
			opts := port.BackupOptions{Profile: "p", Prune: tc.name == "remove"}
			if tc.first {
				if _, err := s.Backup(t.Context(), backupDir, "/", opts); err != nil {
					t.Fatal(err)
				}
			}
			tc.setup(c, files, arc)
			if _, err := s.Backup(t.Context(), backupDir, "/", opts); !errors.Is(err, errBoom) {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestBackupPlanAndFile(t *testing.T) {
	t.Parallel()
	if got := fetch.BackupFile("../x"); got != "..-x.rmdoc" {
		t.Fatal(got)
	}
	if !domain.IsBackupFile(fetch.BackupFile("3f2a")) {
		t.Fatal("not a backup file")
	}
	tree := domain.NewTree(1, nil)
	docs, covered, err := fetch.BackupPlan(tree, "", false)
	if err != nil || len(docs) != 0 || covered != "/" {
		t.Fatalf("%v %q %v", docs, covered, err)
	}
}
