// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func run(t *testing.T, f *clitest.Fake, opts clitest.Options, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, f.Services(), opts, Commands, args...)
}

func TestGet(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Path: "Novel.epub"}
	r := run(t, f, clitest.Options{}, "get", "/Books/Novel")
	if r.Code != 0 || r.Out != "Novel.epub\n" || f.Calls()[0] != "Get /Books/Novel" {
		t.Fatalf("%d %q %v", r.Code, r.Out, f.Calls())
	}
	f = &clitest.Fake{}
	r = run(t, f, clitest.Options{}, "get", "d-paper", "-o", "out.pdf", "--json")
	if r.Out != "{\n  \"path\": \"out.pdf\"\n}\n" || f.Calls()[0] != "Get d-paper out.pdf" {
		t.Fatalf("%q %v", r.Out, f.Calls())
	}
}

func TestGetProgressOnlyOnATerminal(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Path: "x", Progress: [2]int64{50, 100}}
	if r := run(t, f, clitest.Options{}, "get", "x"); r.Err != "" {
		t.Fatalf("progress off a terminal: %q", r.Err)
	}
	f = &clitest.Fake{Path: "x", Progress: [2]int64{50, 100}}
	if r := run(t, f, clitest.Options{Terminal: true}, "get", "x"); r.Err != "\rdownloading  50%\n" {
		t.Fatalf("%q", r.Err)
	}
}

func TestExport(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, clitest.Options{}, "export", "d-notes", "-o", "n.rmdoc")
	if r.Code != 0 || r.Out != "path\tn.rmdoc\ndocuments\t1\nfiles\t3\n" || f.Calls()[0] != "Export d-notes n.rmdoc" {
		t.Fatalf("%d %q %v", r.Code, r.Out, f.Calls())
	}
	// Several queries, a folder among them, into one archive.
	f = &clitest.Fake{}
	r = run(t, f, clitest.Options{}, "export", "/Books", "d-notes", "-o", "b.rmdoc", "--json")
	if r.Code != 0 || f.Calls()[0] != "Export /Books d-notes b.rmdoc" {
		t.Fatalf("%d %q %v", r.Code, r.Out, f.Calls())
	}
	clitest.Golden(t, "export", r.Out)
	r = run(t, &clitest.Fake{ArchiveErr: domain.ErrArchivesUnsupported, Err: domain.ErrArchivesUnsupported}, clitest.Options{}, "export", "x", "-o", "y")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "rebuild with cgo") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r := run(t, &clitest.Fake{}, clitest.Options{}, "export", "d-notes"); r.Code != cliutil.CodeUsage {
		t.Fatalf("export without -o: %d %v", r.Code, r.Error)
	}
}

func TestMget(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Report: port.MirrorReport{Written: 3, Skipped: 2, Deleted: 1}}
	r := run(t, f, clitest.Options{}, "mget", "/Books", "-o", "dir", "--incremental", "--delete", "--jobs", "4")
	if r.Code != 0 || r.Out != "written\t3\nskipped\t2\ndeleted\t1\n" || f.Calls()[0] != "Mirror /Books dir incremental delete" {
		t.Fatalf("%d %q %v", r.Code, r.Out, f.Calls())
	}
	r = run(t, f, clitest.Options{}, "mget", "/Books", "-o", "dir", "--json")
	clitest.Golden(t, "mget", r.Out)
}

func TestErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for _, args := range [][]string{{"get", "x"}, {"export", "x", "-o", "y"}, {"mget", "x", "-o", "y"}} {
		r := run(t, &clitest.Fake{Err: boom}, clitest.Options{}, args...)
		if r.Code != cliutil.CodeError || !strings.HasPrefix(r.Error.Error(), args[0]+": ") {
			t.Fatalf("%v: %d %v", args, r.Code, r.Error)
		}
	}
	for _, args := range [][]string{{"get"}, {"get", "a", "b"}, {"mget", "x"}, {"mget", "x", "-o", "y", "--jobs", "n"}} {
		if r := run(t, &clitest.Fake{}, clitest.Options{}, args...); r.Code != cliutil.CodeUsage {
			t.Fatalf("%v: %d %v", args, r.Code, r.Error)
		}
	}
	if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, "get", "x"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}
