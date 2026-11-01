// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clitest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestItemKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.Kind{domain.KindFolder, domain.KindPDF, domain.KindAnnotatedPDF, domain.KindEPUB, domain.KindNotebook} {
		if got := Item(Spec{ID: "x", Name: "X", Kind: kind}).Kind; got != kind {
			t.Errorf("%s built as %s", kind, got)
		}
	}
	if !Library().IsTrashed("d-old") {
		t.Fatal("the trashed spec is not in the trash")
	}
}

func TestFakeAnswersEveryPort(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var progressed bool
	progress := func(int64, int64) { progressed = true }
	f := &Fake{Path: "p", Progress: [2]int64{1, 2}, Events: []port.Event{{Kind: port.EventConnected}}, WatchErr: errors.New("end")}
	_ = f.Register(ctx, "c", true)
	_ = f.Logout(ctx, false)
	_, _ = f.Info(ctx)
	_, _ = f.Tree(ctx)
	for _, q := range []string{"/", "/Books", "d-paper", "/Nope", "nothing-at-all"} {
		_, _, _ = f.List(ctx, q, false)
	}
	_, _, _ = f.Stat(ctx, "d-paper")
	if _, _, err := f.Stat(ctx, "/Nope"); err == nil {
		t.Fatal("a missing item was found")
	}
	_, _, _ = f.Find(ctx, port.FindQuery{Pattern: "x", Starred: true})
	if _, _, err := f.Find(ctx, port.FindQuery{Pattern: "("}); err == nil {
		t.Fatal("a bad pattern was accepted")
	}
	if p, _ := f.Get(ctx, "x", "", progress); p != "p" || !progressed {
		t.Fatal("Get")
	}
	if r, _ := f.Export(ctx, []string{"x", "z"}, "y"); r.Documents != 2 || r.Path != "p" {
		t.Fatalf("Export %+v", r)
	}
	f.Imported = []port.ImportedDocument{{From: "a"}}
	if got, err := f.Import(ctx, "a.rmdoc", "/"); err != nil || len(got) != 1 {
		t.Fatalf("Import %v %v", got, err)
	}
	f.PutErr = map[string]error{"bad.rmdoc": errors.New("bad")}
	if _, err := f.Import(ctx, "bad.rmdoc", "/"); err == nil {
		t.Fatal("Import of a failing file succeeded")
	}
	svc := f.Services()
	f.Inspected = port.ArchiveReport{Documents: []port.ArchiveDocumentReport{{ID: "n"}}}
	if r, err := svc.Archives.Inspect(ctx, "a.rmdoc"); err != nil || r.Path != "a.rmdoc" || len(r.Documents) != 1 {
		t.Fatalf("Inspect %+v %v", r, err)
	}
	f.Local = []port.DocumentText{{}}
	if d, err := svc.Archives.Pages(ctx, "a.rmdoc", []string{"1"}); err != nil || len(d) != 1 {
		t.Fatalf("Pages %v %v", d, err)
	}
	f.ArchiveErr = errors.New("unsupported")
	if _, err := f.Import(ctx, "a.rmdoc", "/"); err == nil {
		t.Fatal("ArchiveErr did not fail Import")
	}
	f.ArchiveErr = nil
	f.PutErr = nil
	_, _ = f.Mirror(ctx, "x", "y", port.MirrorOptions{Incremental: true})
	_, _ = f.Mkdir(ctx, "/a/b", true)
	_, _ = f.Put(ctx, "a", "/", port.PutOptions{}, nil)
	_, _ = f.Move(ctx, "a", "b")
	_, _ = f.Trash(ctx, []string{"d-paper"})
	_, _ = f.Restore(ctx, []string{"d-old"}, "")
	_, _ = f.Restore(ctx, []string{"d-old"}, "/Books")
	_, _ = f.Remove(ctx, []string{"f-books"})
	_, _ = f.Upload(ctx, "d", "/", port.UploadOptions{Incremental: true})
	if err := f.Watch(ctx, func(port.Event) {}); err == nil {
		t.Fatal("Watch")
	}
	_, _ = f.Root(ctx)
	_, _ = f.Blob(ctx, "h", "n")
	if p, err := f.SaveBlob(ctx, "h", "n", "out"); p != "out" || err != nil {
		t.Fatalf("SaveBlob %q %v", p, err)
	}
	_, _, _ = f.Files(ctx, "a")
	_, _ = f.Hashes(ctx)
	_, _ = f.CheckFiles(ctx)
	_, _ = f.Sweep(ctx)
	if len(f.Calls()) < 25 || !strings.HasPrefix(f.Calls()[0], "Register c replace") {
		t.Fatalf("%v", f.Calls())
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := (&Fake{}).Watch(canceled, func(port.Event) {}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	failing := &Fake{Err: errors.New("x")}
	for _, call := range []func() error{
		func() error { _, err := failing.Tree(ctx); return err },
		func() error { _, _, err := failing.List(ctx, "/", false); return err },
		func() error { _, _, err := failing.Stat(ctx, "x"); return err },
		func() error { _, _, err := failing.Find(ctx, port.FindQuery{}); return err },
		func() error { return failing.Watch(ctx, nil) },
	} {
		if call() == nil {
			t.Fatal("Err ignored")
		}
	}
}

func TestGolden(t *testing.T) {
	t.Parallel()
	Golden(t, "sample", "golden\n")
}

func TestExec(t *testing.T) {
	t.Parallel()
	f := &Fake{Library: Library()}
	build := func(env *cliutil.Env) []*cobra.Command {
		return []*cobra.Command{{
			Use: "hello", GroupID: "g",
			RunE: func(cmd *cobra.Command, _ []string) error {
				svc, err := env.Services(cmd)
				if err != nil {
					return err
				}
				_, err = svc.Browser.Tree(cmd.Context())
				_, _ = cmd.OutOrStdout().Write([]byte("hi\n"))
				return err
			},
		}}
	}
	r := Exec(t, f.Services(), Options{}, build, "hello")
	if r.Code != 0 || r.Out != "hi\n" || f.Calls()[0] != "Tree" {
		t.Fatalf("%+v", r)
	}
	closing := func(context.Context, string, bool) (cliutil.Services, func() error, error) {
		return f.Services(), func() error { return errors.New("close") }, nil
	}
	if r := Exec(t, cliutil.Services{}, Options{Factory: closing}, build, "hello"); r.Code != cliutil.CodeError {
		t.Fatalf("%+v", r)
	}
	if (&Fake{Path: ""}).answerPath("d") != "d" {
		t.Fatal("answerPath")
	}
}

func TestGoldenMismatch(t *testing.T) {
	t.Parallel()
	rec := &recorder{TB: t}
	Golden(rec, "sample", "other\n")
	Golden(rec, "missing", "x")
	if rec.errors != 1 || rec.fatals != 1 {
		t.Fatalf("errors %d fatals %d", rec.errors, rec.fatals)
	}
}

// recorder counts failures instead of failing the test.
type recorder struct {
	testing.TB
	errors, fatals int
}

func (r *recorder) Helper()               {}
func (r *recorder) Errorf(string, ...any) { r.errors++ }
func (r *recorder) Fatalf(string, ...any) { r.fatals++ }

func TestFakePages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := port.DocumentText{Pages: []domain.Page{{ID: "p", Number: 1}}}
	f := &Fake{Document: doc, Unmarked: port.UnmarkReport{Erased: 2}, PutErr: map[string]error{"bad.md": errors.New("bad")}}
	if got, err := f.Pages(ctx, "q", []string{"1"}); err != nil || len(got.Pages) != 1 {
		t.Fatal(got, err)
	}
	if got, err := f.Unmark(ctx, "q", nil); err != nil || got.Erased != 2 {
		t.Fatal(got, err)
	}
	if it, err := f.Notebook(ctx, "a.md", "/", port.NotebookOptions{Device: domain.DeviceMove}); err != nil || it.Kind != domain.KindNotebook {
		t.Fatal(it, err)
	}
	if _, err := f.Notebook(ctx, "bad.md", "/", port.NotebookOptions{}); err == nil {
		t.Fatal("PutErr ignored")
	}
	if _, err := f.Put(ctx, "bad.md", "/", port.PutOptions{}, nil); err == nil {
		t.Fatal("PutErr ignored by Put")
	}
	want := []string{"Pages q 1", "Unmark q", "Notebook a.md /  move", "Notebook bad.md /", "Put bad.md /"}
	if !slices.Equal(f.Calls(), want) {
		t.Fatalf("%q", f.Calls())
	}
	for _, failing := range []*Fake{{PagesErr: domain.ErrPagesUnsupported}, {Err: errors.New("down")}} {
		if _, err := failing.Pages(ctx, "q", nil); err == nil {
			t.Fatal("no error")
		}
		if _, err := failing.Unmark(ctx, "q", nil); err == nil {
			t.Fatal("no error")
		}
		if _, err := failing.Notebook(ctx, "a.md", "/", port.NotebookOptions{}); err == nil {
			t.Fatal("no error")
		}
	}
}
