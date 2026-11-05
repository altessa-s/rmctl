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

func build(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{cliutil.WithAccess(New(env), cliutil.AccessOffline)}
}

func run(t *testing.T, f *clitest.Fake, opts clitest.Options, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, f.Services(), opts, build, args...)
}

func TestRenderPDF(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Rendered: port.RenderReport{Path: "Notes-rendered.pdf", Pages: 3}}
	r := run(t, f, clitest.Options{}, "render", "/Notes")
	want := "path\tNotes-rendered.pdf\npages\t3\nskipped_blocks\t0\nrefused_pages\t0\nlinks_dropped\t0\n"
	if r.Code != 0 || r.Out != want || r.Err != "" || f.Calls()[0] != "Render /Notes  format=pdf page=0 mode=fresh paper=a4 scale=0 quality=0" {
		t.Fatalf("%d %q %q %v", r.Code, r.Out, r.Err, f.Calls())
	}
	f = &clitest.Fake{Rendered: port.RenderReport{Pages: 12, SkippedBlocks: 2, RefusedPages: 1, LinksDropped: 3}}
	r = run(t, f, clitest.Options{}, "render", "d-paper", "-o", "out.pdf", "--mode", "Recomposed", "--paper", "letter", "--json")
	if r.Code != 0 || f.Calls()[0] != "Render d-paper out.pdf format=pdf page=0 mode=recomposed paper=letter scale=0 quality=0" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	clitest.Golden(t, "render-json", r.Out)
	// What the drawing could not keep is said on standard error.
	for _, want := range []string{"2 blocks of the pages", "1 pages name a page", "3 links of the original PDF"} {
		if !strings.Contains(r.Err, want) {
			t.Fatalf("%q lacks %q", r.Err, want)
		}
	}
}

func TestRenderPNG(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, clitest.Options{}, "render", "/Notes", "--page", "3", "-o", "p.png")
	if r.Code != 0 || f.Calls()[0] != "Render /Notes p.png format=png page=3 mode= paper=a4 scale=1 quality=0" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	f = &clitest.Fake{}
	r = run(t, f, clitest.Options{}, "render", "/Notes", "--page", "1", "--format", "PNG", "--scale", "2.5", "--paper", "tablet")
	if r.Code != 0 || f.Calls()[0] != "Render /Notes  format=png page=1 mode= paper=tablet scale=2.5 quality=0" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
}

func TestRenderFormats(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--page", "2", "--format", "jpg"}, "Render /Notes  format=jpg page=2 mode= paper=a4 scale=1 quality=0"},
		{[]string{"--page", "2", "--format", "JPEG", "--quality", "90", "--scale", "2"}, "Render /Notes  format=jpg page=2 mode= paper=a4 scale=2 quality=90"},
		{[]string{"--page", "2", "--format", "svg"}, "Render /Notes  format=svg page=2 mode= paper=a4 scale=0 quality=0"},
		{[]string{"--format", "pdf", "--no-template"}, "Render /Notes  format=pdf page=0 mode=fresh paper=a4 scale=0 quality=0 no-template"},
		{[]string{"--page", "1", "--no-template"}, "Render /Notes  format=png page=1 mode= paper=a4 scale=1 quality=0 no-template"},
		// Without --format, the extension of -o decides.
		{[]string{"--page", "4", "-o", "out/p.JPEG"}, "Render /Notes out/p.JPEG format=jpg page=4 mode= paper=a4 scale=1 quality=0"},
		{[]string{"--page", "4", "-o", "p.jpg", "--quality", "50"}, "Render /Notes p.jpg format=jpg page=4 mode= paper=a4 scale=1 quality=50"},
		{[]string{"--page", "4", "-o", "p.Svg"}, "Render /Notes p.Svg format=svg page=4 mode= paper=a4 scale=0 quality=0"},
		{[]string{"-o", "doc.PDF"}, "Render /Notes doc.PDF format=pdf page=0 mode=fresh paper=a4 scale=0 quality=0"},
		{[]string{"--page", "4", "-o", "out"}, "Render /Notes out format=png page=4 mode= paper=a4 scale=1 quality=0"},
		{[]string{"-o", "out.txt"}, "Render /Notes out.txt format=pdf page=0 mode=fresh paper=a4 scale=0 quality=0"},
		// An explicit --format is used whatever -o ends with.
		{[]string{"--page", "4", "--format", "svg", "-o", "p.png"}, "Render /Notes p.png format=svg page=4 mode= paper=a4 scale=0 quality=0"},
	} {
		f := &clitest.Fake{}
		r := run(t, f, clitest.Options{}, append([]string{"render", "/Notes"}, c.args...)...)
		if r.Code != 0 || f.Calls()[0] != c.want {
			t.Fatalf("%v: %d %v %v", c.args, r.Code, r.Error, f.Calls())
		}
	}
}

func TestRenderLocalArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	local := filepath.Join(dir, "backup.rmdoc")
	if err := os.WriteFile(local, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &clitest.Fake{}
	r := run(t, f, clitest.Options{}, "render", local, "--id", "3f2a", "-o", "x.pdf")
	if r.Code != 0 || f.Calls()[0] != "RenderFile "+local+" x.pdf format=pdf page=0 mode=fresh paper=a4 scale=0 quality=0 id=3f2a" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	// A name ending with .rmdoc that is no local file is a cloud document,
	// which --id does not apply to.
	f = &clitest.Fake{}
	if r = run(t, f, clitest.Options{}, "render", "/Backups/old.rmdoc"); r.Code != 0 || !strings.HasPrefix(f.Calls()[0], "Render /Backups/old.rmdoc ") {
		t.Fatalf("%d %v", r.Code, f.Calls())
	}
	if r = run(t, &clitest.Fake{}, clitest.Options{}, "render", "/Notes", "--id", "3f2a"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestRenderUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"render"},
		{"render", "a", "b"},
		{"render", "x", "--png"},
		{"render", "x", "--format", "gif", "--page", "1"},
		{"render", "x", "--format", "png"},
		{"render", "x", "--format", "jpg"},
		{"render", "x", "--format", "svg"},
		{"render", "x", "-o", "p.png"},
		{"render", "x", "--page", "2", "--format", "pdf"},
		{"render", "x", "--page", "2", "-o", "x.pdf"},
		{"render", "x", "--scale", "2"},
		{"render", "x", "--page", "1", "--format", "svg", "--scale", "2"},
		{"render", "x", "--quality", "80"},
		{"render", "x", "--page", "1", "--quality", "80"},
		{"render", "x", "--page", "1", "--format", "svg", "--quality", "80"},
		{"render", "x", "--page", "1", "--format", "jpg", "--quality", "101"},
		{"render", "x", "--page", "1", "--format", "jpg", "--quality", "-1"},
		{"render", "x", "--page", "1", "--mode", "fresh"},
		{"render", "x", "--page", "1", "--format", "svg", "--mode", "fresh"},
		{"render", "x", "--page", "0"},
		{"render", "x", "--page", "0", "--format", "svg"},
		{"render", "x", "--page", "1", "--scale", "0"},
		{"render", "x", "--mode", "draft"},
		{"render", "x", "--paper", "a3"},
		{"render", "x", "--page", "n"},
	} {
		f := &clitest.Fake{}
		if r := run(t, f, clitest.Options{}, args...); r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
			t.Fatalf("%v: %d %v %v", args, r.Code, r.Error, f.Calls())
		}
	}
}

