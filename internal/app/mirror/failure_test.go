// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/mirror"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestNewEnginePanicsOnNil(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	for _, name := range []string{"Library", "Writer", "Rendering", "Fetcher", "Files", "Disk", "Notifier", "DirWatcher", "Timer"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(string), strings.NewReplacer("Disk", "MirrorFiles").Replace(name)) {
					t.Fatalf("no panic naming %s: %v", name, r)
				}
			}()
			mirror.NewEngine(depsWithout(f, name))
		})
	}
}

// depsWithout is a full set of the fixture's dependencies but name.
func depsWithout(f *fixture, name string) mirror.Deps {
	d := f.deps(true, porttest.NewClock(now))
	switch name {
	case "Library":
		d.Library = nil
	case "Writer":
		d.Writer = nil
	case "Rendering":
		d.Rendering = nil
	case "Fetcher":
		d.Fetcher = nil
	case "Files":
		d.Files = nil
	case "Disk":
		d.Disk = nil
	case "Notifier":
		d.Notifier = nil
	case "DirWatcher":
		d.Dirs = nil
	case "Timer":
		d.Timer = nil
	}
	return d
}

// The directory must be there, and free: a missing one is an unmounted
// drive, and another process's lock is respected.
func TestPassRefusesAMissingOrBusyDirectory(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	missing := f.spec
	missing.Dir = "/gone"
	p, err := f.engine.Pass(t.Context(), missing, port.MirrorRunOptions{})
	if !errors.Is(err, domain.ErrNotFound) || !strings.Contains(err.Error(), "mounted") || p.Err == nil || p.Ended.IsZero() {
		t.Fatalf("%+v %v", p, err)
	}
	f.files.Write("/file", []byte("x"))
	notDir := f.spec
	notDir.Dir = "/file"
	if _, err = f.engine.Pass(t.Context(), notDir, port.MirrorRunOptions{}); !errors.Is(err, domain.ErrNotAFolder) {
		t.Fatal(err)
	}
	release, err := f.disk.Lock(filepath.Join(dir, domain.MirrorLockName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorBusy) {
		t.Fatal(err)
	}
	_ = release()
	f.disk.Fail["Lock"] = errBoom
	if _, err = f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	f.files.Fail["Stat"] = errBoom
	if _, err = f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// A journal that does not decode stops the pass: the mirror never guesses
// what was in step.
func TestPassRefusesAnUnreadableJournal(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{"garbage": "{not json", "version": `{"v": 9, "items": {}}`} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := setup(t, true)
			f.files.Write(filepath.Join(dir, domain.MirrorJournalName), []byte(body))
			if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, mirror.ErrJournal) {
				t.Fatal(err)
			}
		})
	}
	f := setup(t, true)
	if err := f.files.MkdirAll(filepath.Join(dir, domain.MirrorJournalName)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, mirror.ErrJournal) {
		t.Fatal(err)
	}
	f = setup(t, true)
	f.files.Write(filepath.Join(dir, domain.MirrorJournalName), []byte(`{"v": 1}`))
	f.files.Fail["Open"] = errBoom
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// The folder is created for a directory never mirrored; one the journal
// knows that moved, went or was swapped for another is refused.
func TestPassFindsItsFolder(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "f9aaaaaa", Name: "Other", Folder: true}, porttest.Doc{ID: "x1aaaaaa", Name: "File", Ext: "pdf"})
	f.spec.Folder = "/New/Inbox"
	f.pdfFile("A.pdf", "%PDF a", 1, time10)
	want(t, f.pass(port.MirrorRunOptions{DryRun: true}), "upload A.pdf")
	want(t, f.pass(port.MirrorRunOptions{}), "upload A.pdf")
	a, _ := f.doc("A")
	if path, _ := f.path(a.ID); path != "/New/Inbox/A" {
		t.Fatalf("A went to %s", path)
	}
	// The folder renamed on the tablet: refused, naming where it went.
	tree, _, _ := library.New(f.cloud, f.cloud, f.cloud).Load(t.Context())
	inbox, _, _ := tree.Resolve("/New/Inbox", false)
	f.replace(porttest.Doc{ID: inbox.ID, Name: "Renamed", Parent: inbox.Parent, Folder: true})
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorRefused) ||
		!strings.Contains(err.Error(), "/New/Renamed") {
		t.Fatal(err)
	}
	// Trashed: refused, nothing deleted locally.
	f.replace(porttest.Doc{ID: inbox.ID, Name: "Inbox", Parent: inbox.Parent, Folder: true, Trashed: true})
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorRefused) ||
		!strings.Contains(err.Error(), "gone") {
		t.Fatal(err)
	}
	if _, ok := f.local("A.pdf"); !ok {
		t.Fatal("the file went")
	}
	// Another folder at the path now: refused.
	f.replace(porttest.Doc{ID: "f9aaaaaa", Name: "Inbox", Parent: inbox.Parent, Folder: true})
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, port.ErrMirrorRefused) ||
		!strings.Contains(err.Error(), "another folder") {
		t.Fatal(err)
	}
	// A document at the path: not a folder.
	f.spec.Folder = "/File"
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, domain.ErrNotAFolder) {
		t.Fatal(err)
	}
}

