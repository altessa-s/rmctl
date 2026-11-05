// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// The documents of the backup restoreFixture writes.
const (
	notesID = "b0000000-0000-4000-8000-000000000001"
	draftID = "b0000000-0000-4000-8000-000000000002"
	topID   = "b0000000-0000-4000-8000-000000000003"
)

// backupOf writes a backup directory bk of the entries, each archive a
// notebook of the entry's name.
func backupOf(t *testing.T, f fixture, entries ...domain.BackupEntry) {
	t.Helper()
	for i, e := range entries {
		e.File = string(e.ID) + ".rmdoc"
		entries[i] = e
		archive(t, f, "bk/"+e.File, docFiles(string(e.ID), e.Name)...)
	}
	m := domain.BackupManifest{Format: domain.BackupFormat, Profile: "work", Created: porttest.Epoch, Path: "/", Documents: entries}
	f.files.Write("bk/manifest.json", m.Bytes())
}

// restoreFixture is setup with a backup of a document in /Books, which is
// there, one in /Work/Drafts, which is not, and one at the top level.
func restoreFixture(t *testing.T) fixture {
	t.Helper()
	f := setup(t)
	backupOf(t, f,
		domain.BackupEntry{ID: notesID, Name: "Notes", Path: "/Books/Notes", Kind: domain.KindNotebook},
		domain.BackupEntry{ID: draftID, Name: "Draft", Path: "/Work/Drafts/Draft", Kind: domain.KindNotebook},
		domain.BackupEntry{ID: topID, Name: "Top", Path: "/Top", Kind: domain.KindNotebook},
	)
	return f
}

// restoredParents are the folder paths the documents of a restore are in, by name.
func restoredParents(t *testing.T, f fixture, r port.RestoreReport) map[string]string {
	t.Helper()
	tree, _, err := library.New(f.c, f.c, f.c).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range r.Documents {
		it, ok := tree.Get(d.Item.ID)
		if !ok {
			t.Fatalf("%s is not in the library", d.Item.ID)
		}
		out[it.Name] = tree.Path(it.Parent)
	}
	return out
}

func TestRestoreBackupToTheOriginalPaths(t *testing.T) {
	t.Parallel()
	f := restoreFixture(t)
	before := f.c.Count("CommitRoot")
	r, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Profile != "work" || !r.Created.Equal(porttest.Epoch) || !slices.Equal(r.Folders, []string{"/Work", "/Work/Drafts"}) || len(r.Documents) != 3 {
		t.Fatalf("%+v", r)
	}
	if f.c.Count("CommitRoot") != before+1 {
		t.Fatal("not one commit")
	}
	want := map[string]string{"Notes": "/Books", "Draft": "/Work/Drafts", "Top": "/"}
	if got := restoredParents(t, f, r); len(got) != 3 || got["Notes"] != want["Notes"] || got["Draft"] != want["Draft"] || got["Top"] != want["Top"] {
		t.Fatalf("%v", got)
	}
	d := r.Documents[2] // manifest order is path order
	if d.Item.ID != draftID || d.From != draftID || d.Folder != "/Work/Drafts" || d.File != draftID+".rmdoc" || d.Item.Tags[0] != "t1" {
		t.Fatalf("%+v", d)
	}
	// Restored again, the documents come back as copies under new ids, and
	// the folders are there.
	again, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{IDs: []string{draftID}})
	if err != nil || len(again.Folders) != 0 || len(again.Documents) != 1 {
		t.Fatalf("%+v %v", again, err)
	}
	if c := again.Documents[0]; c.From != draftID || c.Item.ID == draftID {
		t.Fatalf("%+v", c)
	}
}

func TestRestoreBackupBelowAFolder(t *testing.T) {
	t.Parallel()
	f := restoreFixture(t)
	r, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{To: "Restored"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Folders, []string{"/Restored", "/Restored/Books", "/Restored/Work", "/Restored/Work/Drafts"}) {
		t.Fatalf("%v", r.Folders)
	}
	got := restoredParents(t, f, r)
	if got["Notes"] != "/Restored/Books" || got["Draft"] != "/Restored/Work/Drafts" || got["Top"] != "/Restored" {
		t.Fatalf("%v", got)
	}
	// Below an existing folder.
	r, err = f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{To: "/Books", IDs: []string{topID}})
	if err != nil || len(r.Folders) != 0 || restoredParents(t, f, r)["Top"] != "/Books" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRestoreBackupDryRun(t *testing.T) {
	t.Parallel()
	f := restoreFixture(t)
	backupOf(t, f, domain.BackupEntry{ID: "d1aaaaaa", Name: "Paper", Path: "/Books/Paper", Kind: domain.KindNotebook},
		domain.BackupEntry{ID: topID, Name: "Top", Path: "/New/Top", Kind: domain.KindNotebook})
	records := f.c.Records()
	commits := f.c.Count("CommitRoot")
	r, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{DryRun: true})
	if err != nil || len(r.Documents) != 2 || !slices.Equal(r.Folders, []string{"/New"}) {
		t.Fatalf("%+v %v", r, err)
	}
	// The library's document would be copied under a new id.
	if d := r.Documents[0]; d.From != "d1aaaaaa" || d.Item.ID == "d1aaaaaa" || d.Item.Parent != "f1aaaaaa" || d.Folder != "/Books" {
		t.Fatalf("%+v", d)
	}
	if d := r.Documents[1]; d.Item.ID != topID || d.Folder != "/New" || len(d.Item.Files) != 3 {
		t.Fatalf("%+v", d)
	}
	if f.c.Count("CommitRoot") != commits || len(f.c.Records()) != len(records) || f.c.Count("Upload") != 0 {
		t.Fatal("a dry run changed the library")
	}
}

