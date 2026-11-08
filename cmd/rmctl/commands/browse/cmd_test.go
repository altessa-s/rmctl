// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
		{"top level", []string{"ls"}, "Books\tfolder\tf-books\tfalse\tfalse\nNotes\tnotebook\td-notes\tfalse\tfalse\n"},
		{"folder by path", []string{"ls", "/Books"}, "Novel\tepub\td-novel\ttrue\tfalse\nPaper\tpdf\td-paper\tfalse\tfalse\n"},
		{"document lists itself", []string{"ls", "d-paper"}, "Paper\tpdf\td-paper\tfalse\tfalse\n"},
		{"all includes the trash", []string{"ls", "--all"},
			"Books\tfolder\tf-books\tfalse\tfalse\nNotes\tnotebook\td-notes\tfalse\tfalse\nOld\tpdf\td-old\tfalse\ttrue\n"},
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
	want := "Novel\tepub\t2026-10-31T12:00:00Z\t4205\td-novel\ttrue\tfalse\n"
	if r.Code != 0 || !strings.HasPrefix(r.Out, want) {
		t.Fatalf("%d %q", r.Code, r.Out)
	}
	r = run(t, &clitest.Fake{}, "ls", "-l")
	if !strings.Contains(r.Out, "Books\tfolder\t2026-10-31T12:00:00Z\t-\t") || !strings.Contains(r.Out, "Notes\tnotebook\t-\t") {
		t.Fatalf("a folder's size or a missing time is not a dash: %q", r.Out)
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
	want := "Books\tfolder\tf-books\n  Novel\tepub\td-novel\n  Paper\tpdf\td-paper\nNotes\tnotebook\td-notes\n"
	if r.Code != 0 || r.Out != want {
		t.Fatalf("%d %q", r.Code, r.Out)
	}
	r = run(t, &clitest.Fake{}, "tree", "/Books", "--all")
	if r.Out != "Novel\tepub\td-novel\nPaper\tpdf\td-paper\n" {
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
	for _, id := range []string{"\ta\n", "\ts\n", "\tx\n", "\ty\n"} {
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
	for _, want := range []string{"id\td-novel\n", "path\t/Books/Novel\n", "kind\tepub\n", "tags\tfiction\n", "\n\nd-novel.metadata\t", "d-novel.epub\t4096\t"} {
		if !strings.Contains(r.Out, want) {
			t.Fatalf("missing %q in\n%s", want, r.Out)
		}
	}
	r = run(t, &clitest.Fake{}, "stat", "d-notes")
	if !strings.Contains(r.Out, "modified\t-\n") || !strings.Contains(r.Out, "tags\t-\n") {
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
	if r.Code != 0 || r.Out != "/Books/Novel\tepub\td-novel\ttrue\tfalse\n" {
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
	cmd, env := treeCommand(t, failWriter{}, "plain")
	if err := PrintTree(cmd, env, clitest.Library(), domain.RootID, false); err == nil {
		t.Fatal("a failing writer went unnoticed")
	}
}

// treeCommand is a bare command writing to out in format, for PrintTree.
func treeCommand(tb testing.TB, out io.Writer, format string) (*cobra.Command, *cliutil.Env) {
	tb.Helper()
	env := cliutil.NewEnv(cliutil.Streams{Getenv: func(string) string { return "" }}, cliutil.Fixed(cliutil.Services{}))
	cmd := &cobra.Command{Use: "x"}
	cliutil.AddPersistentFlags(cmd, env)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.ParseFlags([]string{"--format", format}); err != nil {
		tb.Fatal(err)
	}
	return cmd, env
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func BenchmarkPrintTree(b *testing.B) {
	lib := bigLibrary(10_000)
	for _, name := range []string{"plain", "table", "json"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			var out bytes.Buffer
			cmd, env := treeCommand(b, &out, name)
			for b.Loop() {
				out.Reset()
				if err := PrintTree(cmd, env, lib, domain.RootID, false); err != nil {
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

// table runs args in table format without color, as a person at a terminal
// of width columns would see them.
func table(t *testing.T, width int, args ...string) clitest.Result {
	t.Helper()
	opts := clitest.Options{OutTerminal: true, Width: width}
	r := clitest.Exec(t, (&clitest.Fake{}).Services(), opts, Commands, append(args, "--color", "never")...)
	if r.Code != 0 {
		t.Fatalf("%v: %d %v", args, r.Code, r.Error)
	}
	return r
}

func TestTableGoldens(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"ls-long-table": {"ls", "-l", "--all"},
		"ls-table":      {"ls", "/Books"},
		"find-table":    {"find", "--all"},
		"tree-table":    {"tree", "--all"},
		"stat-table":    {"stat", "/Books/Novel"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			clitest.Golden(t, name, table(t, 0, args...).Out)
		})
	}
}

// A terminal is a table by default, --format plain overrides it, and a
// narrow terminal truncates the names only.
func TestTableIsTheTerminalDefault(t *testing.T) {
	t.Parallel()
	if r := table(t, 0, "ls"); !strings.HasPrefix(r.Out, "NAME    KIND      ID\n") {
		t.Fatalf("%q", r.Out)
	}
	if r := table(t, 0, "ls", "--format", "plain"); strings.Contains(r.Out, "NAME") {
		t.Fatalf("%q", r.Out)
	}
	r := table(t, 30, "find", "--all")
	for line := range strings.Lines(r.Out) {
		if w := len([]rune(strings.TrimRight(line, "\n"))); w > 30 {
			t.Fatalf("a line of %d columns in a terminal of 30:\n%s", w, r.Out)
		}
	}
	if !strings.Contains(r.Out, "…") || !strings.Contains(r.Out, "d-novel") {
		t.Fatalf("the path, and only it, is truncated:\n%s", r.Out)
	}
}

func TestColorGolden(t *testing.T) {
	t.Parallel()
	opts := clitest.Options{OutTerminal: true}
	r := clitest.Exec(t, (&clitest.Fake{}).Services(), opts, Commands, "ls", "-l", "--all", "--color", "always")
	if r.Code != 0 {
		t.Fatal(r.Error)
	}
	clitest.Golden(t, "ls-color", r.Out)
	// Automatic color follows the terminal, and NO_COLOR and TERM=dumb turn
	// it off.
	auto := clitest.Exec(t, (&clitest.Fake{}).Services(), opts, Commands, "ls")
	if !strings.Contains(auto.Out, "\x1b[") {
		t.Fatalf("no color on a terminal: %q", auto.Out)
	}
	for _, env := range []map[string]string{{"NO_COLOR": "1"}, {"TERM": "dumb"}} {
		r := clitest.Exec(t, (&clitest.Fake{}).Services(), clitest.Options{OutTerminal: true, Env: env}, Commands, "ls")
		if strings.Contains(r.Out, "\x1b[") {
			t.Fatalf("%v: color %q", env, r.Out)
		}
	}
}

func TestComplete(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"__complete", "ls", ""}, "/Books/\tfolder\n/Notes\tnotebook\n:6\n"},
		{[]string{"__complete", "ls", "-a", "/O"}, "/Old\tpdf, in the trash\n:4\n"},
		{[]string{"__complete", "ls", "/O"}, ":4\n"},
		{[]string{"__complete", "ls", "/Books", ""}, ":4\n"},
		{[]string{"__complete", "tree", "/"}, "/Books/\tfolder\n:6\n"},
		{[]string{"__complete", "stat", "d-n"}, "d-novel\t/Books/Novel\nd-notes\t/Notes\n:4\n"},
		{[]string{"__complete", "find", ""}, ":4\n"},
		{[]string{"__complete", "find", "--under", "/"}, "/Books/\tfolder\n:6\n"},
		{[]string{"__complete", "du", "/"}, "/Books/\tfolder\n:6\n"},
		{[]string{"__complete", "dupes", "/B"}, "/Books/\tfolder\n:6\n"},
		{[]string{"__complete", "text", "/Books/P"}, "/Books/Paper\tpdf\n:4\n"},
		{[]string{"__complete", "text", "./"}, "rmdoc\n:8\n"},
		{[]string{"__complete", "highlights", "~/backup/"}, "rmdoc\n:8\n"},
	} {
		if r := run(t, &clitest.Fake{}, tc.args...); r.Code != 0 || r.Out != tc.want {
			t.Errorf("%v: %d %q", tc.args, r.Code, r.Out)
		}
	}
}