// The whole library mirrors too.
func TestPassMirrorsTheTop(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.spec.Folder = "/"
	f.pdfFile("Sub/A.pdf", "%PDF a", 1, time10)
	want(t, f.pass(port.MirrorRunOptions{}), "upload Sub/A.pdf")
	a, _ := f.doc("A")
	if path, _ := f.path(a.ID); path != "/Sub/A" {
		t.Fatalf("A went to %s", path)
	}
	want(t, f.pass(port.MirrorRunOptions{}))
}

// A step that fails is reported, stays pending and stops no other; the
// pass ends in errSteps, which a watch does not back off from.
func TestPassReportsFailedSteps(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", RM: true})
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	f.pdfFile("Locked.pdf", "%PDF locked", 1, time10)
	delete(f.pdf.Counts, filepath.Join(dir, "Locked.pdf"))
	f.renderer.FailIDs = map[domain.ItemID]error{"d1aaaaaa": errBoom}
	p, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if err == nil || !strings.Contains(err.Error(), "2 of 3") || p.Pending != 2 {
		t.Fatalf("%v\n%s", err, actions(p))
	}
	// The drawing is tried again at the next pass.
	f.renderer.FailIDs = nil
	delete(f.pdf.Counts, "x")
	p, _ = f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if !strings.Contains(actions(p), "draw Paper.pdf") {
		t.Fatal(actions(p))
	}
}

// A file that cannot be read is skipped.
func TestPassSkipsUnreadableFiles(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.pdfFile("A.pdf", "%PDF a", 1, time10)
	f.files.Fail["Open"] = errBoom
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "skip A.pdf")
	if !strings.Contains(p.Actions[0].Detail, "cannot be read") {
		t.Fatal(p.Actions[0].Detail)
	}
}

