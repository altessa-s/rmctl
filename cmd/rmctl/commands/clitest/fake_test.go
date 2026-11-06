// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clitest

import (
	"errors"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

var errFake = errors.New("fake")

// expectCalls fails unless the fake recorded exactly want, in order: the
// command tests compare these strings, so their form is the fake's
// contract.
func expectCalls(t *testing.T, f *Fake, want ...string) {
	t.Helper()
	if got := f.Calls(); !slices.Equal(got, want) {
		t.Fatalf("calls\n got %q\nwant %q", got, want)
	}
}

func TestFakeRendering(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := &Fake{Rendered: port.RenderReport{Pages: 4}, RenderFolders: []string{"/Books"}, RenderedTree: port.RenderTreeReport{Documents: make([]port.RenderedDocument, 2)}}
	o := port.RenderOptions{Format: port.FormatPNG, Page: 2, Mode: "pages", Grain: "group", Paper: "a4", Scale: 1.5, Quality: 90, NoTemplates: true, Document: "d1"}
	r, err := f.Render(ctx, "/Books/Paper", "out.png", o)
	if err != nil || r.Path != "out.png" || r.Pages != 4 {
		t.Fatalf("Render %+v %v", r, err)
	}
	f.Rendered.Path = "fixed.pdf"
	if r, _ = f.RenderFile(ctx, "a.rmdoc", "out.pdf", port.RenderOptions{}); r.Path != "fixed.pdf" {
		t.Fatalf("RenderFile keeps the answer's path: %+v", r)
	}
	for query, want := range map[string]bool{"/": true, "/Books": true, "/Books/Paper": false} {
		if got, err := f.Folder(ctx, query); err != nil || got != want {
			t.Errorf("Folder %s: %v %v", query, got, err)
		}
	}
	tree, err := f.RenderTree(ctx, "/Books", "dir", port.RenderOptions{Format: port.FormatPDF},
		port.RenderTreeOptions{All: true, Incremental: true, Jobs: 3})
	if err != nil || tree.Dir != "dir" || len(tree.Documents) != 2 {
		t.Fatalf("RenderTree %+v %v", tree, err)
	}
	expectCalls(t, f,
		"Render /Books/Paper out.png format=png page=2 mode=pages grain=group paper=a4 scale=1.5 quality=90 no-template id=d1",
		"RenderFile a.rmdoc out.pdf format= page=0 mode= grain= paper= scale=0 quality=0",
		"RenderTree /Books dir format=pdf page=0 mode= grain= paper= scale=0 quality=0 all incremental jobs=3")

	f = &Fake{RenderErr: errFake, TreeErr: errFake}
	if _, err := f.Render(ctx, "x", "y", port.RenderOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("RenderErr: %v", err)
	}
	if r, err := f.RenderTree(ctx, "x", "d", port.RenderOptions{}, port.RenderTreeOptions{}); !errors.Is(err, errFake) || r.Dir != "d" {
		t.Fatalf("TreeErr: %+v %v", r, err)
	}
	f = &Fake{Err: errFake}
	if _, err := f.Folder(ctx, "/"); !errors.Is(err, errFake) {
		t.Fatalf("Err fails Folder: %v", err)
	}
	if _, err := f.Render(ctx, "x", "y", port.RenderOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("Err fails Render: %v", err)
	}
	if _, err := f.RenderTree(ctx, "x", "d", port.RenderOptions{}, port.RenderTreeOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("Err fails RenderTree: %v", err)
	}
	if len(f.Calls()) != 2 {
		t.Fatalf("Folder records no call: %v", f.Calls())
	}
}

func TestFakeBackupAndRestore(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := &Fake{Backed: port.BackupReport{Documents: 3}, Restored: port.RestoreReport{Folders: []string{"/In"}}}
	b, err := f.Backup(ctx, "dir", "/Books", port.BackupOptions{Profile: "work", All: true, Prune: true, Force: true, Rescope: true, Jobs: 2})
	if err != nil || b.Dir != "dir" || b.Documents != 3 {
		t.Fatalf("Backup %+v %v", b, err)
	}
	r, err := f.RestoreBackup(ctx, "dir", port.RestoreOptions{To: "/In", IDs: []string{"a", "b"}, DryRun: true})
	if err != nil || len(r.Folders) != 1 {
		t.Fatalf("RestoreBackup %+v %v", r, err)
	}
	f.ArchiveErr = errFake
	if _, err := f.RestoreBackup(ctx, "dir", port.RestoreOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("ArchiveErr: %v", err)
	}
	expectCalls(t, f,
		"Backup dir /Books profile=work all prune force rescope jobs=2",
		"RestoreBackup dir to=/In a b dry-run",
		"RestoreBackup dir to=")
	failing := &Fake{Err: errFake}
	if _, err := failing.RestoreBackup(ctx, "dir", port.RestoreOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("Err: %v", err)
	}
}

func TestFakeEdits(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := &Fake{}
	e, err := f.Rename(ctx, PaperID, "Essay")
	if err != nil || e.Item.Name != "Essay" || !e.Changed {
		t.Fatalf("Rename %+v %v", e, err)
	}
	if _, err := f.Rename(ctx, "/Nope", "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Rename of nothing: %v", err)
	}
	pinned, _ := f.Pin(ctx, []string{PaperID, NovelID}, true)
	if len(pinned) != 2 || !pinned[0].Changed {
		t.Fatalf("Pin %+v", pinned)
	}
	f.Unchanged = true
	tagged, _ := f.Tag(ctx, []string{PaperID}, []string{"a", "b"}, []string{"c"})
	if len(tagged) != 1 || tagged[0].Changed {
		t.Fatalf("Tag with Unchanged %+v", tagged)
	}
	expectCalls(t, f, "Rename d-paper Essay", "Rename /Nope x", "Pin true d-paper d-novel", "Tag +a,b -c d-paper")
	if _, err := (&Fake{Err: errFake}).Rename(ctx, PaperID, "x"); !errors.Is(err, errFake) {
		t.Fatalf("Err: %v", err)
	}
}

