// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestBackup(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Backed: port.BackupReport{Documents: 5, Written: 2, Skipped: 3, Moved: 1}}
	r := run(t, f, clitest.Options{}, "backup", "bk")
	want := "dir\tbk\ndocuments\t5\nwritten\t2\nskipped\t3\nmoved\t1\npruned\t0\n"
	if r.Code != 0 || r.Out != want || f.Calls()[0] != "Backup bk / profile=default    jobs=0" {
		t.Fatalf("%d %q %q", r.Code, r.Out, f.Calls())
	}
	f = &clitest.Fake{Backed: port.BackupReport{Documents: 1, Pruned: 2}}
	r = run(t, f, clitest.Options{}, "backup", "bk", "/Notes", "--all", "--prune", "--force", "-j", "8", "--profile", "work", "--json")
	if r.Code != 0 || f.Calls()[0] != "Backup bk /Notes profile=work all prune force jobs=8" {
		t.Fatalf("%d %q", r.Code, f.Calls())
	}
	clitest.Golden(t, "backup", r.Out)
	// A directory named restore is ./restore.
	f = &clitest.Fake{}
	if r = run(t, f, clitest.Options{}, "backup", "./restore"); r.Code != 0 || !strings.HasPrefix(f.Calls()[0], "Backup ./restore /") {
		t.Fatalf("%d %q", r.Code, f.Calls())
	}
	for _, args := range [][]string{{"backup"}, {"backup", "a", "b", "c"}} {
		if r = run(t, &clitest.Fake{}, clitest.Options{}, args...); r.Code != cliutil.CodeUsage {
			t.Fatalf("%v: %d", args, r.Code)
		}
	}
	r = run(t, &clitest.Fake{Err: domain.ErrArchivesUnsupported}, clitest.Options{}, "backup", "bk")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "rebuild with cgo") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

// restored is a restore report: a folder made, a document under its own id
// and one copied under a new one.
func restored() port.RestoreReport {
	notes := clitest.Item(clitest.Spec{ID: "b-notes", Name: "Notes", Kind: domain.KindNotebook})
	copied := clitest.Item(clitest.Spec{ID: "n-paper", Name: "Paper", Kind: domain.KindPDF})
	return port.RestoreReport{
		Profile: "work", Created: time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC), Folders: []string{"/Work"},
		Documents: []port.RestoredDocument{
			{Item: notes, From: "b-notes", File: "b-notes.rmdoc", Folder: "/Work"},
			{Item: copied, From: "d-paper", File: "d-paper.rmdoc", Folder: "/Books"},
		},
	}
}

func TestBackupRestore(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Restored: restored()}
	r := run(t, f, clitest.Options{}, "backup", "restore", "bk", "--yes")
	if r.Code != 0 || !slices.Equal(f.Calls(), []string{"RestoreBackup bk to= dry-run", "RestoreBackup bk to="}) {
		t.Fatalf("%d %q %v", r.Code, f.Calls(), r.Error)
	}
	clitest.Golden(t, "backup-restore", r.Out)
	f = &clitest.Fake{Restored: restored()}
	r = run(t, f, clitest.Options{}, "backup", "restore", "bk", "--to", "/Restored", "--id", "b-no,d-pa", "--id", "x", "--yes", "--json")
	if r.Code != 0 || f.Calls()[1] != "RestoreBackup bk to=/Restored b-no d-pa x" {
		t.Fatalf("%d %q", r.Code, f.Calls())
	}
	clitest.Golden(t, "backup-restore-json", r.Out)
	// A dry run plans and asks nothing.
	f = &clitest.Fake{Restored: restored()}
	r = run(t, f, clitest.Options{}, "backup", "restore", "bk", "--dry-run", "--json")
	if r.Code != 0 || len(f.Calls()) != 1 || !strings.Contains(r.Out, `"dry_run": true`) {
		t.Fatalf("%d %q %s", r.Code, f.Calls(), r.Out)
	}
	r = run(t, &clitest.Fake{Restored: restored()}, clitest.Options{}, "backup", "restore", "bk", "--dry-run")
	if !strings.Contains(r.Out, "would create") || !strings.Contains(r.Out, "would restore") {
		t.Fatalf("%q", r.Out)
	}
}

func TestBackupRestoreAsksFirst(t *testing.T) {
	t.Parallel()
	// Off a terminal without --yes: refused after the plan, nothing restored.
	f := &clitest.Fake{Restored: restored()}
	r := run(t, f, clitest.Options{}, "backup", "restore", "bk")
	if r.Code != cliutil.CodeUsage || len(f.Calls()) != 1 {
		t.Fatalf("%d %v", r.Code, f.Calls())
	}
	// At a terminal the question names what will happen.
	f = &clitest.Fake{Restored: restored()}
	r = run(t, f, clitest.Options{Terminal: true, Stdin: "n\n"}, "backup", "restore", "bk", "--to", "Restored")
	want := `Restore 2 documents from the backup of profile "work" taken 2026-11-02T09:30:00Z into /Restored, creating 1 folder?`
	if !strings.Contains(r.Err, want) || len(f.Calls()) != 1 {
		t.Fatalf("%q %v", r.Err, f.Calls())
	}
	one := restored()
	one.Documents, one.Folders, one.Created = one.Documents[:1], nil, time.Time{}
	if q := restoreQuestion(one, ""); q != `Restore 1 document from the backup of profile "work" into /?` {
		t.Fatal(q)
	}
}

func TestBackupRestoreFails(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{Err: errBoom}, clitest.Options{}, "backup", "restore", "bk", "--yes")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "backup restore: boom") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	// The plan passes and the restore fails.
	svc := (&clitest.Fake{Restored: restored()}).Services()
	svc.Writer = &failSecond{Writer: svc.Writer}
	r = clitest.Exec(t, svc, clitest.Options{}, Commands, "backup", "restore", "bk", "--yes")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "boom") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r = run(t, &clitest.Fake{}, clitest.Options{}, "backup", "restore"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%d", r.Code)
	}
}

var errBoom = errors.New("boom")

// failSecond is a Writer whose second restore fails: the plan passes.
type failSecond struct {
	port.Writer
	n int
}

func (w *failSecond) RestoreBackup(ctx context.Context, dir string, opts port.RestoreOptions) (port.RestoreReport, error) {
	w.n++
	if w.n == 2 {
		return port.RestoreReport{}, errBoom
	}
	return w.Writer.RestoreBackup(ctx, dir, opts)
}
