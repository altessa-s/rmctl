// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func buildOpen(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{cliutil.WithAccess(New(env), cliutil.AccessOffline), cliutil.WithAccess(NewOpen(env), cliutil.AccessOffline)}
}

func runOpen(t *testing.T, f *clitest.Fake, opts clitest.Options, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, f.Services(), opts, buildOpen, args...)
}

func TestOpen(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Opened: port.OpenReport{RenderReport: port.RenderReport{Path: "/c/rendered/k/Notes-rendered.pdf", Pages: 3}, Drawn: true}}
	r := runOpen(t, f, clitest.Options{}, "open", "/Notes")
	want := "path\t/c/rendered/k/Notes-rendered.pdf\ndrawn\ttrue\npages\t3\nunreadable_pages\t0\n"
	if r.Code != 0 || r.Out != want || r.Err != "" || f.Calls()[0] != "Open /Notes  format=pdf page=0 mode=fresh grain=mask paper=a4 scale=0 quality=0" {
		t.Fatalf("%d %q %q %v", r.Code, r.Out, r.Err, f.Calls())
	}
	f = &clitest.Fake{Opened: port.OpenReport{RenderReport: port.RenderReport{Path: "/c/rendered/k/Notes-p2.svg"}}}
	r = runOpen(t, f, clitest.Options{}, "open", "d-notes", "--page", "2", "--format", "svg", "--paper", "letter", "--no-template", "--json")
	if r.Code != 0 || f.Calls()[0] != "Open d-notes  format=svg page=2 mode= grain= paper=letter scale=0 quality=0 no-template" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	clitest.Golden(t, "open-json", r.Out)
	// What a fresh drawing could not keep is said on standard error.
	f = &clitest.Fake{Opened: port.OpenReport{RenderReport: port.RenderReport{Path: "x.pdf", UnreadablePages: 2}, Drawn: true}}
	if r = runOpen(t, f, clitest.Options{}, "open", "x", "--unreadable-pages", "blank"); r.Code != 0 ||
		!strings.Contains(r.Err, "2 pages do not parse and are drawn as pages nobody wrote on") {
		t.Fatalf("%d %q", r.Code, r.Err)
	}
}

func TestOpenLocalArchive(t *testing.T) {
	t.Parallel()
	local := filepath.Join(t.TempDir(), "backup.rmdoc")
	if err := os.WriteFile(local, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &clitest.Fake{}
	r := runOpen(t, f, clitest.Options{}, "open", local, "--id", "3f2a")
	if r.Code != 0 || f.Calls()[0] != "OpenFile "+local+"  format=pdf page=0 mode=fresh grain=mask paper=a4 scale=0 quality=0 id=3f2a" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	if r = runOpen(t, &clitest.Fake{}, clitest.Options{}, "open", "/Notes", "--id", "3f2a"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestOpenUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"open"},
		{"open", "a", "b"},
		{"open", "x", "-o", "x.pdf"},
		{"open", "x", "--all"},
		{"open", "x", "--format", "png"},
		{"open", "x", "--page", "1", "--format", "pdf"},
		{"open", "x", "--paper", "a3"},
		{"open", "x", "--mode", "draft"},
	} {
		f := &clitest.Fake{}
		if r := runOpen(t, f, clitest.Options{}, args...); r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
			t.Fatalf("%v: %d %v %v", args, r.Code, r.Error, f.Calls())
		}
	}
	if r := runOpen(t, &clitest.Fake{}, clitest.Options{}, "open", "x", "--paper", "a3"); !strings.HasPrefix(r.Error.Error(), "open: unknown --paper") {
		t.Fatal(r.Error)
	}
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	r := runOpen(t, &clitest.Fake{RenderErr: domain.ErrRenderLite}, clitest.Options{}, "open", "x")
	if r.Code != cliutil.CodeError || r.Out != "" || r.Error.Error() != "open: rendering is not in rmctl-lite: install rmctl, the full build, for it" {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	// A drawing the viewer could not show is the answer, and an error.
	f := &clitest.Fake{OpenErr: errors.New("no viewer"), Opened: port.OpenReport{RenderReport: port.RenderReport{Path: "/c/x.pdf"}, Drawn: true}}
	r = runOpen(t, f, clitest.Options{}, "open", "x")
	if r.Code != cliutil.CodeError || !strings.HasPrefix(r.Out, "path\t/c/x.pdf\n") ||
		r.Error.Error() != "open: the drawing is at /c/x.pdf, but it could not be shown: no viewer" {
		t.Fatalf("%d %q %v", r.Code, r.Out, r.Error)
	}
	if r = runOpen(t, &clitest.Fake{Err: errors.New("boom")}, clitest.Options{}, "open", "x"); r.Code != cliutil.CodeError {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r = clitest.Exec(t, cliutil.Services{}, clitest.Options{}, buildOpen, "open", "x"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}

func TestRenderAndOpenComplete(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"__complete", "open", "/B"}, "/Books/\tfolder\n:6\n"},
		{[]string{"__complete", "open", "/Books/"}, "/Books/Novel\tepub\n/Books/Paper\tpdf\n:4\n"},
		{[]string{"__complete", "open", "./"}, "rmdoc\n:8\n"},
		{[]string{"__complete", "open", "x", ""}, ":4\n"},
		{[]string{"__complete", "open", "x", "--paper", ""}, "a4\nletter\ntablet\n:4\n"},
		{[]string{"__complete", "open", "x", "--unreadable-pages", ""}, "placeholder\nblank\nstrict\n:4\n"},
		{[]string{"__complete", "render", "/"}, "/Books/\tfolder\n/Notes\tnotebook\n:6\n"},
		{[]string{"__complete", "render", "--all", "/"}, "/Old\tpdf, in the trash\n"},
		{[]string{"__complete", "render", "x", "--mode", ""}, "fresh\npreserving\nrecomposed\n:4\n"},
		{[]string{"__complete", "render", "x", "--grain", ""}, "mask\ngroup\n:4\n"},
		{[]string{"__complete", "render", "x", "--format", ""}, "pdf\npng\njpg\nsvg\n:4\n"},
	} {
		r := runOpen(t, &clitest.Fake{}, clitest.Options{}, tc.args...)
		if r.Code != 0 || !strings.Contains(r.Out, tc.want) {
			t.Errorf("%v: %d %q", tc.args, r.Code, r.Out)
		}
	}
}