func TestFakeUsageAndPlans(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := &Fake{Imported: []port.ImportedDocument{{From: "a"}}}
	if tree, u, err := f.Usage(ctx, "/", 1, true); err != nil || tree == nil || u.Total.Bytes == 0 {
		t.Fatalf("Usage %+v %v", u, err)
	}
	if _, u, err := f.Usage(ctx, BooksID, 0, false); err != nil || u.Total.ID != BooksID {
		t.Fatalf("Usage of a folder %+v %v", u, err)
	}
	if _, _, err := f.Duplicates(ctx, "", false); err != nil {
		t.Fatalf("Duplicates %v", err)
	}
	if got, err := f.PlanImport(ctx, "a.rmdoc", "/"); err != nil || len(got) != 1 {
		t.Fatalf("PlanImport %v %v", got, err)
	}
	it, err := f.PlanNotebook(ctx, "n.pdf", "/", port.NotebookOptions{Name: "N", Device: "rm2", Tags: []string{"t"}})
	if err != nil || it.Name != NotesName || it.ID != "" || it.Kind != domain.KindNotebook {
		t.Fatalf("PlanNotebook %+v %v", it, err)
	}
	expectCalls(t, f, "Usage / 1 all", "Usage f-books 0", "Duplicates", "PlanImport a.rmdoc /", "PlanNotebook n.pdf / N rm2 t")

	f = &Fake{ArchiveErr: errFake, PutErr: map[string]error{"bad.pdf": errFake}}
	if _, err := f.PlanImport(ctx, "a", "/"); !errors.Is(err, errFake) {
		t.Fatalf("ArchiveErr: %v", err)
	}
	if _, err := f.PlanNotebook(ctx, "bad.pdf", "/", port.NotebookOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("PutErr: %v", err)
	}
	failing := &Fake{Err: errFake}
	for name, call := range map[string]func() error{
		"Usage":        func() error { _, _, err := failing.Usage(ctx, "/", 0, false); return err },
		"Duplicates":   func() error { _, _, err := failing.Duplicates(ctx, "/", false); return err },
		"PlanImport":   func() error { _, err := failing.PlanImport(ctx, "a", "/"); return err },
		"PlanNotebook": func() error { _, err := failing.PlanNotebook(ctx, "a", "/", port.NotebookOptions{}); return err },
	} {
		if !errors.Is(call(), errFake) {
			t.Errorf("Err does not fail %s", name)
		}
	}
}