func TestRenderErrors(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{RenderErr: domain.ErrRenderUnsupported}, clitest.Options{}, "render", "x")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, cliutil.ErrNoRender) || !strings.Contains(r.Error.Error(), "not available in this build") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	// rmctl-lite says so, and names the full build.
	r = run(t, &clitest.Fake{RenderErr: domain.ErrRenderLite}, clitest.Options{}, "render", "x")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, cliutil.ErrNoRenderLite) ||
		r.Error.Error() != "render: rendering is not in rmctl-lite: install rmctl, the full build, for it" {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	r = run(t, &clitest.Fake{RenderErr: domain.ErrNoSuchPage}, clitest.Options{}, "render", "x", "--page", "9")
	if r.Code != cliutil.CodeError || r.Error.Error() != "render: no such page" {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r = run(t, &clitest.Fake{Err: errors.New("boom")}, clitest.Options{}, "render", "x"); r.Code != cliutil.CodeError {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r = clitest.Exec(t, cliutil.Services{}, clitest.Options{}, build, "render", "x"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}

// treeReport is a folder drawn: one document of each status.
func treeReport() port.RenderTreeReport {
	return port.RenderTreeReport{Documents: []port.RenderedDocument{
		{
			Document: domain.Item{ID: "d1"}, LibraryPath: "/Notes/A", Status: port.RenderRendered,
			Report: port.RenderReport{Path: "out/A.pdf", Pages: 4, SkippedBlocks: 2, LinksDropped: 1},
		},
		{Document: domain.Item{ID: "d2"}, LibraryPath: "/Notes/B", Status: port.RenderUnchanged, Report: port.RenderReport{Path: "out/B.pdf"}},
		{
			Document: domain.Item{ID: "d3"}, LibraryPath: "/Notes/Sub/C", Status: port.RenderFailed,
			Report: port.RenderReport{Path: "out/Sub/C.pdf"}, Err: errors.New("C: refused: pages moved"),
		},
	}}
}

func TestRenderTree(t *testing.T) {
	t.Parallel()
	errFailed := errors.New("1 of 3 documents could not be drawn")
	f := &clitest.Fake{RenderFolders: []string{"/Notes"}, RenderedTree: treeReport(), TreeErr: errFailed}
	r := run(t, f, clitest.Options{}, "render", "/Notes", "-o", "out", "--incremental", "-j", "3", "-a",
		"--mode", "preserving", "--paper", "letter", "--no-template", "--json")
	want := "RenderTree /Notes out format=pdf page=0 mode=preserving paper=letter scale=0 quality=0 no-template all incremental jobs=3"
	if r.Code != cliutil.CodeError || len(f.Calls()) != 1 || f.Calls()[0] != want {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	// The answer is written, and then the command fails.
	if r.Error.Error() != "render: 1 of 3 documents could not be drawn" {
		t.Fatal(r.Error)
	}
	clitest.Golden(t, "render-tree-json", r.Out)
	for _, note := range []string{"/Notes/A: 2 blocks of the pages", "/Notes/A: 1 links of the original PDF"} {
		if !strings.Contains(r.Err, note) {
			t.Fatalf("%q lacks %q", r.Err, note)
		}
	}
	// A table for people: a row per document, then the totals.
	tree := treeReport()
	tree.Dir = "remarkable"
	f = &clitest.Fake{RenderedTree: tree}
	r = run(t, f, clitest.Options{}, "render", "/")
	if r.Code != 0 || f.Calls()[0] != "RenderTree /  format=pdf page=0 mode=fresh paper=a4 scale=0 quality=0   jobs=0" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	clitest.Golden(t, "render-tree-plain", r.Out)
	// An empty folder says so.
	f = &clitest.Fake{RenderFolders: []string{"/Empty"}}
	if r = run(t, f, clitest.Options{}, "render", "/Empty"); r.Code != 0 || !strings.Contains(r.Err, "no document to draw below /Empty") {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Err)
	}
	// A failure before any document is the command's error alone.
	f = &clitest.Fake{RenderFolders: []string{"/Notes"}, TreeErr: domain.ErrRenderUnsupported}
	if r = run(t, f, clitest.Options{}, "render", "/Notes"); r.Code != cliutil.CodeError || r.Out != "" || !errors.Is(r.Error, cliutil.ErrNoRender) {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Out)
	}
}

func TestRenderTreeUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	local := filepath.Join(dir, "backup.rmdoc")
	if err := os.WriteFile(local, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"/Notes", "--page", "1"}, "leave --page out"},
		{[]string{"/Notes", "--page", "1", "--format", "svg"}, "leave --page out"},
		{[]string{"/Notes", "--format", "png"}, "is one page"},
		{[]string{"/Notes", "--scale", "2"}, "--scale is for a page"},
		{[]string{"/Notes", "--quality", "80"}, "--quality is for a page"},
		{[]string{"/Notes", "-o", "notes.pdf"}, "-o names the directory"},
		{[]string{"/Notes", "-o", "notes.png"}, "is one page"},
		{[]string{"/Notes/Meeting", "--all"}, "--all is for a folder; a document is drawn on its own"},
		{[]string{"/Notes/Meeting", "--incremental"}, "--incremental is for a folder"},
		{[]string{"/Notes/Meeting", "-j", "2"}, "--jobs is for a folder"},
		{[]string{local, "--incremental"}, "--incremental is for a folder; a local archive is drawn on its own"},
	} {
		f := &clitest.Fake{RenderFolders: []string{"/Notes"}}
		r := run(t, f, clitest.Options{}, append([]string{"render"}, c.args...)...)
		if r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 || !strings.Contains(r.Error.Error(), c.want) {
			t.Fatalf("%v: %d %v %v", c.args, r.Code, r.Error, f.Calls())
		}
	}
}
