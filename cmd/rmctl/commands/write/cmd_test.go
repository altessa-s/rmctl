// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

func run(t *testing.T, f *clitest.Fake, opts clitest.Options, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, f.Services(), opts, Commands, args...)
}

func TestCommandsWithYes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args       []string
		call, want string
	}{
		{[]string{"mkdir", "/Papers/2026", "-p"}, "Mkdir /Papers/2026 parents", "2026/  f-new\n"},
		{[]string{"put", "a.pdf", "/Papers", "--name", "A", "--tag", "x", "--tag", "y"}, "Put a.pdf /Papers A x,y", "Uploaded [pdf]  d-new\n"},
		{[]string{"put", "a.pdf"}, "Put a.pdf /", "Uploaded [pdf]  d-new\n"},
		{[]string{"mv", "d-paper", "/"}, "Move d-paper /", "Paper [pdf]  d-paper\n"},
		{[]string{"trash", "d-paper", "d-notes"}, "Trash d-paper d-notes", "Paper [pdf]  d-paper\nNotes [notebook]  d-notes\n"},
		{[]string{"restore", "d-old"}, "Restore d-old", "Old [pdf]  d-old\n"},
		{[]string{"restore", "d-old", "--to", "/Books"}, "Restore d-old to=/Books", "Old [pdf]  d-old\n"},
		{[]string{"rm", "f-books"}, "Remove f-books", "Books/  f-books\n"},
		{[]string{"mput", "dir", "/Papers", "--jobs", "2"}, "Upload dir /Papers", "written 0, skipped 0, deleted 0\n"},
		{[]string{"mput", "dir", "/Papers", "--incremental"}, "Upload dir /Papers incremental", "written 0, skipped 0, deleted 0\n"},
		{[]string{"mput", "dir"}, "Upload dir /", "written 0, skipped 0, deleted 0\n"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			t.Parallel()
			f := &clitest.Fake{}
			r := run(t, f, clitest.Options{}, append(tc.args, "--yes")...)
			calls := f.Calls()
			if r.Code != 0 || r.Out != tc.want || calls[len(calls)-1] != tc.call {
				t.Fatalf("code %d err %v out %q calls %v", r.Code, r.Error, r.Out, calls)
			}
		})
	}
}

func TestRefusedWithoutYesOffATerminal(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"mkdir", "/x"}, {"put", "a.pdf"}, {"mv", "a", "b"}, {"trash", "a"}, {"restore", "a"}, {"rm", "d-paper"}, {"mput", "d"},
	} {
		f := &clitest.Fake{}
		r := run(t, f, clitest.Options{}, args...)
		if r.Code != cliutil.CodeUsage || !strings.Contains(r.Error.Error(), "--yes") {
			t.Fatalf("%v: %d %v", args, r.Code, r.Error)
		}
		for _, c := range f.Calls() {
			if c != "Tree" {
				t.Fatalf("%v changed the library: %v", args, f.Calls())
			}
		}
	}
}

func TestConfirmOnATerminal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		answer string
		ok     bool
	}{{"y\n", true}, {"YES\n", true}, {"n\n", false}, {"\n", false}, {"", false}} {
		f := &clitest.Fake{}
		r := run(t, f, clitest.Options{Terminal: true, Stdin: tc.answer}, "mkdir", "/x")
		if !strings.HasPrefix(r.Err, "Create the folder /x? [y/N] ") {
			t.Fatalf("prompt %q", r.Err)
		}
		if tc.ok != (r.Code == 0) || (!tc.ok && !errors.Is(r.Error, cliutil.ErrDeclined)) {
			t.Fatalf("%q: %d %v", tc.answer, r.Code, r.Error)
		}
	}
}