func TestRestoreBackupReplaysAMovedLibrary(t *testing.T) {
	t.Parallel()
	f := restoreFixture(t)
	f.roots.before = func() { f.c.Add(porttest.Doc{ID: topID, Name: "Theirs", Ext: "pdf"}) }
	r, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{IDs: []string{topID}})
	if err != nil || len(r.Documents) != 1 || r.Documents[0].Item.ID == topID || r.Documents[0].From != topID {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRestoreBackupStopsWhenItsFolderIsGone(t *testing.T) {
	t.Parallel()
	f := restoreFixture(t)
	// /Books leaves the library between the look and the commit.
	f.roots.before = func() { f.c.SetRoot(f.c.Records()[1:]) }
	records := len(f.c.Records())
	if _, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{}); !errors.Is(err, domain.ErrDestinationGone) {
		t.Fatalf("%v", err)
	}
	if len(f.c.Records()) != records-1 {
		t.Fatal("the restore committed")
	}
}

func TestRestoreBackupRefuses(t *testing.T) {
	t.Parallel()
	f := restoreFixture(t)
	for _, tc := range []struct {
		dir  string
		opts port.RestoreOptions
		want error
	}{
		{dir: "none", want: write.ErrNoManifest},
		{dir: "bk", opts: port.RestoreOptions{IDs: []string{"zz"}}, want: domain.ErrNotFound},
		{dir: "bk", opts: port.RestoreOptions{To: "/Books/Paper"}, want: domain.ErrNotAFolder},
	} {
		if _, err := f.s.RestoreBackup(t.Context(), tc.dir, tc.opts); !errors.Is(err, tc.want) {
			t.Errorf("%s %+v: %v", tc.dir, tc.opts, err)
		}
	}
	f.files.Write("empty/manifest.json", (domain.BackupManifest{Format: 1}).Bytes())
	if _, err := f.s.RestoreBackup(t.Context(), "empty", port.RestoreOptions{}); !errors.Is(err, write.ErrNothingToRestore) {
		t.Fatalf("%v", err)
	}
	f.files.Write("bad/manifest.json", []byte("{}"))
	if _, err := f.s.RestoreBackup(t.Context(), "bad", port.RestoreOptions{}); !errors.Is(err, domain.ErrBackupManifest) {
		t.Fatalf("%v", err)
	}
	// An archive missing: nothing is uploaded.
	if err := f.files.Remove("bk/" + topID + ".rmdoc"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{}); err == nil || f.c.Count("Upload") != 0 {
		t.Fatalf("%v", err)
	}
	// A folder name that names two folders.
	g := setup(t)
	g.c.Add(porttest.Doc{ID: "f8aaaaaa", Name: "Twin", Folder: true}, porttest.Doc{ID: "f9aaaaaa", Name: "Twin", Folder: true})
	backupOf(t, g, domain.BackupEntry{ID: topID, Name: "Top", Path: "/Twin/Top"})
	if _, err := g.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{}); !errors.As(err, new(*domain.AmbiguousError)) {
		t.Fatalf("%v", err)
	}
}

func TestRestoreBackupFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ cloud, local string }{
		{cloud: "Root"},
		{cloud: "PutIndex"},
		{cloud: "Upload"},
		{local: "Stat"},
		{local: "Stage"},
		{local: "MkdirAll"},
		{local: "Unstage"},
	} {
		t.Run(tc.cloud+tc.local, func(t *testing.T) {
			t.Parallel()
			f := restoreFixture(t)
			if tc.cloud != "" {
				f.c.Fail[tc.cloud] = errBoom
			}
			if tc.local != "" {
				f.files.Fail[tc.local] = errBoom
			}
			if _, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{}); !errors.Is(err, errBoom) {
				t.Fatalf("%v", err)
			}
		})
	}
	// The upload of a document fails after the folders are made.
	f := restoreFixture(t)
	f.roots.before = func() { f.c.Fail["Upload"] = errBoom }
	if _, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{}); !errors.Is(err, errBoom) {
		t.Fatalf("%v", err)
	}
}
