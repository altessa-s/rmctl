// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
)

func run(t *testing.T, f *clitest.Fake, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, f.Services(), clitest.Options{}, Commands, args...)
}

func TestLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"top level", []string{"ls"}, "Books/  f-books\nNotes [notebook]  d-notes\n"},
		{"folder by path", []string{"ls", "/Books"}, "Novel [epub] *  d-novel\nPaper [pdf]  d-paper\n"},
		{"document lists itself", []string{"ls", "d-paper"}, "Paper [pdf]  d-paper\n"},
		{"all includes the trash", []string{"ls", "--all"}, "Books/  f-books\nNotes [notebook]  d-notes\nOld [pdf] (trashed)  d-old\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := run(t, &clitest.Fake{}, tc.args...)
			if r.Code != 0 || r.Out != tc.want || r.Err != "" {
				t.Fatalf("code %d err %v\nout %q\nerr %q", r.Code, r.Error, r.Out, r.Err)
			}
		})
	}
}

func TestLsLong(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{}, "ls", "-l", "/Books")
	want := "2026-10-31 12:00   4.1 KiB  Novel [epub] *  d-novel\n"
	if r.Code != 0 || !strings.HasPrefix(r.Out, want) {
		t.Fatalf("%d %q", r.Code, r.Out)
	}
	r = run(t, &clitest.Fake{}, "ls", "-l")
	if !strings.Contains(r.Out, "-                  ") {
		t.Fatalf("a missing time is not a dash: %q", r.Out)
	}
}

func TestLsJSON(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{}, "ls", "--json", "/Books")
	if r.Code != 0 {
		t.Fatal(r.Error)
	}
	clitest.Golden(t, "ls", r.Out)
}

func TestTree(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{}, "tree")
	want := "Books/  f-books\n  Novel [epub] *  d-novel\n  Paper [pdf]  d-paper\nNotes [notebook]  d-notes\n"
	if r.Code != 0 || r.Out != want {
		t.Fatalf("%d %q", r.Code, r.Out)
	}
	r = run(t, &clitest.Fake{}, "tree", "/Books", "--all")
	if r.Out != "Novel [epub] *  d-novel\nPaper [pdf]  d-paper\n" {
		t.Fatalf("%q", r.Out)
	}
	r = run(t, &clitest.Fake{}, "tree", "--json")
	if r.Code != 0 {
		t.Fatal(r.Error)
	}
	clitest.Golden(t, "tree", r.Out)
}

func TestTreeNotesOddities(t *testing.T) {
	t.Parallel()
	lib := clitest.Tree(
		clitest.Spec{ID: "a", Name: "A", Kind: domain.KindPDF},
		clitest.Spec{ID: "a", Name: "A again", Kind: domain.KindPDF},
		clitest.Spec{ID: "s", Name: "Stray", Parent: "gone", Kind: domain.KindPDF},
		clitest.Spec{ID: "x", Name: "X", Parent: "y", Kind: domain.KindFolder},
		clitest.Spec{ID: "y", Name: "Y", Parent: "x", Kind: domain.KindFolder},
	)
	r := run(t, &clitest.Fake{Library: lib}, "tree")
	if r.Code != 0 {
		t.Fatal(r.Error)
	}
	if !strings.Contains(r.Err, "1 items repeat an identifier") || !strings.Contains(r.Err, "items name a parent that leads nowhere") {
		t.Fatalf("notes %q", r.Err)
	}
	for _, id := range []string{"  a\n", "  s\n", "  x\n", "  y\n"} {
		if strings.Count(r.Out, id) != 1 {
			t.Fatalf("%q not shown exactly once:\n%s", id, r.Out)
		}
	}
}

func TestTreeRefusesADocument(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{}, "tree", "/Books/Paper")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, domain.ErrNotAFolder) {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestStat(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{}, "stat", "/Books/Novel")
	for _, want := range []string{"id        d-novel\n", "path      /Books/Novel\n", "kind      epub\n", "tags      [fiction]\n", "file      d-novel.epub"} {
		if !strings.Contains(r.Out, want) {
			t.Fatalf("missing %q in\n%s", want, r.Out)
		}
	}
	r = run(t, &clitest.Fake{}, "stat", "d-notes")
	if !strings.Contains(r.Out, "modified  -\n") || !strings.Contains(r.Out, "tags      -\n") {
		t.Fatalf("%s", r.Out)
	}
	r = run(t, &clitest.Fake{}, "stat", "--json", "d-paper")
	if r.Code != 0 {
		t.Fatal(r.Error)
	}
	clitest.Golden(t, "stat", r.Out)
}

func TestFind(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, "find", "Novel|Paper", "--tag", "fiction", "--starred", "--under", "/Books", "--all")
	if r.Code != 0 || r.Out != "/Books/Novel  d-novel\n" {
		t.Fatalf("%d %q %v", r.Code, r.Out, r.Error)
	}
	if got := f.Calls()[0]; got != "Find Novel|Paper fiction starred /Books all" {
		t.Fatalf("query %q", got)
	}
	r = run(t, &clitest.Fake{}, "find", "--json", "Paper")
	if r.Code != 0 {
		t.Fatal(r.Error)
	}
	clitest.Golden(t, "find", r.Out)
}

func TestErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for _, args := range [][]string{{"ls"}, {"tree"}, {"stat", "x"}, {"find"}} {
		r := run(t, &clitest.Fake{Err: boom}, args...)
		if r.Code != cliutil.CodeError || !errors.Is(r.Error, boom) || !strings.HasPrefix(r.Error.Error(), args[0]+": ") {
			t.Fatalf("%v: %d %v", args, r.Code, r.Error)
		}
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"ls", "a", "b"}, {"stat"}, {"tree", "a", "b"}, {"find", "a", "b"}, {"ls", "--bogus"}} {
		r := run(t, &clitest.Fake{}, args...)
		if r.Code != cliutil.CodeUsage {
			t.Fatalf("%v: code %d %v", args, r.Code, r.Error)
		}
	}
}

func TestNoService(t *testing.T) {
	t.Parallel()
	r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, "ls")
	if !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}

func TestPrintTreeFailingWriter(t *testing.T) {
	t.Parallel()
	err := PrintTree(failWriter{}, &bytes.Buffer{}, clitest.Library(), domain.RootID, false, false)
	if err == nil {
		t.Fatal("a failing writer went unnoticed")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func BenchmarkPrintTree(b *testing.B) {
	lib := bigLibrary(10_000)
	for _, asJSON := range []bool{false, true} {
		name := "text"
		if asJSON {
			name = "json"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			var out bytes.Buffer
			for b.Loop() {
				out.Reset()
				if err := PrintTree(&out, &bytes.Buffer{}, lib, domain.RootID, false, asJSON); err != nil {
					b.Fatal(err)
				}
			}
			b.SetBytes(int64(out.Len()))
		})
	}
}

// bigLibrary is n items: a tenth folders, each holding nine documents.
func bigLibrary(n int) *domain.Tree {
	specs := make([]clitest.Spec, 0, n)
	for i := range n {
		folder := "f" + strconv.Itoa(i/10*10)
		if i%10 == 0 {
			specs = append(specs, clitest.Spec{ID: folder, Name: "Folder " + strconv.Itoa(i), Kind: domain.KindFolder})
			continue
		}
		specs = append(specs, clitest.Spec{ID: "d" + strconv.Itoa(i), Name: "Document " + strconv.Itoa(i), Parent: folder, Kind: domain.KindPDF})
	}
	return clitest.Tree(specs...)
}