func TestRmNamesTheDescendants(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, clitest.Options{Terminal: true, Stdin: "n\n"}, "rm", "f-books", "d-notes")
	if !strings.Contains(r.Err, "Delete 2 items for good — 4 items with everything below them?") {
		t.Fatalf("%q", r.Err)
	}
	f = &clitest.Fake{}
	r = run(t, f, clitest.Options{Terminal: true, Stdin: "n\n"}, "rm", "nowhere")
	if !strings.Contains(r.Err, "Delete nowhere for good? [y/N]") {
		t.Fatalf("%q", r.Err)
	}
}

func TestPutProgress(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Progress: [2]int64{10, -1}}
	r := run(t, f, clitest.Options{Terminal: true, Stdin: "y\n"}, "put", "a.pdf")
	if !strings.Contains(r.Err, "\ruploading a.pdf 10 B\n") {
		t.Fatalf("%q", r.Err)
	}
}

func TestJSON(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{}, clitest.Options{}, "trash", "d-paper", "--yes", "--json")
	clitest.Golden(t, "trash", r.Out)
}

func TestErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		code int
	}{{errors.New("boom"), cliutil.CodeError}, {port.ErrRootConflict, cliutil.CodeConflict}, {port.ErrNotRegistered, cliutil.CodeNotRegistered}} {
		for _, args := range [][]string{
			{"mkdir", "/x"}, {"put", "a.pdf"}, {"mv", "a", "b"}, {"trash", "a"}, {"restore", "a"}, {"rm", "a"}, {"mput", "d"},
		} {
			r := run(t, &clitest.Fake{Err: tc.err}, clitest.Options{}, append(args, "--yes")...)
			if r.Code != tc.code || !strings.HasPrefix(r.Error.Error(), args[0]+": ") {
				t.Fatalf("%v: %d %v", args, r.Code, r.Error)
			}
		}
	}
	for _, args := range [][]string{{"mkdir"}, {"put"}, {"mv", "a"}, {"trash"}, {"rm"}, {"mput"}, {"put", "a", "b", "c"}} {
		if r := run(t, &clitest.Fake{}, clitest.Options{}, args...); r.Code != cliutil.CodeUsage {
			t.Fatalf("%v: %d", args, r.Code)
		}
	}
	if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, "mkdir", "x", "--yes"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}

func TestCompletion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"__complete", "put", ""}, "pdf\nepub\n"},
		{[]string{"__complete", "put", "a.pdf", "/"}, "/Books/\n"},
		{[]string{"__complete", "mput", ""}, ":16\n"},
		{[]string{"__complete", "mput", "d", "/"}, "/Books/\n"},
	} {
		r := run(t, &clitest.Fake{}, clitest.Options{}, tc.args...)
		if r.Code != 0 || !strings.Contains(r.Out, tc.want) {
			t.Fatalf("%v: %d %q", tc.args, r.Code, r.Out)
		}
	}
}

func TestRmWithoutALibrary(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Err: errors.New("offline")}
	r := run(t, f, clitest.Options{Terminal: true, Stdin: "n\n"}, "rm", "x")
	if !strings.Contains(r.Err, "Delete x for good? [y/N]") || !errors.Is(r.Error, cliutil.ErrDeclined) {
		t.Fatalf("%q %v", r.Err, r.Error)
	}
	r = clitest.Exec(t, cliutil.Services{Writer: f}, clitest.Options{Terminal: true, Stdin: "n\n"}, Commands, "rm", "x")
	if !strings.Contains(r.Err, "Delete x for good? [y/N]") {
		t.Fatalf("%q", r.Err)
	}
}

func TestPrintItemsFailingWriter(t *testing.T) {
	t.Parallel()
	cmd := Commands(cliutil.NewEnv(cliutil.Streams{}, cliutil.Fixed(cliutil.Services{})))[0]
	cliutil.AddPersistentFlags(cmd)
	cmd.SetOut(failWriter{})
	if printItems(cmd, clitest.Item(clitest.Spec{ID: "a", Name: "A"})) == nil {
		t.Fatal("a failing writer went unnoticed")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