// What the cloud refuses fails the pass as a whole, and the journal
// records why.
func TestPassFailsOnTheCloud(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.cloud.Fail["Root"] = errBoom
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if j := f.journal(); j == nil || !strings.Contains(j["last_error"].(string), "boom") {
		t.Fatalf("journal: %v", j)
	}
	f.cloud.Fail["Root"] = nil
	f.disk.Fail["Scan"] = errBoom
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	f.disk.Fail["Scan"] = nil
	f.files.Fail["Produce"] = errBoom
	if _, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// A folder the mirror would create that the cloud refuses fails that step.
func TestPassFailsToMakeFolders(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.pdfFile("Sub/A.pdf", "%PDF a", 1, time10)
	f.cloud.Fail["CommitRoot"] = errBoom
	p, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if !errors.Is(err, errBoom) || len(p.Actions) != 1 || p.Actions[0].Err == nil {
		t.Fatalf("%v\n%s", err, actions(p))
	}
	// A missing folder the cloud refuses to create fails the pass.
	f = setup(t, true)
	f.spec.Folder = "/Missing"
	f.cloud.Fail["CommitRoot"] = errBoom
	if _, err = f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// Every kind of step fails cleanly when the cloud refuses it.
func TestPassStepsFail(t *testing.T) {
	t.Parallel()
	f := setup(t, true,
		porttest.Doc{ID: "d1aaaaaa", Name: "A", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF a")},
		porttest.Doc{ID: "d2aaaaaa", Name: "B", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF b")},
		porttest.Doc{ID: "d3aaaaaa", Name: "C", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF c")},
		porttest.Doc{ID: "d4aaaaaa", Name: "D", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF d")},
		porttest.Doc{ID: "d5aaaaaa", Name: "E", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF e")},
	)
	for _, n := range []string{"A", "B", "C", "D", "E"} {
		f.pdfFile(n+".pdf", "%PDF "+strings.ToLower(n), 1, time10)
	}
	f.pass(port.MirrorRunOptions{})
	// A trashed locally, B renamed locally, C changed locally.
	if err := f.files.Remove(filepath.Join(dir, "A.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := f.files.Rename(filepath.Join(dir, "B.pdf"), filepath.Join(dir, "B2.pdf")); err != nil {
		t.Fatal(err)
	}
	f.pdfFile("C.pdf", "%PDF c changed", 1, time10.Add(time10s/2))
	f.cloud.Fail["CommitRoot"] = errBoom
	p, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if err == nil || p.Pending != 3 {
		t.Fatalf("%v\n%s", err, actions(p))
	}
	for _, a := range p.Actions {
		if a.Err == nil {
			t.Fatalf("%s did not fail", a.Kind)
		}
	}
}

// A file where a move would go stops that move: nothing is overwritten.
func TestPassNeverOverwrites(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "A", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF a")})
	f.pdfFile("A.pdf", "%PDF a", 1, time10)
	f.pass(port.MirrorRunOptions{})
	f.replace(porttest.Doc{ID: "d1aaaaaa", Name: "B", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF a")})
	f.pdfFile("B.pdf", "%PDF someone else's", 1, time10)
	p, _ := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if got, _ := f.local("B.pdf"); got != "%PDF someone else's" {
		t.Fatalf("B.pdf was overwritten: %q\n%s", got, actions(p))
	}
	if _, ok := f.local("A.pdf"); !ok {
		t.Fatalf("A.pdf went\n%s", actions(p))
	}
}

// A move the file system refuses fails that step only.
func TestPassMoveFails(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "A", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF a")},
		porttest.Doc{ID: "d2aaaaaa", Name: "Z", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF z")})
	f.pdfFile("A.pdf", "%PDF a", 1, time10)
	f.pdfFile("Z.pdf", "%PDF z", 1, time10)
	f.pass(port.MirrorRunOptions{})
	f.replace(porttest.Doc{ID: "d1aaaaaa", Name: "B", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF a")})
	f.files.Fail["Rename"] = errBoom
	p, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if !errors.Is(err, errBoom) {
		t.Fatalf("%v\n%s", err, actions(p))
	}
	f.files.Fail["Rename"] = nil
	f.files.Fail["MkdirAll"] = errBoom
	if _, err = f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	// Deleted on the tablet, the file cannot go aside: the step fails.
	f.files.Fail["MkdirAll"] = nil
	f.drop("d1aaaaaa")
	f.files.Fail["Rename"] = errBoom
	if _, err = f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// Two documents whose drawings would take one name are told apart.
func TestPassKeepsDrawingsApart(t *testing.T) {
	t.Parallel()
	f := setup(t, true,
		porttest.Doc{ID: "d1aaaaaa", Name: "Book", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF a"), RM: true},
		porttest.Doc{ID: "d2aaaaaa", Name: "Book", Parent: "f0aaaaaa", Ext: "epub", Body: []byte("PK\x03\x04b"), RM: true},
	)
	f.pdfFile("Book.pdf", "%PDF a", 1, time10)
	f.epubFile("Book.epub", "b")
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "adopt Book.epub", "adopt Book.pdf", "draw Book.pdf -> _annotated/Book.pdf", "draw Book.epub -> _annotated/Book (d2aaaaaa).pdf")
}

// A cancelled pass stops between steps and records what it did.
func TestPassStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.pdfFile("A.pdf", "%PDF a", 1, time10)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p, err := f.engine.Pass(ctx, f.spec, port.MirrorRunOptions{})
	if err == nil || len(p.Actions) != 0 {
		t.Fatalf("%v %s", err, actions(p))
	}
}

// MaxDeletes bounds one side's deletions however many are followed.
func TestPassGuardsMany(t *testing.T) {
	t.Parallel()
	var docs []porttest.Doc
	for i := range 2*mirror.MaxDeletes + 4 {
		docs = append(docs, porttest.Doc{ID: domain.ItemID(idOf(i)), Name: "doc" + idOf(i), Parent: "f0aaaaaa", Ext: "pdf"})
	}
	f := setup(t, true, docs...)
	for _, d := range docs {
		f.pdfFile(d.Name+".pdf", "%PDF "+d.Name, 1, time10)
	}
	f.pass(port.MirrorRunOptions{})
	for _, d := range docs[:mirror.MaxDeletes+1] {
		f.drop(d.ID)
	}
	p := f.pass(port.MirrorRunOptions{})
	if p.Pending != mirror.MaxDeletes+1 || !strings.Contains(p.Actions[0].Detail, "more than 50 at once") {
		t.Fatalf("%d %s", p.Pending, p.Actions[0].Detail)
	}
}

func idOf(i int) string {
	return strings.Replace("e0000000", "0", string(rune('a'+i%26)), 1) + string(rune('a'+i/26))
}