func TestFakeSecretsCacheAndProfiles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := &Fake{
		SecretsAt: port.SecretsStatus{Profile: "work"},
		Migration: port.SecretsMigration{To: port.SecretsKeychain},
		CacheAt: port.CacheReport{Profile: "work", Profiles: []port.ProfileCache{
			{Profile: "home", BudgetSource: port.BudgetFromProfile}, {Profile: "work", Budget: 1 << 20},
		}},
		ProfileList: []port.ProfileSummary{{Name: "work"}},
		ProfileInfo: port.ProfileInfo{Registered: true},
		Removal:     port.ProfileRemoval{Name: "old"},
	}
	svc := f.Services()
	if s, err := svc.Secrets.Status(ctx); err != nil || s.Profile != "work" {
		t.Fatalf("Secrets.Status %+v %v", s, err)
	}
	if m, err := svc.Secrets.Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{Replace: true, SetDefault: true}); err != nil ||
		m.To != port.SecretsKeychain {
		t.Fatalf("Migrate %+v %v", m, err)
	}
	if r, err := svc.Cache.Status(ctx, false); err != nil || len(r.Profiles) != 1 || r.Profiles[0].Profile != "work" {
		t.Fatalf("the current profile's cache alone: %+v %v", r, err)
	}
	if names, err := svc.Cache.Clear(ctx, true); err != nil || !slices.Equal(names, []string{"home", "work"}) {
		t.Fatalf("Clear --all %v %v", names, err)
	}
	if list, err := svc.Profiles.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("List %v %v", list, err)
	}
	if info, err := svc.Profiles.Info(ctx, "work", true); err != nil || info.Name != "work" || !info.Registered {
		t.Fatalf("Info fills the name: %+v %v", info, err)
	}
	if err := svc.Profiles.Add(ctx, "new", "code"); err != nil {
		t.Fatal(err)
	}
	if r, err := svc.Profiles.Remove(ctx, "old", port.RemoveProfileOptions{Revoke: true}); err != nil || r.Name != "old" {
		t.Fatalf("Remove %+v %v", r, err)
	}
	expectCalls(t, f, "SecretsStatus", "Migrate keychain replace set-default", "CacheStatus", "CacheStatus all", "CacheClear all",
		"ProfileList", "ProfileInfo work account", "ProfileAdd new code", "ProfileRemove old revoke")

	f = &Fake{SecretsErr: errFake, CacheErr: errFake, ProfilesErr: errFake, RemoveErr: errFake}
	svc = f.Services()
	for name, call := range map[string]func() error{
		"Secrets.Status":  func() error { _, err := svc.Secrets.Status(ctx); return err },
		"Secrets.Migrate": func() error { _, err := svc.Secrets.Migrate(ctx, port.SecretsFile, port.MigrateOptions{}); return err },
		"Cache.Clear":     func() error { _, err := svc.Cache.Clear(ctx, false); return err },
		"Profiles.List":   func() error { _, err := svc.Profiles.List(ctx); return err },
		"Profiles.Info":   func() error { _, err := svc.Profiles.Info(ctx, "x", false); return err },
		"Profiles.Add":    func() error { return svc.Profiles.Add(ctx, "x", "c") },
		"Profiles.Remove": func() error { _, err := svc.Profiles.Remove(ctx, "x", port.RemoveProfileOptions{}); return err },
	} {
		if !errors.Is(call(), errFake) {
			t.Errorf("%s ignores its error", name)
		}
	}
	failing := (&Fake{Err: errFake}).Services()
	for name, call := range map[string]func() error{
		"Secrets.Status": func() error { _, err := failing.Secrets.Status(ctx); return err },
		"Secrets.Migrate": func() error {
			_, err := failing.Secrets.Migrate(ctx, port.SecretsFile, port.MigrateOptions{})
			return err
		},
		"Cache.Status":    func() error { _, err := failing.Cache.Status(ctx, true); return err },
		"Profiles.List":   func() error { _, err := failing.Profiles.List(ctx); return err },
		"Profiles.Info":   func() error { _, err := failing.Profiles.Info(ctx, "x", false); return err },
		"Profiles.Add":    func() error { return failing.Profiles.Add(ctx, "x", "c") },
		"Profiles.Remove": func() error { _, err := failing.Profiles.Remove(ctx, "x", port.RemoveProfileOptions{}); return err },
	} {
		if !errors.Is(call(), errFake) {
			t.Errorf("Err does not fail %s", name)
		}
	}
}

// Clear fails when recording the clear fails, after the status.
func TestFakeCacheClearRecords(t *testing.T) {
	t.Parallel()
	f := &Fake{CacheAt: port.CacheReport{Profile: "work", Profiles: []port.ProfileCache{{Profile: "work"}}}}
	names, err := f.Services().Cache.Clear(t.Context(), false)
	if err != nil || !slices.Equal(names, []string{"work"}) {
		t.Fatalf("%v %v", names, err)
	}
	expectCalls(t, f, "CacheStatus", "CacheClear")
}

func TestFakeSync(t *testing.T) {
	t.Parallel()
	steps := []port.SyncProgress{{Files: 1, FilesTotal: 2}, {Files: 2, FilesTotal: 2}}
	f := &Fake{Synced: port.SyncReport{Documents: 3}, SyncSteps: steps}
	var seen []port.SyncProgress
	r, err := f.Sync(t.Context(), "/Books", port.SyncOptions{Files: true, All: true, Force: true, Jobs: 4,
		Progress: func(p port.SyncProgress) { seen = append(seen, p) }})
	if err != nil || r.Documents != 3 || !slices.Equal(seen, steps) {
		t.Fatalf("%+v %v %v", r, err, seen)
	}
	if _, err := f.Sync(t.Context(), "/", port.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	expectCalls(t, f, "Sync /Books files all force jobs=4", "Sync / jobs=0")
	if _, err := (&Fake{Err: errFake}).Sync(t.Context(), "/", port.SyncOptions{}); !errors.Is(err, errFake) {
		t.Fatalf("Err: %v", err)
	}
}
